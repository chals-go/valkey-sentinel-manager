package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chals-go/valkey-sentinel-manager/internal/core"
	"github.com/chals-go/valkey-sentinel-manager/internal/dns"
	"github.com/chals-go/valkey-sentinel-manager/internal/models"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
)

// AdminHandler holds dependencies for the admin web UI.
type AdminHandler struct {
	store        store.Store
	session      *SessionManager
	tmpl         *template.Template
	lang         string
	dnsProviders map[string]dns.Provider
	encryptor    *Encryptor
	healthCheck  *core.SentinelHealthChecker
}

// NewAdminHandler creates an AdminHandler.
func NewAdminHandler(s store.Store, sm *SessionManager, tmpl *template.Template, lang string, providers map[string]dns.Provider, enc *Encryptor, hc *core.SentinelHealthChecker) *AdminHandler {
	return &AdminHandler{store: s, session: sm, tmpl: tmpl, lang: lang, dnsProviders: providers, encryptor: enc, healthCheck: hc}
}

// PageData is the common template data structure.
type PageData struct {
	Page         string
	HideSidebar  bool
	FlashMessage string
	FlashType    string
	Data         map[string]any
}

func (h *AdminHandler) render(w http.ResponseWriter, name string, data PageData) {
	t := NewTranslator(h.lang)
	funcMap := template.FuncMap{
		"t": t,
		"isMonitoringPage": func(page string) bool {
			return page == "dashboard" || page == "clusters" || page == "sentinels" || page == "events"
		},
	}
	tmpl, err := h.tmpl.Clone()
	if err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	tmpl.Funcs(funcMap)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *AdminHandler) redirect(w http.ResponseWriter, r *http.Request, url string) {
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// RegisterRoutes registers all admin web routes on the given mux.
func (h *AdminHandler) RegisterRoutes(mux *http.ServeMux) {
	// Login (no auth required).
	mux.HandleFunc("GET /admin/login", h.LoginPage)
	mux.HandleFunc("POST /admin/login", h.LoginSubmit)

	auth := h.session.RequireAuth

	// Dashboard
	mux.Handle("GET /admin/", auth(http.HandlerFunc(h.Dashboard)))
	mux.Handle("POST /admin/logout", auth(http.HandlerFunc(h.Logout)))

	// Clusters
	mux.Handle("GET /admin/clusters", auth(http.HandlerFunc(h.Clusters)))
	mux.Handle("GET /admin/clusters/new", auth(http.HandlerFunc(h.ClusterFormPage)))
	mux.Handle("POST /admin/clusters/new", auth(http.HandlerFunc(h.ClusterCreate)))
	mux.Handle("GET /admin/clusters/{masterName}/edit", auth(http.HandlerFunc(h.ClusterEditPage)))
	mux.Handle("POST /admin/clusters/{masterName}/edit", auth(http.HandlerFunc(h.ClusterEditSubmit)))
	mux.Handle("POST /admin/clusters/{masterName}/delete", auth(http.HandlerFunc(h.ClusterDelete)))
	mux.Handle("POST /admin/clusters/{masterName}/pause", auth(http.HandlerFunc(h.ClusterPause)))
	mux.Handle("POST /admin/clusters/{masterName}/resume", auth(http.HandlerFunc(h.ClusterResume)))
	mux.Handle("POST /admin/clusters/{masterName}/test-failover", auth(http.HandlerFunc(h.ClusterTestFailover)))
	mux.Handle("POST /admin/clusters/{masterName}/sync-dns", auth(http.HandlerFunc(h.ClusterSyncDNS)))

	// Sentinels
	mux.Handle("GET /admin/sentinels", auth(http.HandlerFunc(h.Sentinels)))
	mux.Handle("POST /admin/sentinels/new-cluster", auth(http.HandlerFunc(h.SentinelClusterCreate)))
	mux.Handle("POST /admin/sentinels/{grpName}/delete-cluster", auth(http.HandlerFunc(h.SentinelClusterDelete)))
	mux.Handle("POST /admin/sentinels/{grpName}/edit", auth(http.HandlerFunc(h.SentinelClusterEditSubmit)))
	mux.Handle("POST /admin/sentinels/{grpName}/toggle-alert", auth(http.HandlerFunc(h.SentinelToggleAlert)))
	mux.Handle("POST /admin/sentinels/{grpName}/add-node", auth(http.HandlerFunc(h.SentinelAddNode)))
	mux.Handle("POST /admin/sentinels/{nodeName}/delete-node", auth(http.HandlerFunc(h.SentinelDeleteNode)))

	// Events
	mux.Handle("GET /admin/events", auth(http.HandlerFunc(h.Events)))

	// Settings redirect
	mux.Handle("GET /admin/settings", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.redirect(w, r, "/admin/settings/server")
	})))
	mux.Handle("GET /admin/settings/server", auth(http.HandlerFunc(h.SettingsServer)))
	mux.Handle("POST /admin/settings/server", auth(http.HandlerFunc(h.SettingsServerSave)))
	mux.Handle("GET /admin/settings/dns", auth(http.HandlerFunc(h.SettingsDNS)))
	mux.Handle("POST /admin/dns-provider/new", auth(http.HandlerFunc(h.DNSProviderCreate)))
	mux.Handle("GET /admin/settings/dns/edit/{providerName}", auth(http.HandlerFunc(h.DNSProviderEditPage)))
	mux.Handle("POST /admin/settings/dns/edit/{providerName}", auth(http.HandlerFunc(h.DNSProviderEditSubmit)))
	mux.Handle("POST /admin/dns-provider/{providerName}/delete", auth(http.HandlerFunc(h.DNSProviderDelete)))
	mux.Handle("GET /admin/settings/token", auth(http.HandlerFunc(h.SettingsToken)))
	mux.Handle("POST /admin/regenerate-token", auth(http.HandlerFunc(h.RegenerateToken)))
	mux.Handle("POST /admin/delete-token", auth(http.HandlerFunc(h.DeleteToken)))
	mux.Handle("GET /admin/settings/slack", auth(http.HandlerFunc(h.SettingsSlack)))
	mux.Handle("POST /admin/slack-webhook", auth(http.HandlerFunc(h.SlackWebhookSave)))
	mux.Handle("POST /admin/slack-webhook/delete", auth(http.HandlerFunc(h.SlackWebhookDelete)))
	mux.Handle("POST /admin/slack-webhook/test", auth(http.HandlerFunc(h.SlackWebhookTest)))
	mux.Handle("GET /admin/settings/account", auth(http.HandlerFunc(h.SettingsAccount)))
	mux.Handle("POST /admin/settings/account", auth(http.HandlerFunc(h.SettingsAccountSubmit)))
}

