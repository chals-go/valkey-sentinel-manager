package web

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/core"
	"github.com/chals-go/valkey-sentinel-manager/internal/dns"
	"github.com/chals-go/valkey-sentinel-manager/internal/models"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

// AdminHandler는 관리자 웹 UI 핸들러의 의존성을 보관하는 구조체다.
type AdminHandler struct {
	store        store.Store
	session      *SessionManager
	tmpl         *template.Template
	langMu       sync.RWMutex
	lang         string
	dnsProviders map[string]dns.Provider
	encryptor    *Encryptor
	healthCheck  *core.SentinelHealthChecker
}

// NewAdminHandler는 AdminHandler를 생성하여 반환한다.
func NewAdminHandler(s store.Store, sm *SessionManager, tmpl *template.Template, lang string, providers map[string]dns.Provider, enc *Encryptor, hc *core.SentinelHealthChecker) *AdminHandler {
	return &AdminHandler{store: s, session: sm, tmpl: tmpl, lang: lang, dnsProviders: providers, encryptor: enc, healthCheck: hc}
}

// getLang은 현재 언어 설정을 안전하게 읽어 반환한다.
func (h *AdminHandler) getLang() string {
	h.langMu.RLock()
	defer h.langMu.RUnlock()
	return h.lang
}

// setLang은 언어 설정을 안전하게 변경한다.
func (h *AdminHandler) setLang(lang string) {
	h.langMu.Lock()
	defer h.langMu.Unlock()
	h.lang = lang
}

// PageData는 템플릿 렌더링에 공통으로 사용되는 데이터 구조체다.
type PageData struct {
	Page         string
	HideSidebar  bool
	FlashMessage string
	FlashType    string
	Data         map[string]any
}

func (h *AdminHandler) render(w http.ResponseWriter, r *http.Request, name string, data PageData) {
	t := NewTranslator(h.getLang())
	csrfToken := csrfTokenFromContext(r.Context())
	funcMap := template.FuncMap{
		"t": t,
		"isMonitoringPage": func(page string) bool {
			return page == "dashboard" || page == "clusters" || page == "sentinels" || page == "events"
		},
		"csrfToken": func() string { return csrfToken },
	}
	tmpl, err := h.tmpl.Clone()
	if err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	tmpl.Funcs(funcMap)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Fragment mode: content 영역만 반환 (AJAX 부분 갱신용)
	if r.URL.Query().Get("fragment") == "true" {
		contentName := data.Page + "-content"
		if err := tmpl.ExecuteTemplate(w, contentName, data); err != nil {
			http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *AdminHandler) redirect(w http.ResponseWriter, r *http.Request, url string) {
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// RegisterRoutes는 관리자 웹 UI의 모든 라우트를 주어진 ServeMux에 등록한다.
func (h *AdminHandler) RegisterRoutes(mux *http.ServeMux) {
	// Login (no auth required).
	mux.HandleFunc("GET /admin/login", h.LoginPage)
	mux.HandleFunc("POST /admin/login", h.LoginSubmit)

	// protect는 인증 + CSRF 보호를 적용하는 미들웨어 체인이다.
	protect := func(next http.Handler) http.Handler {
		return h.session.RequireAuth(h.session.CSRFProtect(next))
	}

	// Dashboard
	mux.Handle("GET /admin/", protect(http.HandlerFunc(h.Dashboard)))
	mux.Handle("POST /admin/logout", protect(http.HandlerFunc(h.Logout)))

	// Clusters
	mux.Handle("GET /admin/clusters", protect(http.HandlerFunc(h.Clusters)))
	mux.Handle("GET /admin/clusters/new", protect(http.HandlerFunc(h.ClusterFormPage)))
	mux.Handle("POST /admin/clusters/new", protect(http.HandlerFunc(h.ClusterCreate)))
	mux.Handle("POST /admin/clusters/load-sentinels/query", protect(http.HandlerFunc(h.LoadSentinelsQuery)))
	mux.Handle("POST /admin/clusters/load-sentinels", protect(http.HandlerFunc(h.LoadSentinelsSave)))
	mux.Handle("GET /admin/clusters/{masterName}/edit", protect(http.HandlerFunc(h.ClusterEditPage)))
	mux.Handle("POST /admin/clusters/{masterName}/edit", protect(http.HandlerFunc(h.ClusterEditSave)))
	mux.Handle("POST /admin/clusters/{masterName}/delete", protect(http.HandlerFunc(h.ClusterDelete)))
	mux.Handle("POST /admin/clusters/{masterName}/pause", protect(http.HandlerFunc(h.ClusterPause)))
	mux.Handle("POST /admin/clusters/{masterName}/resume", protect(http.HandlerFunc(h.ClusterResume)))
	mux.Handle("POST /admin/clusters/{masterName}/test-failover", protect(http.HandlerFunc(h.ClusterTestFailover)))
	mux.Handle("POST /admin/clusters/{masterName}/sync-dns", protect(http.HandlerFunc(h.ClusterSyncDNS)))

	// Sentinels
	mux.Handle("GET /admin/sentinels", protect(http.HandlerFunc(h.Sentinels)))
	mux.Handle("POST /admin/sentinels/new-cluster", protect(http.HandlerFunc(h.SentinelClusterCreate)))
	mux.Handle("POST /admin/sentinels/{grpName}/delete-cluster", protect(http.HandlerFunc(h.SentinelClusterDelete)))
	mux.Handle("POST /admin/sentinels/{grpName}/edit", protect(http.HandlerFunc(h.SentinelClusterEditSave)))
	mux.Handle("POST /admin/sentinels/{grpName}/toggle-alert", protect(http.HandlerFunc(h.SentinelToggleAlert)))
	mux.Handle("POST /admin/sentinels/{grpName}/add-node", protect(http.HandlerFunc(h.SentinelAddNode)))
	mux.Handle("POST /admin/sentinels/{nodeName}/delete-node", protect(http.HandlerFunc(h.SentinelDeleteNode)))

	// Events
	mux.Handle("GET /admin/events", protect(http.HandlerFunc(h.Events)))

	// Settings redirect
	mux.Handle("GET /admin/settings", protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.redirect(w, r, "/admin/settings/server")
	})))
	mux.Handle("GET /admin/settings/server", protect(http.HandlerFunc(h.SettingsServer)))
	mux.Handle("POST /admin/settings/server", protect(http.HandlerFunc(h.SettingsServerSave)))
	mux.Handle("GET /admin/settings/dns", protect(http.HandlerFunc(h.SettingsDNS)))
	mux.Handle("POST /admin/dns-provider/new", protect(http.HandlerFunc(h.DNSProviderCreate)))
	mux.Handle("GET /admin/settings/dns/edit/{providerName}", protect(http.HandlerFunc(h.DNSProviderEditPage)))
	mux.Handle("POST /admin/settings/dns/edit/{providerName}", protect(http.HandlerFunc(h.DNSProviderEditSave)))
	mux.Handle("POST /admin/dns-provider/{providerName}/delete", protect(http.HandlerFunc(h.DNSProviderDelete)))
	mux.Handle("GET /admin/settings/token", protect(http.HandlerFunc(h.SettingsToken)))
	mux.Handle("POST /admin/regenerate-token", protect(http.HandlerFunc(h.RegenerateToken)))
	mux.Handle("POST /admin/delete-token", protect(http.HandlerFunc(h.DeleteToken)))
	mux.Handle("GET /admin/settings/notification", protect(http.HandlerFunc(h.SettingsNotification)))
	mux.Handle("POST /admin/webhook", protect(http.HandlerFunc(h.WebhookCreate)))
	mux.Handle("POST /admin/webhook/{id}/edit", protect(http.HandlerFunc(h.WebhookEdit)))
	mux.Handle("POST /admin/webhook/{id}/delete", protect(http.HandlerFunc(h.WebhookDelete)))
	mux.Handle("POST /admin/webhook/{id}/test", protect(http.HandlerFunc(h.WebhookTest)))
	mux.Handle("POST /admin/webhook/{id}/toggle", protect(http.HandlerFunc(h.WebhookToggle)))
	mux.Handle("GET /admin/settings/account", protect(http.HandlerFunc(h.SettingsAccount)))
	mux.Handle("POST /admin/settings/account", protect(http.HandlerFunc(h.SettingsAccountSave)))
}

// === Login / Logout ===

// LoginPage는 로그인 페이지를 렌더링한다. 이미 인증된 경우 대시보드로 리다이렉트한다.
func (h *AdminHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if h.session.ValidateSession(r) {
		h.redirect(w, r, "/admin/")
		return
	}
	h.render(w, r, "base", PageData{Page: "login", HideSidebar: true})
}

// LoginSubmit은 로그인 폼 제출을 처리하고, 인증 성공 시 세션을 생성하여 대시보드로 리다이렉트한다.
func (h *AdminHandler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if h.session.IsLoginLocked(ip) {
		t := NewTranslator(h.getLang())
		h.render(w, r, "base", PageData{Page: "login", HideSidebar: true, FlashMessage: t("flash_login_locked"), FlashType: "error"})
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}
	password := r.FormValue("password")

	hash, storeErr := h.store.GetAdminPasswordHash(context.Background())
	if storeErr != nil {
		if !errors.Is(storeErr, store.ErrNotFound) {
			slog.Error("store error", "method", "GetAdminPasswordHash", "error", storeErr)
			http.Error(w, "503 Service Unavailable", http.StatusServiceUnavailable)
			return
		}
		slog.Warn("store error", "method", "GetAdminPasswordHash", "error", storeErr)
	}
	var ok bool
	if hash == "" {
		ok = password == defaultPassword
	} else {
		ok = VerifyHash(password, hash)
	}

	if !ok {
		h.session.RecordLoginFailure(ip)
		t := NewTranslator(h.getLang())
		h.render(w, r, "base", PageData{Page: "login", HideSidebar: true, FlashMessage: t("flash_login_failed"), FlashType: "error"})
		return
	}

	h.session.ClearLoginFailures(ip)
	sid, err := h.session.CreateSession()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.session.SetSessionCookie(w, sid)
	h.redirect(w, r, "/admin/")
}

