// Package main is the entry point for the Sentinel Manager server.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	vsm "github.com/chals-go/valkey-sentinel-manager"
	"github.com/chals-go/valkey-sentinel-manager/internal/api"
	"github.com/chals-go/valkey-sentinel-manager/internal/models"
	"github.com/chals-go/valkey-sentinel-manager/internal/config"
	"github.com/chals-go/valkey-sentinel-manager/internal/core"
	"github.com/chals-go/valkey-sentinel-manager/internal/dns"
	"github.com/chals-go/valkey-sentinel-manager/internal/server"
	"github.com/chals-go/valkey-sentinel-manager/internal/store"
	"github.com/chals-go/valkey-sentinel-manager/internal/web"
)

const banner = `
  ____            _   _            _   __  __
 / ___|  ___ _ __|_|_(_)_ __   ___| | |  \/  | __ _ _ __   __ _  __ _  ___ _ __
 \___ \ / _ \ '_ \| __| | '_ \ / _ \ | | |\/| |/ _' | '_ \ / _' |/ _' |/ _ \ '__|
  ___) |  __/ | | | |_| | | | |  __/ | | |  | | (_| | | | | (_| | (_| |  __/ |
 |____/ \___|_| |_|\__|_|_| |_|\___|_| |_|  |_|\__,_|_| |_|\__,_|\__, |\___|_|
                                                                    |___/
  Valkey Sentinel DNS Failover Manager

`

const configHelp = `Configuration:
  YAML file (default: config.yaml). Override with --config flag.
  Environment variables (SMGR_ prefix) override YAML values.

  --config PATH     Path to config.yaml (default: config.yaml)

Example config.yaml:
  host: "0.0.0.0"
  port: 8000
  debug: false
  store_type: "valkey"              # "valkey" or "memory"

  # Store: Sentinel connection (recommended for production)
  store_sentinels: "10.0.0.1:26379,10.0.0.2:26379,10.0.0.3:26379"
  store_sentinel_master: "smgr-store"
  store_db: 0
  store_password: ""

  # Store: Direct connection (dev/test only, used when store_sentinels is empty)
  # store_url: "valkey://localhost:6379/0"

  event_dedup_window_seconds: 30
  quorum_threshold: 2
  log_dir: "/var/log/sentinel-manager"
  dns_default_ttl: 3
  dns_retry_count: 3
  dns_retry_base_delay: 1.0
  encryption_key: ""                # auto-generated on first run

Environment variables:
  SMGR_HOST, SMGR_PORT, SMGR_DEBUG, SMGR_SECURE_COOKIE,
  SMGR_STORE_TYPE, SMGR_STORE_URL, SMGR_STORE_SENTINELS,
  SMGR_STORE_SENTINEL_MASTER, SMGR_STORE_DB, SMGR_STORE_PASSWORD,
  SMGR_EVENT_DEDUP_WINDOW_SECONDS, SMGR_QUORUM_THRESHOLD,
  SMGR_LOG_DIR, SMGR_DNS_DEFAULT_TTL, SMGR_DNS_RETRY_COUNT,
  SMGR_DNS_RETRY_BASE_DELAY, SMGR_ENCRYPTION_KEY
`