// === Login / Logout ===

func (h *AdminHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if h.session.ValidateSession(r) {
		h.redirect(w, r, "/admin/")
		return
	}
	h.render(w, "base", PageData{Page: "login", HideSidebar: true})
}

func (h *AdminHandler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if h.session.IsLoginLocked(ip) {
		t := NewTranslator(h.lang)
		h.render(w, "base", PageData{Page: "login", HideSidebar: true, FlashMessage: t("flash_login_locked"), FlashType: "error"})
		return
	}

	r.ParseForm()
	password := r.FormValue("password")

	hash, _ := h.store.GetAdminPasswordHash(context.Background())
	var ok bool
	if hash == "" {
		ok = password == defaultPassword
	} else {
		ok = VerifyHash(password, hash)
	}

	if !ok {
		h.session.RecordLoginFailure(ip)
		t := NewTranslator(h.lang)
		h.render(w, "base", PageData{Page: "login", HideSidebar: true, FlashMessage: t("flash_login_failed"), FlashType: "error"})
		return
	}

	h.session.ClearLoginFailures(ip)
	sid := h.session.CreateSession()
	h.session.SetSessionCookie(w, sid)
	h.redirect(w, r, "/admin/")
}

func (h *AdminHandler) Logout(w http.ResponseWriter, r *http.Request) {
	h.session.DestroySession(r)
	h.session.ClearSessionCookie(w)
	h.redirect(w, r, "/admin/login")
}