// Logout은 현재 세션을 종료하고 쿠키를 삭제한 후 로그인 페이지로 리다이렉트한다.
func (h *AdminHandler) Logout(w http.ResponseWriter, r *http.Request) {
	h.session.DestroySession(r)
	h.session.ClearSessionCookie(w)
	h.redirect(w, r, "/admin/login")
}

// === Dashboard ===

// Dashboard는 시스템 개요가 포함된 메인 대시보드 페이지를 렌더링한다.
func (h *AdminHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Load language from runtime settings.
	if rt, err := h.store.GetRuntimeSettings(ctx); err == nil {
		if l, ok := rt["language"]; ok && l != "" {
			h.setLang(l)
		}
	}

	clusters, storeErr := h.store.ListClusters(ctx)
	if storeErr != nil { slog.Warn("store error", "method", "ListClusters", "error", storeErr) }
	events, storeErr := h.store.GetRecentEvents(ctx, 500)
	if storeErr != nil { slog.Warn("store error", "method", "GetRecentEvents", "error", storeErr) }
	sentinels, storeErr := h.store.ListSentinels(ctx, "")
	if storeErr != nil { slog.Warn("store error", "method", "ListSentinels", "error", storeErr) }

	oneHourAgo := float64(time.Now().Unix()) - 3600
	recentCount := 0
	for _, e := range events {
		if e.Timestamp > oneHourAgo {
			recentCount++
		}
	}

	// Store type detection.
	storeType := "VALKEY"
	if _, ok := h.store.(*store.MemoryStore); ok {
		storeType = "MEMORY"
	}

	// Live info from Sentinel for each cluster (parallel).
	type dashInfo struct {
		name   string
		detail *core.MasterDetail
	}
	dashCh := make(chan dashInfo, len(clusters))
	for _, c := range clusters {
		go func(c *models.Cluster) {
			d := core.GetMasterDetail(ctx, c.SentinelAddrs, c.MasterName, c.SentinelPassword)
			dashCh <- dashInfo{name: c.MasterName, detail: d}
		}(c)
	}
	liveInfo := make(map[string]*core.MasterDetail, len(clusters))
	for range clusters {
		info := <-dashCh
		if info.detail != nil {
			liveInfo[info.name] = info.detail
		}
	}

	// Sentinels by group.
	sentinelsByGroup := make(map[string][]*models.Sentinel)
	for _, s := range sentinels {
		sentinelsByGroup[s.GroupName] = append(sentinelsByGroup[s.GroupName], s)
	}

	// Cluster pagination (5 per page).
	cpStr := r.URL.Query().Get("cp")
	cp, _ := strconv.Atoi(cpStr)
	if cp < 1 {
		cp = 1
	}
	clusterPerPage := 5
	clusterTotal := len(clusters)
	clusterTotalPages := (clusterTotal + clusterPerPage - 1) / clusterPerPage
	if clusterTotalPages < 1 {
		clusterTotalPages = 1
	}
	if cp > clusterTotalPages {
		cp = clusterTotalPages
	}
	clusterStart := (cp - 1) * clusterPerPage
	clusterEnd := clusterStart + clusterPerPage
	if clusterEnd > clusterTotal {
		clusterEnd = clusterTotal
	}
	displayClusters := clusters[clusterStart:clusterEnd]

	// Event pagination (10 per page).
	epStr := r.URL.Query().Get("ep")
	ep, _ := strconv.Atoi(epStr)
	if ep < 1 {
		ep = 1
	}
	eventPerPage := 10
	eventTotal := len(events)
	eventTotalPages := (eventTotal + eventPerPage - 1) / eventPerPage
	if eventTotalPages < 1 {
		eventTotalPages = 1
	}
	if ep > eventTotalPages {
		ep = eventTotalPages
	}
	eventStart := (ep - 1) * eventPerPage
	eventEnd := eventStart + eventPerPage
	if eventEnd > eventTotal {
		eventEnd = eventTotal
	}
	displayEvents := events[eventStart:eventEnd]

	h.render(w, r, "base", PageData{
		Page: "dashboard",
		Data: map[string]any{
			"Clusters":           displayClusters,
			"AllClusters":        clusters,
			"Events":             displayEvents,
			"Sentinels":          sentinels,
			"RecentEventCount":   recentCount,
			"StoreType":          storeType,
			"LiveInfo":           liveInfo,
			"SentinelsByGroup":   sentinelsByGroup,
			"ClusterPage":        cp,
			"ClusterTotalPages":  clusterTotalPages,
			"EventPage":          ep,
			"EventTotalPages":    eventTotalPages,
		},
	})
}

// === Clusters ===

// Clusters는 등록된 Replication Group 목록 페이지를 렌더링한다.
// clustersPageData는 클러스터 목록 페이지에 필요한 데이터를 조회하여 반환한다.
func (h *AdminHandler) clustersPageData(ctx context.Context) map[string]any {
	clusters, _ := h.store.ListClusters(ctx)
	sentinels, _ := h.store.ListSentinels(ctx, "")
	sentinelClusterNames := make(map[string]bool)
	for _, s := range sentinels {
		sentinelClusterNames[s.GroupName] = true
	}
	dnsConfigs, _ := h.store.ListDNSProviderConfigs(ctx)
	dnsProvidersWithZone := make(map[string]string)
	for name, cfg := range dnsConfigs {
		dnsProvidersWithZone[name] = cfg["zone_name"]
	}
	return map[string]any{
		"Clusters":             clusters,
		"SentinelClusterNames": sortedKeys(sentinelClusterNames),
		"DNSProviders":         dnsProvidersWithZone,
		"LiveInfo":             map[string]*core.MasterDetail{},
		"SentinelsByGroup":     map[string][]*models.Sentinel{},
		"DNSRecords":           map[string]map[string][]string{},
		"SentinelPingResults":  h.healthCheck.GetAllStatuses(),
		"ClusterPage":          1,
		"ClusterTotalPages":    1,
		"ExistingMasters":      existingMastersByGroup(clusters),
	}
}

func (h *AdminHandler) Clusters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clusters, storeErr := h.store.ListClusters(ctx)
	if storeErr != nil { slog.Warn("store error", "method", "ListClusters", "error", storeErr) }
	sentinels, storeErr := h.store.ListSentinels(ctx, "")
	if storeErr != nil { slog.Warn("store error", "method", "ListSentinels", "error", storeErr) }

	sentinelClusterNames := make(map[string]bool)
	for _, s := range sentinels {
		sentinelClusterNames[s.GroupName] = true
	}

	dnsConfigs, storeErr := h.store.ListDNSProviderConfigs(ctx)
	if storeErr != nil { slog.Warn("store error", "method", "ListDNSProviderConfigs", "error", storeErr) }
	dnsProvidersWithZone := make(map[string]string)
	for name, cfg := range dnsConfigs {
		dnsProvidersWithZone[name] = cfg["zone_name"]
	}

	// Live info + DNS resolve in parallel for all clusters.
	type clusterInfo struct {
		name   string
		detail *core.MasterDetail
		dns    map[string][]string
	}
	infoCh := make(chan clusterInfo, len(clusters))

	for _, c := range clusters {
		go func(c *models.Cluster) {
			detail := core.GetMasterDetail(ctx, c.SentinelAddrs, c.MasterName, c.SentinelPassword)

			rec := map[string][]string{"primary_ips": nil, "replica_ips": nil}
			if c.PrimaryDNS.RecordName != "" && c.PrimaryDNS.Zone != "" {
				if ips, err := net.LookupHost(c.PrimaryDNS.RecordName + "." + c.PrimaryDNS.Zone); err == nil {
					rec["primary_ips"] = ips
				}
			}
			if c.ReplicaDNS != nil && c.ReplicaDNS.RecordName != "" && c.ReplicaDNS.Zone != "" {
				if ips, err := net.LookupHost(c.ReplicaDNS.RecordName + "." + c.ReplicaDNS.Zone); err == nil {
					rec["replica_ips"] = ips
				}
			}

			infoCh <- clusterInfo{name: c.MasterName, detail: detail, dns: rec}
		}(c)
	}

	liveInfo := make(map[string]*core.MasterDetail, len(clusters))
	dnsRecords := make(map[string]map[string][]string, len(clusters))
	for range clusters {
		info := <-infoCh
		if info.detail != nil {
			liveInfo[info.name] = info.detail
		}
		dnsRecords[info.name] = info.dns
	}

	// Sentinels by group.
	sentinelsByGroup := make(map[string][]*models.Sentinel)
	for _, s := range sentinels {
		sentinelsByGroup[s.GroupName] = append(sentinelsByGroup[s.GroupName], s)
	}

	// Pagination (15 per page).
	cpStr := r.URL.Query().Get("cp")
	cp, _ := strconv.Atoi(cpStr)
	if cp < 1 {
		cp = 1
	}
	perPage := 15
	total := len(clusters)
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	if cp > totalPages {
		cp = totalPages
	}
	start := (cp - 1) * perPage
	end := start + perPage
	if end > total {
		end = total
	}
	displayClusters := clusters[start:end]

	h.render(w, r, "base", PageData{
		Page: "clusters",
		Data: map[string]any{
			"Clusters":             displayClusters,
			"SentinelClusterNames": sortedKeys(sentinelClusterNames),
			"DNSProviders":         dnsProvidersWithZone,
			"LiveInfo":             liveInfo,
			"SentinelsByGroup":     sentinelsByGroup,
			"DNSRecords":           dnsRecords,
			"SentinelPingResults":  h.healthCheck.GetAllStatuses(),
			"ClusterPage":          cp,
			"ClusterTotalPages":    totalPages,
			"ExistingMasters":      existingMastersByGroup(clusters),
		},
	})
}