func main() {
	configFile := flag.String("config", "config.yaml", "path to config.yaml file")
	showHelp := flag.Bool("help", false, "show help and config example")
	flag.BoolVar(showHelp, "h", false, "show help and config example")
	flag.Parse()

	if *showHelp {
		fmt.Print(banner)
		fmt.Print(configHelp)
		os.Exit(0)
	}

	fmt.Print(banner)

	cfg := config.Load(*configFile)

	// Configure slog level.
	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}

	// Log to stderr + file if log_dir exists or is configured.
	var writers []io.Writer
	writers = append(writers, os.Stderr)

	if cfg.LogDir != "" {
		if err := os.MkdirAll(cfg.LogDir, 0755); err == nil {
			logPath := cfg.LogDir + "/sentinel-manager.log"
			logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err == nil {
				writers = append(writers, logFile)
				defer logFile.Close()
				fmt.Printf("  Log file: %s\n\n", logPath)
			} else {
				fmt.Fprintf(os.Stderr, "  Warning: cannot open log file %s: %v\n\n", logPath, err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "  Warning: cannot create log dir %s: %v\n\n", cfg.LogDir, err)
		}
	}

	multiWriter := io.MultiWriter(writers...)
	slog.SetDefault(slog.New(slog.NewTextHandler(multiWriter, &slog.HandlerOptions{Level: level})))

	slog.Info("Sentinel Manager starting",
		"config", *configFile,
		"host", cfg.Host,
		"port", cfg.Port,
		"store", cfg.StoreType,
	)

	// Initialize store.
	var st store.Store
	ctx := context.Background()

	enc := web.NewEncryptor(cfg.EncryptionKey)

	switch cfg.StoreType {
	case "memory":
		st = store.NewMemoryStore(cfg.EventDedupWindowSeconds)
		slog.Info("using in-memory store")
	default:
		if cfg.StoreSentinels != "" {
			addrs := strings.Split(cfg.StoreSentinels, ",")
			var err error
			st, err = store.NewValkeyStoreSentinel(ctx, addrs, cfg.StoreSentinelMaster, cfg.StoreDB, cfg.StorePassword, cfg.EventDedupWindowSeconds, enc.Encrypt, enc.Decrypt)
			if err != nil {
				slog.Error("failed to connect valkey sentinel store", "error", err)
				os.Exit(1)
			}
		} else {
			addr := cfg.StoreURL
			addr = strings.TrimPrefix(addr, "valkey://")
			addr = strings.TrimPrefix(addr, "redis://")
			if idx := strings.LastIndex(addr, "/"); idx > 0 {
				addr = addr[:idx]
			}
			var err error
			st, err = store.NewValkeyStore(ctx, addr, cfg.StoreDB, cfg.StorePassword, cfg.EventDedupWindowSeconds, enc.Encrypt, enc.Decrypt)
			if err != nil {
				slog.Error("failed to connect valkey store", "error", err)
				os.Exit(1)
			}
		}
	}
	defer st.Close()

	// Initialize DNS providers from stored config.
	dnsProviders := make(map[string]dns.Provider)
	dnsConfigs, _ := st.ListDNSProviderConfigs(ctx)
	for name, rawCfg := range dnsConfigs {
		decrypted := enc.DecryptAllFields(rawCfg)
		providerType := decrypted["type"]
		if !dns.IsProviderAvailable(providerType) {
			slog.Warn("DNS provider not available in this build, skipping", "name", name, "type", providerType)
			continue
		}
		p, err := dns.NewProvider(ctx, providerType, decrypted)
		if err != nil {
			slog.Warn("failed to init DNS provider", "name", name, "error", err)
			continue
		}
		dnsProviders[name] = p
		slog.Info("DNS provider loaded", "name", name, "type", providerType)
	}

	// Migrate legacy Slack webhook to new webhook system.
	if slackURL, _ := st.GetSlackWebhookURL(ctx); slackURL != "" {
		channel, _ := st.GetSlackChannel(ctx)
		wh := &models.WebhookEndpoint{
			ID:      "wh_slack_migrated",
			Name:    "Slack (migrated)",
			Type:    models.WebhookTypeSlack,
			URL:     slackURL,
			Enabled: true,
			Channel: channel,
		}
		st.SaveWebhook(ctx, wh)
		st.DeleteSlackLegacy(ctx)
		slog.Info("migrated legacy Slack webhook to new webhook system")
	}

	// Initialize core services.
	ep := core.NewEventProcessor(st)
	fm := core.NewFailoverManager(st, ep, dnsProviders)

	// Parse templates.
	lang := "en"
	if settings, err := st.GetRuntimeSettings(ctx); err == nil {
		if l, ok := settings["language"]; ok && l != "" {
			lang = l
		}
	}
	translate := web.NewTranslator(lang)
	funcMap := server.TemplateFuncMap(translate)
	tmpl, err := server.ParseTemplates(vsm.TemplateFS, funcMap)
	if err != nil {
		slog.Error("failed to parse templates", "error", err)
		os.Exit(1)
	}

	// Build router and register routes.
	mux, err := server.NewRouter(vsm.StaticFS)
	if err != nil {
		slog.Error("failed to create router", "error", err)
		os.Exit(1)
	}
	api.RegisterRoutes(mux, st, fm, dnsProviders)

	sm := web.NewSessionManager(st, cfg.SecureCookie)
	// Start background sentinel health checker.
	healthChecker := core.NewSentinelHealthChecker(st)
	healthChecker.Start()
	defer healthChecker.Stop()

	admin := web.NewAdminHandler(st, sm, tmpl, lang, dnsProviders, enc, healthChecker)
	admin.RegisterRoutes(mux)

	// Apply middleware chain: SecurityHeaders → RequestLogger → mux
	handler := server.SecurityHeadersWithOptions(server.RequestLogger(mux), cfg.SecureCookie)

	if err := server.Run(cfg.Addr(), handler); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