// === Dashboard ===

func (h *AdminHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Load language from runtime settings.
	if rt, err := h.store.GetRuntimeSettings(ctx); err == nil {
		if l, ok := rt["language"]; ok && l != "" {
			h.lang = l
		}
	}

	clusters, _ := h.store.ListClusters(ctx)
	events, _ := h.store.GetRecentEvents(ctx, 500)
	sentinels, _ := h.store.ListSentinels(ctx, "")

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

	h.render(w, "base", PageData{
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

func (h *AdminHandler) Clusters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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

	h.render(w, "base", PageData{
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
		},
	})
}

func (h *AdminHandler) ClusterFormPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
	h.render(w, "base", PageData{
		Page: "cluster-form",
		Data: map[string]any{
			"SentinelClusterNames": sortedKeys(sentinelClusterNames),
			"DNSProviders":         dnsProvidersWithZone,
		},
	})
}

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
	dnsConfigs, _ := h.store.ListDNSProviderConfigs(ctx)
	dnsProvidersWithZone := make(map[string]string)
	for name, cfg := range dnsConfigs {
		dnsProvidersWithZone[name] = cfg["zone_name"]
	}

	h.render(w, "base", PageData{
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

func (h *AdminHandler) ClusterCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()

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
	dnsTTL, _ := strconv.Atoi(r.FormValue("dns_ttl"))
	if dnsTTL == 0 {
		dnsTTL = 3
	}
	quorumMode := r.FormValue("quorum_mode") == "on" || r.FormValue("quorum_mode") == "true"
	createReplicaDNS := r.FormValue("create_replica_dns") == "on" || r.FormValue("create_replica_dns") == "true"

	// Check duplicate.
	if _, err := h.store.GetCluster(ctx, monitoringName); err == nil {
		h.redirect(w, r, "/admin/clusters")
		return
	}

	// Get sentinel addrs from cluster.
	sents, _ := h.store.ListSentinels(ctx, sentinelCluster)
	var sentinelAddrs []string
	var sentinelPassword string
	for _, s := range sents {
		sentinelAddrs = append(sentinelAddrs, fmt.Sprintf("%s:%d", s.Host, s.Port))
	}

	// Get zone_name from DNS provider config.
	dnsCfg, _ := h.store.GetDNSProviderConfig(ctx, dnsProvider)
	zoneName := ""
	if dnsCfg != nil {
		zoneName = dnsCfg["zone_name"]
	}

	primaryDNS := models.DNSMapping{Zone: zoneName, RecordName: fmt.Sprintf("primary-%s", monitoringName), RecordType: "A", TTL: dnsTTL}

	cluster := &models.Cluster{
		GroupName:        sentinelCluster,
		MasterName:       monitoringName,
		SentinelAddrs:    sentinelAddrs,
		DNSProvider:      dnsProvider,
		PrimaryIP:        primaryIP,
		PrimaryPort:      primaryPort,
		PrimaryDNS:       primaryDNS,
		RedisPassword:    redisPassword,
		SentinelPassword: sentinelPassword,
		QuorumMode:       quorumMode,
		QuorumThreshold:  quorumThreshold,
	}
	h.store.RegisterCluster(ctx, cluster)

	// Sentinel MONITOR.
	rt, _ := h.store.GetRuntimeSettings(ctx)
	downMs := intFromMap(rt, "sentinel_down_after_ms", 5000)
	failTimeout := intFromMap(rt, "sentinel_failover_timeout", 30000)

	core.SentinelMonitor(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.PrimaryIP, cluster.PrimaryPort, cluster.QuorumThreshold, cluster.RedisPassword, cluster.SentinelPassword, downMs, failTimeout)

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
			h.store.RegisterCluster(ctx, cluster)
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

func (h *AdminHandler) ClusterEditSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	masterName := r.PathValue("masterName")
	r.ParseForm()

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

	cluster.PrimaryDNS.TTL = dnsTTL
	if cluster.ReplicaDNS != nil {
		cluster.ReplicaDNS.TTL = dnsTTL
	}
	h.store.RegisterCluster(ctx, cluster)

	core.SentinelSetConfig(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword, downAfterMs, failoverTimeout)

	h.redirect(w, r, "/admin/clusters")
}

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
	h.store.UnregisterCluster(ctx, masterName)
	h.redirect(w, r, "/admin/clusters")
}

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
		rt, _ := h.store.GetRuntimeSettings(ctx)
		cluster.PausedDownAfterMs = intFromMap(rt, "sentinel_down_after_ms", 5000)
	}
	cluster.IsPaused = true
	h.store.RegisterCluster(ctx, cluster)

	core.SentinelSetConfig(ctx, cluster.SentinelAddrs, cluster.MasterName, cluster.SentinelPassword, 7200000, 30000)
	h.redirect(w, r, "/admin/clusters")
}

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
	h.store.RegisterCluster(ctx, cluster)
	h.redirect(w, r, "/admin/clusters")
}

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