// ClusterFormPage는 새 Replication Group 등록 폼 페이지를 렌더링한다.
func (h *AdminHandler) ClusterFormPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sentinels, storeErr := h.store.ListSentinels(ctx, "")
	if storeErr != nil { slog.Warn("store error", "method", "ListSentinels", "error", storeErr) }
	sentinelClusterNames := make(map[string]bool)
	for _, s := range sentinels {
		sentinelClusterNames[s.GroupName] = true
	}
	dnsConfigs, storeErr := h.store.ListDNSProviderConfigs(ctx)
	if storeErr != nil { slog.Warn("store error", "method", "ListDNSProviderConfigs", "error", storeErr) }
	dnsProvidersWithZone := make(map[string]string)
	for name, cfg := range dnsConfigs {
		dnsProvidersWithZone[name] = cfg["zone_name"]
	}
	allClusters, _ := h.store.ListClusters(ctx)
	h.render(w, r, "base", PageData{
		Page: "cluster-form",
		Data: map[string]any{
			"SentinelClusterNames": sortedKeys(sentinelClusterNames),
			"DNSProviders":         dnsProvidersWithZone,
			"ExistingMasters":      existingMastersByGroup(allClusters),
		},
	})
}

// ClusterEditPage는 기존 Replication Group 수정 폼 페이지를 렌더링한다.
func (h *AdminHandler) ClusterEditPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	masterName := r.PathValue("masterName")
	cluster, err := h.store.GetCluster(ctx, masterName)
	if err != nil {
		h.redirect(w, r, "/admin/clusters")
		return
	}
	detail := core.GetMasterDetail(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword)
	downAfterMs := 5000
	failoverTimeout := 30000
	if detail != nil {
		downAfterMs = detail.DownAfterMs
		failoverTimeout = detail.FailoverTimeout
	}
	dnsConfigs, storeErr := h.store.ListDNSProviderConfigs(ctx)
	if storeErr != nil { slog.Warn("store error", "method", "ListDNSProviderConfigs", "error", storeErr) }
	dnsProvidersWithZone := make(map[string]string)
	for name, cfg := range dnsConfigs {
		dnsProvidersWithZone[name] = cfg["zone_name"]
	}

	h.render(w, r, "base", PageData{
		Page: "cluster-edit",
		Data: map[string]any{
			"Cluster":        cluster,
			"SentinelCluster": cluster.GroupName,
			"PrimaryIP":      cluster.PrimaryIP,
			"PrimaryPort":    cluster.PrimaryPort,
			"DownAfterMs":    downAfterMs,
			"FailoverTimeout": failoverTimeout,
			"DNSProviders":   dnsProvidersWithZone,
		},
	})
}

// ClusterCreate는 새 Replication Group 등록 요청을 처리하고, Sentinel 모니터링과 DNS 레코드를 생성한다.
func (h *AdminHandler) ClusterCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	sentinelCluster := strings.TrimSpace(r.FormValue("sentinel_cluster"))
	monitoringName := strings.TrimSpace(r.FormValue("monitoring_name"))
	primaryIP := strings.TrimSpace(r.FormValue("primary_ip"))
	dnsProvider := strings.TrimSpace(r.FormValue("dns_provider"))
	primaryPort, _ := strconv.Atoi(r.FormValue("primary_port"))
	if primaryPort == 0 {
		primaryPort = 6379
	}
	quorumThreshold, _ := strconv.Atoi(r.FormValue("quorum_threshold"))
	if quorumThreshold == 0 {
		quorumThreshold = 2
	}
	redisPassword := r.FormValue("redis_password")
	redisUsername := strings.TrimSpace(r.FormValue("redis_username"))
	dnsTTL, _ := strconv.Atoi(r.FormValue("dns_ttl"))
	if dnsTTL == 0 {
		dnsTTL = 3
	}
	quorumMode := r.FormValue("quorum_mode") == "on" || r.FormValue("quorum_mode") == "true"
	createReplicaDNS := r.FormValue("create_replica_dns") == "on" || r.FormValue("create_replica_dns") == "true"

	skipDNS := r.FormValue("skip_dns") == "on"

	// Check duplicate.
	if _, err := h.store.GetCluster(ctx, monitoringName); err == nil {
		t := NewTranslator(h.getLang())
		h.render(w, r, "base", PageData{
			Page: "clusters", FlashMessage: t("flash_duplicate_cluster") + ": " + monitoringName, FlashType: "error",
			Data: h.clustersPageData(ctx),
		})
		return
	}

	// Get sentinel addrs from cluster.
	sents, storeErr := h.store.ListSentinels(ctx, sentinelCluster)
	if storeErr != nil { slog.Warn("store error", "method", "ListSentinels", "error", storeErr) }
	var sentinelAddrs []string
	var sentinelPassword string
	for _, s := range sents {
		sentinelAddrs = append(sentinelAddrs, fmt.Sprintf("%s:%d", s.Host, s.Port))
	}

	var primaryDNS models.DNSMapping
	if !skipDNS {
		// Get zone_name from DNS provider config.
		dnsCfg, storeErr := h.store.GetDNSProviderConfig(ctx, dnsProvider)
		if storeErr != nil { slog.Warn("store error", "method", "GetDNSProviderConfig", "error", storeErr) }
		zoneName := ""
		if dnsCfg != nil {
			zoneName = dnsCfg["zone_name"]
		}
		primaryDNS = models.DNSMapping{Zone: zoneName, RecordName: fmt.Sprintf("primary-%s", monitoringName), RecordType: "A", TTL: dnsTTL}
	} else {
		dnsProvider = ""
	}

	cluster := &models.Cluster{
		GroupName:        sentinelCluster,
		MasterName:       monitoringName,
		SentinelAddrs:    sentinelAddrs,
		DNSProvider:      dnsProvider,
		PrimaryIP:        primaryIP,
		PrimaryPort:      primaryPort,
		PrimaryDNS:       primaryDNS,
		RedisPassword:    redisPassword,
		RedisUsername:    redisUsername,
		SentinelPassword: sentinelPassword,
		QuorumMode:       quorumMode,
		QuorumThreshold:  quorumThreshold,
	}
	h.store.SaveCluster(ctx, cluster)

	// Sentinel MONITOR.
	rt, storeErr := h.store.GetRuntimeSettings(ctx)
	if storeErr != nil { slog.Warn("store error", "method", "GetRuntimeSettings", "error", storeErr) }
	downMs := intFromMap(rt, "sentinel_down_after_ms", 5000)
	failTimeout := intFromMap(rt, "sentinel_failover_timeout", 30000)

	core.SentinelMonitor(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.PrimaryIP, cluster.PrimaryPort, cluster.QuorumThreshold, cluster.RedisUsername, cluster.RedisPassword, cluster.SentinelPassword, downMs, failTimeout)

	// DNS record creation.
	detail := core.GetMasterDetail(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword)
	provider := h.dnsProviders[cluster.DNSProvider]
	if detail != nil && provider != nil {
		provider.UpdateRecord(ctx, cluster.PrimaryDNS.Zone, cluster.PrimaryDNS.RecordName, cluster.PrimaryDNS.RecordType, detail.MasterIP, cluster.PrimaryDNS.TTL)
		if createReplicaDNS && len(detail.Slaves) > 0 {
			rRec := strings.Replace(cluster.PrimaryDNS.RecordName, "primary", "replica", 1)
			replicaDNS := &models.DNSMapping{Zone: cluster.PrimaryDNS.Zone, RecordName: rRec, RecordType: "A", TTL: cluster.PrimaryDNS.TTL}
			cluster.ReplicaDNS = replicaDNS
			cluster.MultiReplica = len(detail.Slaves) > 1
			h.store.SaveCluster(ctx, cluster)
			if len(detail.Slaves) == 1 {
				provider.UpdateRecord(ctx, replicaDNS.Zone, rRec, "A", detail.Slaves[0].IP, replicaDNS.TTL)
			} else {
				var ips []string
				for _, s := range detail.Slaves {
					ips = append(ips, s.IP)
				}
				provider.UpdateRecordValues(ctx, replicaDNS.Zone, rRec, "A", ips, replicaDNS.TTL)
			}
		}
	}

	h.redirect(w, r, "/admin/clusters")
}