func (h *AdminHandler) Sentinels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sentinels, _ := h.store.ListSentinels(ctx, "")

	groups := make(map[string][]*models.Sentinel)
	for _, s := range sentinels {
		groups[s.GroupName] = append(groups[s.GroupName], s)
	}

	// Use background health checker results.
	pingResults := h.healthCheck.GetAllStatuses()

	// Alert settings per group.
	rt, _ := h.store.GetRuntimeSettings(ctx)
	alertSettings := make(map[string]bool)
	for grpName := range groups {
		alertSettings[grpName] = rt["sentinel_alert:"+grpName] == "true"
	}

	h.render(w, "base", PageData{
		Page: "sentinels",
		Data: map[string]any{
			"SentinelGroups": groups,
			"PingResults":    pingResults,
			"AlertSettings":  alertSettings,
		},
	})
}

func (h *AdminHandler) SentinelClusterCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()

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
			h.store.RegisterSentinel(ctx, sentinel)
		}
	}

	h.redirect(w, r, "/admin/sentinels")
}

func (h *AdminHandler) SentinelClusterDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	grpName := r.PathValue("grpName")
	sentinels, _ := h.store.ListSentinels(ctx, grpName)
	for _, s := range sentinels {
		h.store.UnregisterSentinel(ctx, s.SentinelNodeName)
	}
	h.redirect(w, r, "/admin/sentinels")
}

func (h *AdminHandler) SentinelAddNode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	grpName := r.PathValue("grpName")
	r.ParseForm()

	nodeName := strings.TrimSpace(r.FormValue("sentinel_node_name"))
	host := strings.TrimSpace(r.FormValue("host"))
	port, _ := strconv.Atoi(r.FormValue("port"))
	if port == 0 {
		port = 26379
	}

	if _, err := h.store.GetSentinel(ctx, nodeName); err != nil {
		sentinel := models.NewSentinel(nodeName, grpName, host, port)
		h.store.RegisterSentinel(ctx, sentinel)
	}
	h.redirect(w, r, "/admin/sentinels")
}

func (h *AdminHandler) SentinelDeleteNode(w http.ResponseWriter, r *http.Request) {
	nodeName := r.PathValue("nodeName")
	h.store.UnregisterSentinel(r.Context(), nodeName)
	h.redirect(w, r, "/admin/sentinels")
}

func (h *AdminHandler) SentinelClusterEditSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	grpName := r.PathValue("grpName")
	r.ParseForm()

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
		h.store.UnregisterSentinel(ctx, oldName)
		sentinel := models.NewSentinel(newName, grpName, host, port)
		h.store.RegisterSentinel(ctx, sentinel)
	}

	// Alert toggle.
	alertEnabled := r.FormValue("alert_enabled") == "on" || r.FormValue("alert_enabled") == "true"
	rt, _ := h.store.GetRuntimeSettings(ctx)
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

func (h *AdminHandler) SentinelToggleAlert(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	grpName := r.PathValue("grpName")
	r.ParseForm()

	enabled := r.FormValue("enabled") == "true"
	rt, _ := h.store.GetRuntimeSettings(ctx)
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

func (h *AdminHandler) Events(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	allEvents, _ := h.store.GetRecentEvents(ctx, 5000)

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

	h.render(w, "base", PageData{
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

func (h *AdminHandler) SettingsServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rt, _ := h.store.GetRuntimeSettings(ctx)
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

	h.render(w, "base", PageData{
		Page: "settings-server",
		Data: map[string]any{"RuntimeSettings": rt, "StoreType": storeType, "StoreConnected": true},
	})
}

func (h *AdminHandler) SettingsServerSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()

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
	h.store.SaveRuntimeSettings(ctx, settings)

	if lang := settings["language"]; lang != "" {
		h.lang = lang
	}

	h.redirect(w, r, "/admin/settings/server")
}

// === Settings: DNS ===

func (h *AdminHandler) SettingsDNS(w http.ResponseWriter, r *http.Request) {
	configs, _ := h.store.ListDNSProviderConfigs(r.Context())

	dnsStatus := make(map[string]bool)
	for name, p := range h.dnsProviders {
		err := p.HealthCheck(r.Context())
		dnsStatus[name] = err == nil
	}

	h.render(w, "base", PageData{
		Page: "settings-dns",
		Data: map[string]any{"Configs": configs, "DNSStatus": dnsStatus},
	})
}

func (h *AdminHandler) DNSProviderCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()
	t := NewTranslator(h.lang)

	providerName := strings.TrimSpace(r.FormValue("provider_name"))
	providerType := strings.TrimSpace(r.FormValue("provider_type"))

	cfg := map[string]string{"type": providerType}

	switch providerType {
	case "route53":
		zoneID := strings.TrimSpace(r.FormValue("r53_zone_id"))
		if zoneID == "" {
			h.render(w, "base", PageData{Page: "settings-dns", FlashMessage: t("flash_zone_id_required"), FlashType: "error"})
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
			h.render(w, "base", PageData{Page: "settings-dns", FlashMessage: t("flash_azure_required"), FlashType: "error"})
			return
		}
		cfg["subscription_id"] = subID
		cfg["resource_group"] = rg
		cfg["zone_name"] = zn
		cfg["auth_type"] = strings.TrimSpace(r.FormValue("az_auth_type"))
		cfg["client_id"] = strings.TrimSpace(r.FormValue("az_client_id"))
		cfg["client_secret"] = strings.TrimSpace(r.FormValue("az_client_secret"))
		cfg["tenant_id"] = strings.TrimSpace(r.FormValue("az_tenant_id"))
	case "bind":
		apiURL := strings.TrimSpace(r.FormValue("bind_api_url"))
		if apiURL == "" {
			h.render(w, "base", PageData{Page: "settings-dns", FlashMessage: t("flash_api_url_required"), FlashType: "error"})
			return
		}
		cfg["api_url"] = apiURL
		cfg["zone_name"] = strings.TrimSpace(r.FormValue("bind_zone_name"))
		cfg["api_key"] = strings.TrimSpace(r.FormValue("bind_api_key"))
	}

	encrypted := h.encryptor.EncryptSensitiveFields(cfg)
	h.store.SaveDNSProviderConfig(ctx, providerName, encrypted)

	// Reload provider instance.
	decrypted := h.encryptor.DecryptSensitiveFields(encrypted)
	if p, err := dns.NewProvider(ctx, decrypted["type"], decrypted); err == nil {
		h.dnsProviders[providerName] = p
	}

	h.redirect(w, r, "/admin/settings/dns")
}

func (h *AdminHandler) DNSProviderEditPage(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("providerName")
	cfg, err := h.store.GetDNSProviderConfig(r.Context(), providerName)
	if err != nil {
		h.redirect(w, r, "/admin/settings/dns")
		return
	}
	h.render(w, "base", PageData{
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
		},
	})
}