// LoadSentinelsQuery는 선택한 센티널 클러스터에서 모니터링 중인 마스터 목록을 JSON으로 반환한다.
func (h *AdminHandler) LoadSentinelsQuery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}
	clusterName := strings.TrimSpace(r.FormValue("sentinel_cluster"))
	if clusterName == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"masters":[]}`))
		return
	}

	sents, _ := h.store.ListSentinels(ctx, clusterName)
	if len(sents) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"masters":[],"error":"no sentinel nodes found for cluster: ` + clusterName + `"}`))
		return
	}
	var addrs []string
	for _, s := range sents {
		addrs = append(addrs, fmt.Sprintf("%s:%d", s.Host, s.Port))
	}

	// 센티널 비밀번호는 해당 그룹의 기존 클러스터에서 가져오기 시도
	var sentinelPassword string
	existingClusters, _ := h.store.ListClusters(ctx)
	for _, c := range existingClusters {
		if c.GroupName == clusterName && c.SentinelPassword != "" {
			sentinelPassword = c.SentinelPassword
			break
		}
	}

	masters := core.ListSentinelMasters(ctx, addrs, sentinelPassword)

	// 이미 등록된 클러스터 목록
	registered := make(map[string]bool)
	for _, c := range existingClusters {
		registered[c.MasterName] = true
	}

	type masterResult struct {
		Name       string `json:"name"`
		IP         string `json:"ip"`
		Port       int    `json:"port"`
		Quorum     int    `json:"quorum"`
		Status     string `json:"status"`
		Registered bool   `json:"registered"`
	}
	var results []masterResult
	for _, m := range masters {
		results = append(results, masterResult{
			Name:       m.Name,
			IP:         m.IP,
			Port:       m.Port,
			Quorum:     m.Quorum,
			Status:     m.Status,
			Registered: registered[m.Name],
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"masters": results})
}

// LoadSentinelsSave는 선택된 마스터들을 DNS disabled 상태로 일괄 등록한다.
func (h *AdminHandler) LoadSentinelsSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}
	clusterName := strings.TrimSpace(r.FormValue("sentinel_cluster"))
	selectedMasters := r.Form["masters"]

	if clusterName == "" || len(selectedMasters) == 0 {
		h.redirect(w, r, "/admin/clusters")
		return
	}

	sents, _ := h.store.ListSentinels(ctx, clusterName)
	var addrs []string
	for _, s := range sents {
		addrs = append(addrs, fmt.Sprintf("%s:%d", s.Host, s.Port))
	}

	// 센티널에서 마스터 상세 정보 조회
	allMasters := core.ListSentinelMasters(ctx, addrs, "")
	masterMap := make(map[string]core.SentinelMasterInfo)
	for _, m := range allMasters {
		masterMap[m.Name] = m
	}

	// Runtime 설정에서 down-after-ms, failover-timeout 조회
	rt, _ := h.store.GetRuntimeSettings(ctx)
	downMs := intFromMap(rt, "sentinel_down_after_ms", 5000)
	failTimeout := intFromMap(rt, "sentinel_failover_timeout", 30000)

	count := 0
	for _, name := range selectedMasters {
		// 이미 등록된 경우 스킵
		if _, err := h.store.GetCluster(ctx, name); err == nil {
			continue
		}
		info, ok := masterMap[name]
		if !ok {
			continue
		}
		cluster := &models.Cluster{
			GroupName:       clusterName,
			MasterName:      name,
			SentinelAddrs:   addrs,
			PrimaryIP:       info.IP,
			PrimaryPort:     info.Port,
			QuorumMode:      true,
			QuorumThreshold: info.Quorum,
		}
		if err := h.store.SaveCluster(ctx, cluster); err != nil {
			slog.Error("save cluster failed", "name", name, "error", err)
			continue
		}
		// 센티널 설정 적용 (down-after-ms, failover-timeout, scripts)
		core.SentinelApplyConfig(ctx, addrs, name, "", "", downMs, failTimeout)
		count++
	}

	slog.Info("load sentinels completed", "cluster", clusterName, "registered", count)
	h.redirect(w, r, "/admin/clusters")
}

// ClusterEditSave는 Replication Group 수정 폼 제출을 처리하고 DNS TTL 및 Sentinel 설정을 업데이트한다.
func (h *AdminHandler) ClusterEditSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	masterName := r.PathValue("masterName")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	cluster, err := h.store.GetCluster(ctx, masterName)
	if err != nil {
		h.redirect(w, r, "/admin/clusters")
		return
	}

	dnsTTL, _ := strconv.Atoi(r.FormValue("dns_ttl"))
	if dnsTTL == 0 {
		dnsTTL = 3
	}
	downAfterMs, _ := strconv.Atoi(r.FormValue("down_after_ms"))
	if downAfterMs == 0 {
		downAfterMs = 5000
	}
	failoverTimeout, _ := strconv.Atoi(r.FormValue("failover_timeout"))
	if failoverTimeout == 0 {
		failoverTimeout = 30000
	}

	// DNS 추가 (DNS disabled → DNS enabled 전환)
	addDNS := r.FormValue("add_dns") == "on"
	if addDNS && cluster.DNSProvider == "" {
		dnsProvider := strings.TrimSpace(r.FormValue("dns_provider"))
		if dnsProvider != "" {
			dnsCfg, _ := h.store.GetDNSProviderConfig(ctx, dnsProvider)
			zoneName := ""
			if dnsCfg != nil {
				zoneName = dnsCfg["zone_name"]
			}
			primaryRecord := strings.TrimSpace(r.FormValue("primary_record"))
			if primaryRecord == "" {
				primaryRecord = fmt.Sprintf("primary-%s", cluster.MasterName)
			}
			cluster.DNSProvider = dnsProvider
			cluster.PrimaryDNS = models.DNSMapping{Zone: zoneName, RecordName: primaryRecord, RecordType: "A", TTL: dnsTTL}

			// DNS 레코드 생성
			detail := core.GetMasterDetail(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword)
			provider := h.dnsProviders[dnsProvider]
			if detail != nil && provider != nil {
				provider.UpdateRecord(ctx, cluster.PrimaryDNS.Zone, cluster.PrimaryDNS.RecordName, cluster.PrimaryDNS.RecordType, detail.MasterIP, cluster.PrimaryDNS.TTL)

				createReplicaDNS := r.FormValue("create_replica_dns") == "on"
				if createReplicaDNS && len(detail.Slaves) > 0 {
					rRec := strings.Replace(primaryRecord, "primary", "replica", 1)
					replicaDNS := &models.DNSMapping{Zone: zoneName, RecordName: rRec, RecordType: "A", TTL: dnsTTL}
					cluster.ReplicaDNS = replicaDNS
					cluster.MultiReplica = len(detail.Slaves) > 1
					if len(detail.Slaves) == 1 {
						provider.UpdateRecord(ctx, replicaDNS.Zone, rRec, "A", detail.Slaves[0].IP, replicaDNS.TTL)
					} else {
						var ips []string
						for _, s := range detail.Slaves {
							ips = append(ips, s.IP)
						}
						provider.UpdateRecordValues(ctx, replicaDNS.Zone, rRec, "A", ips, replicaDNS.TTL)
					}
				}
			}
		}
	} else if cluster.DNSProvider != "" {
		disableDNS := r.FormValue("disable_dns") == "on"
		addReplicaDNS := r.FormValue("add_replica_dns") == "on"

		if disableDNS {
			// DNS 비활성화: 레코드 삭제 후 모델 초기화
			if provider := h.dnsProviders[cluster.DNSProvider]; provider != nil {
				provider.DeleteRecord(ctx, cluster.PrimaryDNS.Zone, cluster.PrimaryDNS.RecordName, cluster.PrimaryDNS.RecordType)
				if cluster.ReplicaDNS != nil {
					provider.DeleteRecord(ctx, cluster.ReplicaDNS.Zone, cluster.ReplicaDNS.RecordName, cluster.ReplicaDNS.RecordType)
				}
			}
			cluster.DNSProvider = ""
			cluster.PrimaryDNS = models.DNSMapping{}
			cluster.ReplicaDNS = nil
		} else {
			// TTL 업데이트
			cluster.PrimaryDNS.TTL = dnsTTL
			if cluster.ReplicaDNS != nil {
				cluster.ReplicaDNS.TTL = dnsTTL
			}
			// Replica DNS 추가 (없는 경우만)
			if addReplicaDNS && cluster.ReplicaDNS == nil {
				rRec := strings.Replace(cluster.PrimaryDNS.RecordName, "primary", "replica", 1)
				replicaDNS := &models.DNSMapping{Zone: cluster.PrimaryDNS.Zone, RecordName: rRec, RecordType: "A", TTL: dnsTTL}
				cluster.ReplicaDNS = replicaDNS

				detail := core.GetMasterDetail(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword)
				provider := h.dnsProviders[cluster.DNSProvider]
				if detail != nil && provider != nil && len(detail.Slaves) > 0 {
					cluster.MultiReplica = len(detail.Slaves) > 1
					if len(detail.Slaves) == 1 {
						provider.UpdateRecord(ctx, replicaDNS.Zone, rRec, "A", detail.Slaves[0].IP, replicaDNS.TTL)
					} else {
						var ips []string
						for _, s := range detail.Slaves {
							ips = append(ips, s.IP)
						}
						provider.UpdateRecordValues(ctx, replicaDNS.Zone, rRec, "A", ips, replicaDNS.TTL)
					}
				}
			}
		}
	}
	// Redis 인증 정보 업데이트 (비밀번호가 제출된 경우에만 반영)
	newRedisPassword := r.FormValue("redis_password")
	newRedisUsername := strings.TrimSpace(r.FormValue("redis_username"))
	passwordChanged := newRedisPassword != ""
	if passwordChanged {
		cluster.RedisPassword = newRedisPassword
		cluster.RedisUsername = newRedisUsername
	}

	h.store.SaveCluster(ctx, cluster)

	core.SentinelSetConfig(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword, downAfterMs, failoverTimeout)

	// 비밀번호가 변경된 경우 센티널에 auth-user/auth-pass 업데이트 적용
	if passwordChanged {
		core.SentinelApplyConfig(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.RedisUsername, cluster.SentinelPassword, downAfterMs, failoverTimeout)
	}

	h.redirect(w, r, "/admin/clusters")
}