func (h *AdminHandler) DNSProviderEditSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	providerName := r.PathValue("providerName")
	r.ParseForm()

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
	case "bind":
		cfg["api_url"] = strings.TrimSpace(r.FormValue("bind_api_url"))
		cfg["zone_name"] = strings.TrimSpace(r.FormValue("bind_zone_name"))
		cfg["api_key"] = strings.TrimSpace(r.FormValue("bind_api_key"))
	}

	encrypted := h.encryptor.EncryptSensitiveFields(cfg)
	h.store.SaveDNSProviderConfig(ctx, providerName, encrypted)

	decrypted := h.encryptor.DecryptSensitiveFields(encrypted)
	if p, err := dns.NewProvider(ctx, decrypted["type"], decrypted); err == nil {
		h.dnsProviders[providerName] = p
	}

	h.redirect(w, r, "/admin/settings/dns")
}

func (h *AdminHandler) DNSProviderDelete(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("providerName")
	h.store.DeleteDNSProviderConfig(r.Context(), providerName)
	delete(h.dnsProviders, providerName)
	h.redirect(w, r, "/admin/settings/dns")
}

// === Settings: Token ===

func (h *AdminHandler) getAPITokens(ctx context.Context) map[string]string {
	rt, _ := h.store.GetRuntimeSettings(ctx)
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
	rt, _ := h.store.GetRuntimeSettings(ctx)
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

func (h *AdminHandler) SettingsToken(w http.ResponseWriter, r *http.Request) {
	tokens := h.getAPITokens(r.Context())
	h.render(w, "base", PageData{
		Page: "settings-token",
		Data: map[string]any{"Tokens": tokens},
	})
}

func (h *AdminHandler) RegenerateToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()
	t := NewTranslator(h.lang)

	tokenName := strings.TrimSpace(r.FormValue("token_name"))
	if tokenName == "" {
		tokenName = "default"
	}

	tokens := h.getAPITokens(ctx)
	tokens[tokenName] = GenerateAPIToken()
	h.saveAPITokens(ctx, tokens)

	h.render(w, "base", PageData{
		Page: "settings-token", FlashMessage: t("flash_token_created"), FlashType: "success",
		Data: map[string]any{"Tokens": tokens},
	})
}

func (h *AdminHandler) DeleteToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()
	t := NewTranslator(h.lang)

	tokenName := strings.TrimSpace(r.FormValue("token_name"))
	tokens := h.getAPITokens(ctx)
	delete(tokens, tokenName)
	h.saveAPITokens(ctx, tokens)

	h.render(w, "base", PageData{
		Page: "settings-token", FlashMessage: t("flash_token_deleted"), FlashType: "success",
		Data: map[string]any{"Tokens": tokens},
	})
}

// === Settings: Slack ===

func (h *AdminHandler) SettingsSlack(w http.ResponseWriter, r *http.Request) {
	webhook, _ := h.store.GetSlackWebhookURL(r.Context())
	channel, _ := h.store.GetSlackChannel(r.Context())
	h.render(w, "base", PageData{
		Page: "settings-slack",
		Data: map[string]any{"WebhookURL": webhook, "Channel": channel},
	})
}

func (h *AdminHandler) SlackWebhookSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()
	t := NewTranslator(h.lang)

	webhookURL := strings.TrimSpace(r.FormValue("slack_webhook_url"))
	channel := strings.TrimSpace(r.FormValue("slack_channel"))

	if webhookURL == "" {
		h.render(w, "base", PageData{
			Page: "settings-slack", FlashMessage: t("flash_webhook_required"), FlashType: "error",
			Data: map[string]any{"WebhookURL": "", "Channel": channel},
		})
		return
	}

	h.store.SetSlackWebhookURL(ctx, webhookURL)
	if channel != "" {
		h.store.SetSlackChannel(ctx, channel)
	}

	h.render(w, "base", PageData{
		Page: "settings-slack", FlashMessage: t("flash_slack_saved"), FlashType: "success",
		Data: map[string]any{"WebhookURL": webhookURL, "Channel": channel},
	})
}