// ClusterDelete는 Replication Group 삭제 요청을 처리하고, Sentinel 모니터링과 DNS 레코드를 제거한다.
func (h *AdminHandler) ClusterDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	masterName := r.PathValue("masterName")

	cluster, err := h.store.GetCluster(ctx, masterName)
	if err == nil && cluster != nil {
		core.SentinelRemove(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword)

		if provider := h.dnsProviders[cluster.DNSProvider]; provider != nil {
			provider.DeleteRecord(ctx, cluster.PrimaryDNS.Zone, cluster.PrimaryDNS.RecordName, cluster.PrimaryDNS.RecordType)
			if cluster.ReplicaDNS != nil {
				provider.DeleteRecord(ctx, cluster.ReplicaDNS.Zone, cluster.ReplicaDNS.RecordName, cluster.ReplicaDNS.RecordType)
			}
		}
	}
	h.store.DeleteCluster(ctx, masterName)
	h.redirect(w, r, "/admin/clusters")
}

// ClusterPause는 Replication Group 모니터링을 일시정지하고, Sentinel의 down-after-milliseconds를 2시간으로 설정한다.
func (h *AdminHandler) ClusterPause(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	masterName := r.PathValue("masterName")

	cluster, err := h.store.GetCluster(ctx, masterName)
	if err != nil {
		h.redirect(w, r, "/admin/clusters")
		return
	}

	detail := core.GetMasterDetail(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword)
	if detail != nil {
		cluster.PausedDownAfterMs = detail.DownAfterMs
	} else {
		rt, storeErr := h.store.GetRuntimeSettings(ctx)
		if storeErr != nil { slog.Warn("store error", "method", "GetRuntimeSettings", "error", storeErr) }
		cluster.PausedDownAfterMs = intFromMap(rt, "sentinel_down_after_ms", 5000)
	}
	cluster.IsPaused = true
	h.store.SaveCluster(ctx, cluster)

	core.SentinelSetConfig(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword, 7200000, 30000)
	h.redirect(w, r, "/admin/clusters")
}

// ClusterResume은 일시정지된 Replication Group 모니터링을 재개하고, Sentinel 설정을 원래 값으로 복원한다.
func (h *AdminHandler) ClusterResume(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	masterName := r.PathValue("masterName")

	cluster, err := h.store.GetCluster(ctx, masterName)
	if err != nil {
		h.redirect(w, r, "/admin/clusters")
		return
	}

	originalMs := cluster.PausedDownAfterMs
	if originalMs == 0 {
		originalMs = 5000
	}
	core.SentinelSetConfig(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword, originalMs, 30000)

	cluster.IsPaused = false
	cluster.PausedDownAfterMs = 0
	h.store.SaveCluster(ctx, cluster)
	h.redirect(w, r, "/admin/clusters")
}

// ClusterTestFailover는 특정 Replication Group에 대해 SENTINEL FAILOVER 명령을 실행하여 수동 페일오버를 트리거한다.
func (h *AdminHandler) ClusterTestFailover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	masterName := r.PathValue("masterName")

	cluster, err := h.store.GetCluster(ctx, masterName)
	if err != nil {
		h.redirect(w, r, "/admin/clusters")
		return
	}

	core.TriggerTestFailover(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword)
	h.redirect(w, r, "/admin/clusters")
}

// ClusterSyncDNS는 Sentinel에서 현재 Primary/Replica IP를 조회하여 DNS 레코드를 강제로 동기화한다.
func (h *AdminHandler) ClusterSyncDNS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	masterName := r.PathValue("masterName")

	cluster, err := h.store.GetCluster(ctx, masterName)
	if err != nil {
		h.redirect(w, r, "/admin/clusters")
		return
	}

	detail := core.GetMasterDetail(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword)
	if detail == nil {
		h.redirect(w, r, "/admin/clusters")
		return
	}

	provider := h.dnsProviders[cluster.DNSProvider]
	if provider == nil {
		h.redirect(w, r, "/admin/clusters")
		return
	}

	provider.UpdateRecord(ctx, cluster.PrimaryDNS.Zone, cluster.PrimaryDNS.RecordName, cluster.PrimaryDNS.RecordType, detail.MasterIP, cluster.PrimaryDNS.TTL)

	if cluster.ReplicaDNS != nil && len(detail.Slaves) > 0 {
		var slaveIPs []string
		for _, s := range detail.Slaves {
			if s.IP != detail.MasterIP {
				slaveIPs = append(slaveIPs, s.IP)
			}
		}
		if len(slaveIPs) > 0 {
			provider.UpdateRecordValues(ctx, cluster.ReplicaDNS.Zone, cluster.ReplicaDNS.RecordName, cluster.ReplicaDNS.RecordType, slaveIPs, cluster.ReplicaDNS.TTL)
		}
	}

	h.redirect(w, r, "/admin/clusters")
}

// === Sentinels ===

// Sentinels는 등록된 Sentinel Cluster 목록과 노드별 상태를 렌더링한다.
func (h *AdminHandler) Sentinels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sentinels, storeErr := h.store.ListSentinels(ctx, "")
	if storeErr != nil { slog.Warn("store error", "method", "ListSentinels", "error", storeErr) }

	groups := make(map[string][]*models.Sentinel)
	for _, s := range sentinels {
		groups[s.GroupName] = append(groups[s.GroupName], s)
	}

	// Use background health checker results.
	pingResults := h.healthCheck.GetAllStatuses()

	// Alert settings per group.
	rt, storeErr := h.store.GetRuntimeSettings(ctx)
	if storeErr != nil { slog.Warn("store error", "method", "GetRuntimeSettings", "error", storeErr) }
	alertSettings := make(map[string]bool)
	for grpName := range groups {
		alertSettings[grpName] = rt["sentinel_alert:"+grpName] == "true"
	}

	h.render(w, r, "base", PageData{
		Page: "sentinels",
		Data: map[string]any{
			"SentinelGroups": groups,
			"PingResults":    pingResults,
			"AlertSettings":  alertSettings,
		},
	})
}

// SentinelClusterCreate는 새 Sentinel Cluster와 노드 목록을 등록하는 요청을 처리한다.
func (h *AdminHandler) SentinelClusterCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	clusterName := strings.TrimSpace(r.FormValue("cluster_name"))
	sentinelIDs := r.Form["sentinel_ids"]
	sentinelHosts := r.Form["sentinel_hosts"]
	sentinelPorts := r.Form["sentinel_ports"]

	for i := range sentinelIDs {
		if i >= len(sentinelHosts) || i >= len(sentinelPorts) {
			break
		}
		sid := strings.TrimSpace(sentinelIDs[i])
		host := strings.TrimSpace(sentinelHosts[i])
		port, _ := strconv.Atoi(sentinelPorts[i])
		if port == 0 {
			port = 26379
		}

		if _, err := h.store.GetSentinel(ctx, sid); err != nil {
			sentinel := models.NewSentinel(sid, clusterName, host, port)
			h.store.SaveSentinel(ctx, sentinel)
		}
	}

	h.redirect(w, r, "/admin/sentinels")
}

// SentinelClusterDelete는 특정 Sentinel 그룹에 속한 모든 노드를 삭제하는 요청을 처리한다.
func (h *AdminHandler) SentinelClusterDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	grpName := r.PathValue("grpName")
	sentinels, storeErr := h.store.ListSentinels(ctx, grpName)
	if storeErr != nil { slog.Warn("store error", "method", "ListSentinels", "error", storeErr) }
	for _, s := range sentinels {
		h.store.DeleteSentinel(ctx, s.SentinelNodeName)
	}
	h.redirect(w, r, "/admin/sentinels")
}

// SentinelAddNode는 기존 Sentinel 그룹에 새 노드를 추가하는 요청을 처리한다.
func (h *AdminHandler) SentinelAddNode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	grpName := r.PathValue("grpName")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	nodeName := strings.TrimSpace(r.FormValue("sentinel_node_name"))
	host := strings.TrimSpace(r.FormValue("host"))
	port, _ := strconv.Atoi(r.FormValue("port"))
	if port == 0 {
		port = 26379
	}

	if _, err := h.store.GetSentinel(ctx, nodeName); err != nil {
		sentinel := models.NewSentinel(nodeName, grpName, host, port)
		h.store.SaveSentinel(ctx, sentinel)
	}
	h.redirect(w, r, "/admin/sentinels")
}

// SentinelDeleteNode는 특정 Sentinel 노드를 삭제하는 요청을 처리한다.
func (h *AdminHandler) SentinelDeleteNode(w http.ResponseWriter, r *http.Request) {
	nodeName := r.PathValue("nodeName")
	h.store.DeleteSentinel(r.Context(), nodeName)
	h.redirect(w, r, "/admin/sentinels")
}

// SentinelClusterEditSave는 Sentinel Cluster 수정 폼 제출을 처리하고 노드 정보와 알림 설정을 업데이트한다.
func (h *AdminHandler) SentinelClusterEditSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	grpName := r.PathValue("grpName")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	oldNames := r.Form["old_names"]
	nodeNames := r.Form["node_names"]
	hosts := r.Form["hosts"]
	ports := r.Form["ports"]

	for i := range oldNames {
		if i >= len(nodeNames) || i >= len(hosts) || i >= len(ports) {
			break
		}
		oldName := strings.TrimSpace(oldNames[i])
		newName := strings.TrimSpace(nodeNames[i])
		host := strings.TrimSpace(hosts[i])
		port, _ := strconv.Atoi(ports[i])
		if port == 0 {
			port = 26379
		}

		// Unregister old, register new.
		h.store.DeleteSentinel(ctx, oldName)
		sentinel := models.NewSentinel(newName, grpName, host, port)
		h.store.SaveSentinel(ctx, sentinel)
	}

	// Alert toggle.
	alertEnabled := r.FormValue("alert_enabled") == "on" || r.FormValue("alert_enabled") == "true"
	rt, storeErr := h.store.GetRuntimeSettings(ctx)
	if storeErr != nil { slog.Warn("store error", "method", "GetRuntimeSettings", "error", storeErr) }
	if rt == nil {
		rt = make(map[string]string)
	}
	if alertEnabled {
		rt["sentinel_alert:"+grpName] = "true"
	} else {
		delete(rt, "sentinel_alert:"+grpName)
	}
	h.store.SaveRuntimeSettings(ctx, rt)

	h.redirect(w, r, "/admin/sentinels")
}

// SentinelToggleAlert는 특정 Sentinel 그룹의 다운 알림 ON/OFF를 전환하는 요청을 처리한다.
func (h *AdminHandler) SentinelToggleAlert(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	grpName := r.PathValue("grpName")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	enabled := r.FormValue("enabled") == "true"
	rt, storeErr := h.store.GetRuntimeSettings(ctx)
	if storeErr != nil { slog.Warn("store error", "method", "GetRuntimeSettings", "error", storeErr) }
	if rt == nil {
		rt = make(map[string]string)
	}
	if enabled {
		rt["sentinel_alert:"+grpName] = "true"
	} else {
		delete(rt, "sentinel_alert:"+grpName)
	}
	h.store.SaveRuntimeSettings(ctx, rt)
	h.redirect(w, r, "/admin/sentinels")
}

// === Events ===

// Events는 페일오버 이벤트 로그 페이지를 렌더링한다. 타입 필터와 키워드 검색을 지원한다.
func (h *AdminHandler) Events(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	allEvents, storeErr := h.store.GetRecentEvents(ctx, 5000)
	if storeErr != nil { slog.Warn("store error", "method", "GetRecentEvents", "error", storeErr) }

	filterType := r.URL.Query().Get("type")
	searchQuery := strings.TrimSpace(r.URL.Query().Get("q"))

	if filterType != "" {
		var filtered []*models.FailoverEvent
		for _, e := range allEvents {
			if string(e.EventType) == filterType {
				filtered = append(filtered, e)
			}
		}
		allEvents = filtered
	}

	if searchQuery != "" {
		q := strings.ToLower(searchQuery)
		var filtered []*models.FailoverEvent
		for _, e := range allEvents {
			if strings.Contains(strings.ToLower(e.GroupName), q) ||
				strings.Contains(strings.ToLower(e.MasterName), q) ||
				strings.Contains(strings.ToLower(e.SentinelNodeName), q) ||
				strings.Contains(strings.ToLower(e.FromIP), q) ||
				strings.Contains(strings.ToLower(e.ToIP), q) {
				filtered = append(filtered, e)
			}
		}
		allEvents = filtered
	}

	// Pagination (35 per page).
	pageStr := r.URL.Query().Get("page")
	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}
	perPage := 35
	total := len(allEvents)
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * perPage
	end := start + perPage
	if end > total {
		end = total
	}
	events := allEvents[start:end]

	h.render(w, r, "base", PageData{
		Page: "events",
		Data: map[string]any{
			"Events":      events,
			"FilterType":  filterType,
			"SearchQuery": searchQuery,
			"CurrentPage": page,
			"TotalPages":  totalPages,
		},
	})
}

// === Settings: Server ===

// SettingsServer는 서버 설정 페이지를 렌더링한다. 저장소 연결 상태와 런타임 설정을 표시한다.
func (h *AdminHandler) SettingsServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rt, storeErr := h.store.GetRuntimeSettings(ctx)
	if storeErr != nil { slog.Warn("store error", "method", "GetRuntimeSettings", "error", storeErr) }
	if rt == nil {
		rt = make(map[string]string)
	}
	defaults := map[string]string{
		"event_dedup_window_seconds": "30",
		"quorum_threshold":           "2",
		"dns_default_ttl":            "3",
		"dns_retry_count":            "3",
		"dns_retry_base_delay":       "1.0",
		"sentinel_down_after_ms":     "5000",
		"sentinel_failover_timeout":  "30000",
		"language":                   "en",
		"sentinel_ping_interval":     "5",
		"client_kill_enabled":        "true",
	}
	for k, v := range defaults {
		if rt[k] == "" {
			rt[k] = v
		}
	}

	storeType := "VALKEY"
	if _, ok := h.store.(*store.MemoryStore); ok {
		storeType = "MEMORY"
	}

	h.render(w, r, "base", PageData{
		Page: "settings-server",
		Data: map[string]any{"RuntimeSettings": rt, "StoreType": storeType, "StoreConnected": true},
	})
}

// SettingsServerSave는 서버 런타임 설정 저장 요청을 처리하고 언어 설정을 즉시 반영한다.
func (h *AdminHandler) SettingsServerSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	settings := map[string]string{
		"event_dedup_window_seconds": r.FormValue("event_dedup_window_seconds"),
		"quorum_threshold":           r.FormValue("quorum_threshold"),
		"dns_default_ttl":            r.FormValue("dns_default_ttl"),
		"dns_retry_count":            r.FormValue("dns_retry_count"),
		"dns_retry_base_delay":       r.FormValue("dns_retry_base_delay"),
		"sentinel_down_after_ms":     r.FormValue("sentinel_down_after_ms"),
		"sentinel_failover_timeout":  r.FormValue("sentinel_failover_timeout"),
		"language":                   r.FormValue("language"),
		"sentinel_ping_interval":     r.FormValue("sentinel_ping_interval"),
	}
	// 체크박스: 미체크 시 빈 문자열 → "false"로 변환
	if r.FormValue("client_kill_enabled") == "true" {
		settings["client_kill_enabled"] = "true"
	} else {
		settings["client_kill_enabled"] = "false"
	}
	h.store.SaveRuntimeSettings(ctx, settings)

	if lang := settings["language"]; lang != "" {
		h.setLang(lang)
	}

	h.redirect(w, r, "/admin/settings/server")
}

// === Settings: DNS ===

// SettingsDNS는 DNS 프로바이더 설정 페이지를 렌더링한다. 등록된 프로바이더 목록과 연결 상태를 표시한다.
func (h *AdminHandler) SettingsDNS(w http.ResponseWriter, r *http.Request) {
	configs, storeErr := h.store.ListDNSProviderConfigs(r.Context())
	if storeErr != nil { slog.Warn("store error", "method", "ListDNSProviderConfigs", "error", storeErr) }

	dnsStatus := make(map[string]bool)
	for name, p := range h.dnsProviders {
		err := p.HealthCheck(r.Context())
		dnsStatus[name] = err == nil
	}

	h.render(w, r, "base", PageData{
		Page: "settings-dns",
		Data: map[string]any{
			"Configs":            configs,
			"DNSStatus":          dnsStatus,
			"AvailableProviders": dns.AvailableProviders(),
		},
	})
}