func (h *AdminHandler) SlackWebhookDelete(w http.ResponseWriter, r *http.Request) {
	t := NewTranslator(h.lang)
	h.store.DeleteSlackWebhookURL(r.Context())
	h.render(w, "base", PageData{
		Page: "settings-slack", FlashMessage: t("flash_slack_disabled"), FlashType: "success",
		Data: map[string]any{"WebhookURL": "", "Channel": ""},
	})
}

func (h *AdminHandler) SlackWebhookTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t := NewTranslator(h.lang)

	webhookURL, _ := h.store.GetSlackWebhookURL(ctx)
	if webhookURL == "" {
		h.render(w, "base", PageData{
			Page: "settings-slack", FlashMessage: t("flash_slack_no_url"), FlashType: "error",
			Data: map[string]any{"WebhookURL": "", "Channel": ""},
		})
		return
	}

	channel, _ := h.store.GetSlackChannel(ctx)

	testEvent := &models.FailoverEvent{
		GroupName: "test-group", MasterName: "mymaster", EventType: models.EventTypeFailover,
		Role: "leader", State: "promoted",
		FromIP: "10.0.0.1", FromPort: 6379, ToIP: "10.0.0.2", ToPort: 6379,
		SentinelNodeName: "test-sentinel",
		Timestamp:        float64(time.Now().Unix()),
	}
	testCluster := &models.Cluster{
		GroupName: "test-group", MasterName: "mymaster",
		DNSProvider: "test",
		PrimaryDNS:  models.DNSMapping{Zone: "example.com", RecordName: "primary.valkey", RecordType: "A", TTL: 3},
	}

	success := core.SendSlackNotification(ctx, webhookURL, testEvent, testCluster, channel)
	msg := t("flash_slack_test_sent")
	flashType := "success"
	if !success {
		msg = t("flash_slack_test_failed")
		flashType = "error"
	}

	h.render(w, "base", PageData{
		Page: "settings-slack", FlashMessage: msg, FlashType: flashType,
		Data: map[string]any{"WebhookURL": webhookURL, "Channel": channel},
	})
}

// === Settings: Account ===

func (h *AdminHandler) SettingsAccount(w http.ResponseWriter, r *http.Request) {
	isDefault := h.session.IsDefaultPassword()
	h.render(w, "base", PageData{
		Page: "settings-account",
		Data: map[string]any{"IsDefaultPassword": isDefault},
	})
}

func (h *AdminHandler) SettingsAccountSubmit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	t := NewTranslator(h.lang)
	currentPw := r.FormValue("current_password")
	newPw := r.FormValue("new_password")
	confirmPw := r.FormValue("confirm_password")

	hash, _ := h.store.GetAdminPasswordHash(r.Context())
	var currentOK bool
	if hash == "" {
		currentOK = currentPw == defaultPassword
	} else {
		currentOK = VerifyHash(currentPw, hash)
	}

	if !currentOK {
		h.render(w, "base", PageData{Page: "settings-account", FlashMessage: t("flash_pw_wrong"), FlashType: "error"})
		return
	}
	if newPw != confirmPw {
		h.render(w, "base", PageData{Page: "settings-account", FlashMessage: t("flash_pw_mismatch"), FlashType: "error"})
		return
	}
	if len(newPw) < 4 {
		h.render(w, "base", PageData{Page: "settings-account", FlashMessage: t("flash_pw_too_short"), FlashType: "error"})
		return
	}

	h.store.SetAdminPasswordHash(r.Context(), HashPassword(newPw))
	h.render(w, "base", PageData{Page: "settings-account", FlashMessage: t("flash_pw_changed"), FlashType: "success"})
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