// DNSProviderCreate는 새 DNS 프로바이더 등록 요청을 처리하고 민감한 필드를 암호화하여 저장한다.
func (h *AdminHandler) DNSProviderCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}
	t := NewTranslator(h.getLang())

	providerName := strings.TrimSpace(r.FormValue("provider_name"))
	providerType := strings.TrimSpace(r.FormValue("provider_type"))

	if !dns.IsProviderAvailable(providerType) {
		h.render(w, r, "base", PageData{
			Page: "settings-dns", FlashMessage: t("flash_provider_not_available"), FlashType: "error",
			Data: map[string]any{"AvailableProviders": dns.AvailableProviders()},
		})
		return
	}

	cfg := map[string]string{"type": providerType}

	switch providerType {
	case "route53":
		zoneID := strings.TrimSpace(r.FormValue("r53_zone_id"))
		if zoneID == "" {
			h.render(w, r, "base", PageData{Page: "settings-dns", FlashMessage: t("flash_zone_id_required"), FlashType: "error"})
			return
		}
		cfg["zone_id"] = zoneID
		cfg["zone_name"] = strings.TrimSpace(r.FormValue("r53_zone_name"))
		cfg["region"] = strings.TrimSpace(r.FormValue("r53_region"))
		cfg["access_key"] = strings.TrimSpace(r.FormValue("r53_access_key"))
		cfg["secret_key"] = strings.TrimSpace(r.FormValue("r53_secret_key"))
	case "azure":
		subID := strings.TrimSpace(r.FormValue("az_subscription_id"))
		rg := strings.TrimSpace(r.FormValue("az_resource_group"))
		zn := strings.TrimSpace(r.FormValue("az_zone_name"))
		if subID == "" || rg == "" || zn == "" {
			h.render(w, r, "base", PageData{Page: "settings-dns", FlashMessage: t("flash_azure_required"), FlashType: "error"})
			return
		}
		cfg["subscription_id"] = subID
		cfg["resource_group"] = rg
		cfg["zone_name"] = zn
		cfg["auth_type"] = strings.TrimSpace(r.FormValue("az_auth_type"))
		cfg["client_id"] = strings.TrimSpace(r.FormValue("az_client_id"))
		cfg["client_secret"] = strings.TrimSpace(r.FormValue("az_client_secret"))
		cfg["tenant_id"] = strings.TrimSpace(r.FormValue("az_tenant_id"))
	case "restapi":
		baseURL := strings.TrimSpace(r.FormValue("rest_base_url"))
		updateMethod := strings.TrimSpace(r.FormValue("rest_update_method"))
		updateURL := strings.TrimSpace(r.FormValue("rest_update_url"))
		updateBody := strings.TrimSpace(r.FormValue("rest_update_body"))
		if baseURL == "" || updateMethod == "" || updateURL == "" || updateBody == "" {
			h.render(w, r, "base", PageData{
				Page: "settings-dns", FlashMessage: t("flash_restapi_required"), FlashType: "error",
				Data: map[string]any{"AvailableProviders": dns.AvailableProviders()},
			})
			return
		}
		cfg["base_url"] = baseURL
		cfg["headers"] = strings.TrimSpace(r.FormValue("rest_headers"))
		cfg["update_method"] = updateMethod
		cfg["update_url"] = updateURL
		cfg["update_body"] = updateBody
		cfg["update_multi_method"] = strings.TrimSpace(r.FormValue("rest_update_multi_method"))
		cfg["update_multi_url"] = strings.TrimSpace(r.FormValue("rest_update_multi_url"))
		cfg["update_multi_body"] = strings.TrimSpace(r.FormValue("rest_update_multi_body"))
		cfg["delete_method"] = strings.TrimSpace(r.FormValue("rest_delete_method"))
		cfg["delete_url"] = strings.TrimSpace(r.FormValue("rest_delete_url"))
		cfg["delete_body"] = strings.TrimSpace(r.FormValue("rest_delete_body"))
		cfg["health_method"] = strings.TrimSpace(r.FormValue("rest_health_method"))
		cfg["health_url"] = strings.TrimSpace(r.FormValue("rest_health_url"))
	case "cloudflare":
		apiToken := strings.TrimSpace(r.FormValue("cf_api_token"))
		zoneID := strings.TrimSpace(r.FormValue("cf_zone_id"))
		if apiToken == "" || zoneID == "" {
			h.render(w, r, "base", PageData{Page: "settings-dns", FlashMessage: t("flash_cloudflare_required"), FlashType: "error"})
			return
		}
		cfg["api_token"] = apiToken
		cfg["zone_id"] = zoneID
		cfg["zone_name"] = strings.TrimSpace(r.FormValue("cf_zone_name"))
	}

	encrypted := h.encryptor.EncryptAllFields(cfg)
	h.store.SaveDNSProviderConfig(ctx, providerName, encrypted)

	// Reload provider instance.
	decrypted := h.encryptor.DecryptAllFields(encrypted)
	if p, err := dns.NewProvider(ctx, decrypted["type"], decrypted); err == nil {
		h.dnsProviders[providerName] = p
	}

	h.redirect(w, r, "/admin/settings/dns")
}

// DNSProviderEditPage는 DNS 프로바이더 수정 폼 페이지를 렌더링한다.
func (h *AdminHandler) DNSProviderEditPage(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("providerName")
	cfg, err := h.store.GetDNSProviderConfig(r.Context(), providerName)
	if err != nil {
		h.redirect(w, r, "/admin/settings/dns")
		return
	}
	h.render(w, r, "base", PageData{
		Page: "settings-dns-edit",
		Data: map[string]any{
			"ProviderName":   providerName,
			"ProviderType":   cfg["type"],
			"ZoneID":         cfg["zone_id"],
			"ZoneName":       cfg["zone_name"],
			"Region":         cfg["region"],
			"SubscriptionID": cfg["subscription_id"],
			"ResourceGroup":  cfg["resource_group"],
			"AuthType":       cfg["auth_type"],
			"APIURL":         cfg["api_url"],
			"Config":         cfg,
		},
	})
}

// DNSProviderEditSave는 DNS 프로바이더 수정 폼 제출을 처리하고 설정을 암호화하여 저장한다.
func (h *AdminHandler) DNSProviderEditSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	providerName := r.PathValue("providerName")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	providerType := strings.TrimSpace(r.FormValue("provider_type"))
	cfg := map[string]string{"type": providerType}

	switch providerType {
	case "route53":
		cfg["zone_id"] = strings.TrimSpace(r.FormValue("r53_zone_id"))
		cfg["zone_name"] = strings.TrimSpace(r.FormValue("r53_zone_name"))
		cfg["region"] = strings.TrimSpace(r.FormValue("r53_region"))
		cfg["access_key"] = strings.TrimSpace(r.FormValue("r53_access_key"))
		cfg["secret_key"] = strings.TrimSpace(r.FormValue("r53_secret_key"))
	case "azure":
		cfg["subscription_id"] = strings.TrimSpace(r.FormValue("az_subscription_id"))
		cfg["resource_group"] = strings.TrimSpace(r.FormValue("az_resource_group"))
		cfg["zone_name"] = strings.TrimSpace(r.FormValue("az_zone_name"))
		cfg["auth_type"] = strings.TrimSpace(r.FormValue("az_auth_type"))
		cfg["client_id"] = strings.TrimSpace(r.FormValue("az_client_id"))
		cfg["client_secret"] = strings.TrimSpace(r.FormValue("az_client_secret"))
		cfg["tenant_id"] = strings.TrimSpace(r.FormValue("az_tenant_id"))
	case "restapi":
		cfg["base_url"] = strings.TrimSpace(r.FormValue("rest_base_url"))
		cfg["headers"] = strings.TrimSpace(r.FormValue("rest_headers"))
		cfg["update_method"] = strings.TrimSpace(r.FormValue("rest_update_method"))
		cfg["update_url"] = strings.TrimSpace(r.FormValue("rest_update_url"))
		cfg["update_body"] = strings.TrimSpace(r.FormValue("rest_update_body"))
		cfg["update_multi_method"] = strings.TrimSpace(r.FormValue("rest_update_multi_method"))
		cfg["update_multi_url"] = strings.TrimSpace(r.FormValue("rest_update_multi_url"))
		cfg["update_multi_body"] = strings.TrimSpace(r.FormValue("rest_update_multi_body"))
		cfg["delete_method"] = strings.TrimSpace(r.FormValue("rest_delete_method"))
		cfg["delete_url"] = strings.TrimSpace(r.FormValue("rest_delete_url"))
		cfg["delete_body"] = strings.TrimSpace(r.FormValue("rest_delete_body"))
		cfg["health_method"] = strings.TrimSpace(r.FormValue("rest_health_method"))
		cfg["health_url"] = strings.TrimSpace(r.FormValue("rest_health_url"))
	case "cloudflare":
		cfg["api_token"] = strings.TrimSpace(r.FormValue("cf_api_token"))
		cfg["zone_id"] = strings.TrimSpace(r.FormValue("cf_zone_id"))
		cfg["zone_name"] = strings.TrimSpace(r.FormValue("cf_zone_name"))
	}

	encrypted := h.encryptor.EncryptAllFields(cfg)
	h.store.SaveDNSProviderConfig(ctx, providerName, encrypted)

	decrypted := h.encryptor.DecryptAllFields(encrypted)
	if p, err := dns.NewProvider(ctx, decrypted["type"], decrypted); err == nil {
		h.dnsProviders[providerName] = p
	}

	h.redirect(w, r, "/admin/settings/dns")
}

// DNSProviderDelete는 DNS 프로바이더 삭제 요청을 처리하고 인메모리 프로바이더 인스턴스도 제거한다.
func (h *AdminHandler) DNSProviderDelete(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("providerName")
	h.store.DeleteDNSProviderConfig(r.Context(), providerName)
	delete(h.dnsProviders, providerName)
	h.redirect(w, r, "/admin/settings/dns")
}

// === Settings: Token ===

func (h *AdminHandler) getAPITokens(ctx context.Context) map[string]string {
	rt, storeErr := h.store.GetRuntimeSettings(ctx)
	if storeErr != nil { slog.Warn("store error", "method", "GetRuntimeSettings", "error", storeErr) }
	tokens := make(map[string]string)
	if raw, ok := rt["api_tokens"]; ok && raw != "" {
		if err := json.Unmarshal([]byte(raw), &tokens); err != nil {
			slog.Warn("failed to parse api_tokens JSON", "error", err)
		}
	}
	// Backward compat: migrate single token.
	if len(tokens) == 0 {
		if single, _ := h.store.GetAPIToken(ctx); single != "" {
			tokens["default"] = single
		}
	}
	return tokens
}

func (h *AdminHandler) saveAPITokens(ctx context.Context, tokens map[string]string) {
	rt, storeErr := h.store.GetRuntimeSettings(ctx)
	if storeErr != nil { slog.Warn("store error", "method", "GetRuntimeSettings", "error", storeErr) }
	if rt == nil {
		rt = make(map[string]string)
	}
	data, _ := json.Marshal(tokens)
	rt["api_tokens"] = string(data)
	h.store.SaveRuntimeSettings(ctx, rt)
	// Also update single token for API auth backward compat (first token).
	if len(tokens) > 0 {
		for _, v := range tokens {
			h.store.SetAPIToken(ctx, v)
			return
		}
	} else {
		h.store.DeleteAPIToken(ctx)
	}
}

// SettingsToken은 API 토큰 관리 페이지를 렌더링한다.
func (h *AdminHandler) SettingsToken(w http.ResponseWriter, r *http.Request) {
	tokens := h.getAPITokens(r.Context())
	h.render(w, r, "base", PageData{
		Page: "settings-token",
		Data: map[string]any{"Tokens": tokens},
	})
}

// RegenerateToken은 새 API 토큰을 생성하거나 기존 토큰을 재생성하는 요청을 처리한다.
func (h *AdminHandler) RegenerateToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}
	t := NewTranslator(h.getLang())

	tokenName := strings.TrimSpace(r.FormValue("token_name"))
	if tokenName == "" {
		tokenName = "default"
	}

	tokens := h.getAPITokens(ctx)
	token, err := GenerateAPIToken()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	tokens[tokenName] = token
	h.saveAPITokens(ctx, tokens)

	h.render(w, r, "base", PageData{
		Page: "settings-token", FlashMessage: t("flash_token_created"), FlashType: "success",
		Data: map[string]any{"Tokens": tokens},
	})
}

// DeleteToken은 특정 API 토큰을 삭제하는 요청을 처리한다.
func (h *AdminHandler) DeleteToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}
	t := NewTranslator(h.getLang())

	tokenName := strings.TrimSpace(r.FormValue("token_name"))
	tokens := h.getAPITokens(ctx)
	delete(tokens, tokenName)
	h.saveAPITokens(ctx, tokens)

	h.render(w, r, "base", PageData{
		Page: "settings-token", FlashMessage: t("flash_token_deleted"), FlashType: "success",
		Data: map[string]any{"Tokens": tokens},
	})
}

// === Settings: Notification ===

// generateWebhookID는 웹훅 고유 ID를 생성한다.
func generateWebhookID() (string, error) {
	b := make([]byte, 8)
	if _, err := crypto_rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("wh_%s", hex.EncodeToString(b)), nil
}

// SettingsNotification은 알림 설정 페이지를 렌더링한다.
func (h *AdminHandler) SettingsNotification(w http.ResponseWriter, r *http.Request) {
	webhooks, storeErr := h.store.ListWebhooks(r.Context())
	if storeErr != nil { slog.Warn("store error", "method", "ListWebhooks", "error", storeErr) }
	h.render(w, r, "base", PageData{
		Page: "settings-notification",
		Data: map[string]any{"Webhooks": webhooks},
	})
}

// WebhookCreate는 새 웹훅 엔드포인트를 생성하여 저장한다.
func (h *AdminHandler) WebhookCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	id, err := generateWebhookID()
	if err != nil {
		http.Error(w, "failed to generate ID", http.StatusInternalServerError)
		return
	}

	whType := strings.TrimSpace(r.FormValue("type"))
	url := strings.TrimSpace(r.FormValue("url"))
	if whType == models.WebhookTypeKakaoWork {
		url = "https://api.kakaowork.com/v1/messages.send"
	}

	wh := &models.WebhookEndpoint{
		ID:             id,
		Name:           strings.TrimSpace(r.FormValue("name")),
		Type:           whType,
		URL:            url,
		Enabled:        true,
		Channel:        strings.TrimSpace(r.FormValue("channel")),
		AppKey:         strings.TrimSpace(r.FormValue("app_key")),
		ConversationID: strings.TrimSpace(r.FormValue("conversation_id")),
		PayloadMode:    r.FormValue("payload_mode"),
		BodyKey:        strings.TrimSpace(r.FormValue("body_key")),
	}

	// Parse custom headers
	headersJSON := strings.TrimSpace(r.FormValue("custom_headers"))
	if headersJSON != "" {
		var headers map[string]string
		if json.Unmarshal([]byte(headersJSON), &headers) == nil {
			wh.CustomHeaders = headers
		}
	}

	if err := h.store.SaveWebhook(ctx, wh); err != nil {
		slog.Error("save webhook failed", "error", err)
	}
	h.redirect(w, r, "/admin/settings/notification")
}

// WebhookEdit은 기존 웹훅 엔드포인트를 수정하여 저장한다.
func (h *AdminHandler) WebhookEdit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	wh, err := h.store.GetWebhook(ctx, id)
	if err != nil {
		h.redirect(w, r, "/admin/settings/notification")
		return
	}

	wh.Name = strings.TrimSpace(r.FormValue("name"))
	whType := strings.TrimSpace(r.FormValue("type"))
	wh.Type = whType
	if whType == models.WebhookTypeKakaoWork {
		wh.URL = "https://api.kakaowork.com/v1/messages.send"
	} else {
		wh.URL = strings.TrimSpace(r.FormValue("url"))
	}
	wh.Channel = strings.TrimSpace(r.FormValue("channel"))
	wh.AppKey = strings.TrimSpace(r.FormValue("app_key"))
	wh.ConversationID = strings.TrimSpace(r.FormValue("conversation_id"))
	wh.PayloadMode = r.FormValue("payload_mode")
	wh.BodyKey = strings.TrimSpace(r.FormValue("body_key"))

	headersJSON := strings.TrimSpace(r.FormValue("custom_headers"))
	if headersJSON != "" {
		var headers map[string]string
		if json.Unmarshal([]byte(headersJSON), &headers) == nil {
			wh.CustomHeaders = headers
		}
	} else {
		wh.CustomHeaders = nil
	}

	if err := h.store.SaveWebhook(ctx, wh); err != nil {
		slog.Error("save webhook failed", "error", err)
	}
	h.redirect(w, r, "/admin/settings/notification")
}

// WebhookDelete는 웹훅 엔드포인트를 삭제한다.
func (h *AdminHandler) WebhookDelete(w http.ResponseWriter, r *http.Request) {
	h.store.DeleteWebhook(r.Context(), r.PathValue("id"))
	h.redirect(w, r, "/admin/settings/notification")
}

// WebhookTest는 지정된 웹훅 엔드포인트로 테스트 알림을 전송한다.
func (h *AdminHandler) WebhookTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t := NewTranslator(h.getLang())
	wh, err := h.store.GetWebhook(ctx, r.PathValue("id"))
	if err != nil {
		h.redirect(w, r, "/admin/settings/notification")
		return
	}

	webhooks, _ := h.store.ListWebhooks(ctx)
	testErr := core.SendTestNotification(ctx, wh)
	msg := t("flash_webhook_test_sent")
	flashType := "success"
	if testErr != nil {
		msg = t("flash_webhook_test_failed") + ": " + testErr.Error()
		flashType = "error"
	}
	h.render(w, r, "base", PageData{
		Page: "settings-notification", FlashMessage: msg, FlashType: flashType,
		Data: map[string]any{"Webhooks": webhooks},
	})
}

// WebhookToggle은 웹훅 엔드포인트의 활성화 상태를 토글한다.
func (h *AdminHandler) WebhookToggle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wh, err := h.store.GetWebhook(ctx, r.PathValue("id"))
	if err != nil {
		h.redirect(w, r, "/admin/settings/notification")
		return
	}
	wh.Enabled = !wh.Enabled
	h.store.SaveWebhook(ctx, wh)
	h.redirect(w, r, "/admin/settings/notification")
}

// === Settings: Account ===

// SettingsAccount는 관리자 계정 설정 페이지를 렌더링한다. 기본 비밀번호 사용 여부를 표시한다.
func (h *AdminHandler) SettingsAccount(w http.ResponseWriter, r *http.Request) {
	isDefault := h.session.IsDefaultPassword()
	h.render(w, r, "base", PageData{
		Page: "settings-account",
		Data: map[string]any{"IsDefaultPassword": isDefault},
	})
}

// SettingsAccountSave은 관리자 비밀번호 변경 요청을 처리한다. 현재 비밀번호 검증 후 새 비밀번호를 저장한다.
func (h *AdminHandler) SettingsAccountSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}
	t := NewTranslator(h.getLang())
	currentPw := r.FormValue("current_password")
	newPw := r.FormValue("new_password")
	confirmPw := r.FormValue("confirm_password")

	hash, storeErr := h.store.GetAdminPasswordHash(r.Context())
	if storeErr != nil { slog.Warn("store error", "method", "GetAdminPasswordHash", "error", storeErr) }
	var currentOK bool
	if hash == "" {
		currentOK = currentPw == defaultPassword
	} else {
		currentOK = VerifyHash(currentPw, hash)
	}

	if !currentOK {
		h.render(w, r, "base", PageData{Page: "settings-account", FlashMessage: t("flash_pw_wrong"), FlashType: "error"})
		return
	}
	if newPw != confirmPw {
		h.render(w, r, "base", PageData{Page: "settings-account", FlashMessage: t("flash_pw_mismatch"), FlashType: "error"})
		return
	}
	if len(newPw) < 4 {
		h.render(w, r, "base", PageData{Page: "settings-account", FlashMessage: t("flash_pw_too_short"), FlashType: "error"})
		return
	}

	hash, err := HashPassword(newPw)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.store.SetAdminPasswordHash(r.Context(), hash)
	h.render(w, r, "base", PageData{Page: "settings-account", FlashMessage: t("flash_pw_changed"), FlashType: "success"})
}

// === Helpers ===

func intFromMap(m map[string]string, key string, fallback int) int {
	if v, ok := m[key]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// existingMastersByGroup는 클러스터 목록에서 센티널 그룹별 마스터 이름 목록을 반환한다.
func existingMastersByGroup(clusters []*models.Cluster) map[string][]string {
	result := make(map[string][]string)
	for _, c := range clusters {
		result[c.GroupName] = append(result[c.GroupName], c.MasterName)
	}
	return result
}

