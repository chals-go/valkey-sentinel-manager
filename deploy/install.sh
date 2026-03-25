#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# Valkey Sentinel Manager — Unified Install Script
# ============================================================================

INSTALL_DIR="/usr/local/bin"
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

# ---------- Colors ----------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

# ---------- Help / Usage ----------
show_help() {
    echo ""
    echo -e "${BOLD}Valkey Sentinel Manager — Installer${NC}"
    echo ""
    echo -e "${BOLD}USAGE${NC}"
    echo "    sudo bash deploy/install.sh <component> [options]"
    echo ""
    echo -e "${BOLD}COMPONENTS${NC}"
    echo -e "    ${CYAN}sentinel-manager${NC}   Web UI + REST API server for managing Valkey Sentinel DNS failover."
    echo "                       Runs as a systemd service."
    echo ""
    echo -e "    ${CYAN}sentinel-agent${NC}     CLI tool called by Valkey Sentinel via notification-script and"
    echo "                       client-reconfig-script. Installed with Valkey user permissions."
    echo ""
    echo -e "    ${CYAN}all${NC}                Install both components."
    echo ""
    echo -e "${BOLD}OPTIONS${NC}"
    echo "    -h, --help         Show this help message"
    echo ""
    echo -e "${BOLD}EXAMPLES${NC}"
    echo -e "    ${DIM}# Install sentinel-manager only${NC}"
    echo "    sudo bash deploy/install.sh sentinel-manager"
    echo ""
    echo -e "    ${DIM}# Install sentinel-agent only${NC}"
    echo "    sudo bash deploy/install.sh sentinel-agent"
    echo ""
    echo -e "    ${DIM}# Install both${NC}"
    echo "    sudo bash deploy/install.sh all"
    echo ""
    echo -e "${BOLD}WHAT GETS INSTALLED${NC}"
    echo ""
    echo -e "  ${CYAN}sentinel-manager:${NC}"
    echo -e "    Binary      ${DIM}→${NC}  /usr/local/bin/sentinel-manager             ${DIM}(755, sentinel-manager:sentinel-manager)${NC}"
    echo -e "    Config      ${DIM}→${NC}  /etc/sentinel-manager/config.yaml           ${DIM}(600, sentinel-manager:sentinel-manager)${NC}"
    echo -e "    Logs        ${DIM}→${NC}  /var/log/sentinel-manager/                  ${DIM}(sentinel-manager:sentinel-manager)${NC}"
    echo -e "    Service     ${DIM}→${NC}  /etc/systemd/system/sentinel-manager.service"
    echo -e "    User        ${DIM}→${NC}  sentinel-manager (system user, no login)"
    echo ""
    echo -e "  ${CYAN}sentinel-agent:${NC}"
    echo -e "    Binary      ${DIM}→${NC}  /usr/local/bin/sentinel-agent               ${DIM}(755, <valkey-user>:<valkey-group>)${NC}"
    echo -e "    Symlinks    ${DIM}→${NC}  /usr/local/bin/sentinel-agent-reconfig      ${DIM}→ sentinel-agent${NC}"
    echo -e "                   /usr/local/bin/sentinel-agent-notify        ${DIM}→ sentinel-agent${NC}"
    echo -e "    Config      ${DIM}→${NC}  /etc/valkey/sentinel-agent.yaml             ${DIM}(640, <valkey-user>:<valkey-group>)${NC}"
    echo ""
    echo -e "    ${DIM}* <valkey-user> is auto-detected from running valkey-sentinel/valkey-server process,${NC}"
    echo -e "    ${DIM}  binary ownership, or system user (valkey → redis → nobody fallback).${NC}"
    echo ""
    echo -e "${BOLD}PREREQUISITES${NC}"
    echo "    • Root privileges (sudo)"
    echo "    • Go 1.24+ (for building) or pre-built binaries in bin/"
    echo "    • make (for build targets)"
    echo ""
    exit 0
}

# ---------- Pre-checks ----------
# Handle help flag before root check
for arg in "$@"; do
    case "$arg" in
        -h|--help) show_help ;;
    esac
done

[ $# -lt 1 ] && show_help
[ "$(id -u)" -ne 0 ] && error "Run as root: sudo bash deploy/install.sh <component>"

COMPONENT="$1"
case "$COMPONENT" in
    sentinel-manager|sentinel-agent|all) ;;
    *) error "Unknown component: $COMPONENT (use sentinel-manager, sentinel-agent, or all)" ;;
esac

# ---------- OS Detection ----------
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS_ID="${ID:-unknown}"
        OS_VERSION="${VERSION_ID:-unknown}"
        OS_NAME="${PRETTY_NAME:-$OS_ID}"
    elif [ -f /etc/redhat-release ]; then
        OS_ID="rhel"
        OS_NAME="$(cat /etc/redhat-release)"
        OS_VERSION="unknown"
    else
        OS_ID="unknown"
        OS_NAME="Unknown"
        OS_VERSION="unknown"
    fi
    info "OS detected: $OS_NAME"
}

# ---------- Valkey User/Group Detection ----------
detect_valkey_user() {
    VALKEY_USER=""
    VALKEY_GROUP=""

    # 1. Check running valkey-sentinel or valkey-server process
    for proc in valkey-sentinel valkey-server redis-sentinel redis-server; do
        local u
        u=$(ps -eo user,comm 2>/dev/null | awk -v p="$proc" '$2 == p {print $1; exit}')
        if [ -n "$u" ]; then
            VALKEY_USER="$u"
            VALKEY_GROUP=$(id -gn "$u" 2>/dev/null || echo "$u")
            info "Detected Valkey user from process ($proc): $VALKEY_USER:$VALKEY_GROUP"
            return
        fi
    done

    # 2. Check binary ownership
    for bin in /usr/bin/valkey-sentinel /usr/local/bin/valkey-sentinel /usr/bin/redis-sentinel /usr/local/bin/redis-sentinel; do
        if [ -f "$bin" ]; then
            VALKEY_USER=$(stat -c '%U' "$bin" 2>/dev/null || stat -f '%Su' "$bin" 2>/dev/null || echo "")
            if [ -n "$VALKEY_USER" ] && [ "$VALKEY_USER" != "root" ]; then
                VALKEY_GROUP=$(id -gn "$VALKEY_USER" 2>/dev/null || echo "$VALKEY_USER")
                info "Detected Valkey user from binary ($bin): $VALKEY_USER:$VALKEY_GROUP"
                return
            fi
        fi
    done

    # 3. Check if valkey or redis user exists
    for u in valkey redis; do
        if id "$u" &>/dev/null; then
            VALKEY_USER="$u"
            VALKEY_GROUP=$(id -gn "$u" 2>/dev/null || echo "$u")
            info "Detected Valkey user from system user: $VALKEY_USER:$VALKEY_GROUP"
            return
        fi
    done

    # 4. Fallback
    VALKEY_USER="nobody"
    VALKEY_GROUP="nogroup"
    if [ "$OS_ID" = "centos" ] || [ "$OS_ID" = "rhel" ] || [ "$OS_ID" = "amzn" ] || [ "$OS_ID" = "fedora" ]; then
        VALKEY_GROUP="nobody"
    fi
    warn "Could not detect Valkey user, using fallback: $VALKEY_USER:$VALKEY_GROUP"
}

# ---------- Go Build ----------
build_component() {
    local target="$1"
    local binary="bin/$target"

    if [ -f "$SCRIPT_DIR/$binary" ]; then
        info "Binary exists: $binary (skipping build)"
        return
    fi

    info "Building $target..."
    if ! command -v go &>/dev/null; then
        error "Go is not installed. Install Go 1.24+ or pre-build with 'make build-${target#sentinel-}'"
    fi

    cd "$SCRIPT_DIR"
    case "$target" in
        sentinel-manager) make build-manager ;;
        sentinel-agent)   make build-agent ;;
    esac

    [ -f "$binary" ] || error "Build failed: $binary not found"
    info "Build complete: $binary"
}

# ---------- Summary Table ----------
print_summary() {
    local component="$1"
    echo ""
    echo -e "${BOLD}  Installed Files:${NC}"
    echo -e "  ${DIM}────────────────────────────────────────────────────────────────${NC}"

    if [ "$component" = "manager" ] || [ "$component" = "all" ]; then
        local MGR_USER="sentinel-manager"
        printf "  %-12s %-46s %s\n" "Binary"   "$INSTALL_DIR/sentinel-manager" "(755, $MGR_USER:$MGR_USER)"
        printf "  %-12s %-46s %s\n" "Config"   "/etc/sentinel-manager/config.yaml" "(600, $MGR_USER:$MGR_USER)"
        printf "  %-12s %-46s %s\n" "Logs"     "/var/log/sentinel-manager/" "($MGR_USER:$MGR_USER)"
        printf "  %-12s %-46s %s\n" "Service"  "/etc/systemd/system/sentinel-manager.service" "(enabled)"
        printf "  %-12s %-46s %s\n" "User"     "$MGR_USER" "(system, nologin)"
    fi

    if [ "$component" = "agent" ] || [ "$component" = "all" ]; then
        [ "$component" = "all" ] && echo -e "  ${DIM}────────────────────────────────────────────────────────────────${NC}"
        printf "  %-12s %-46s %s\n" "Binary"   "$INSTALL_DIR/sentinel-agent" "(755, $VALKEY_USER:$VALKEY_GROUP)"
        printf "  %-12s %-46s %s\n" "Symlink"  "$INSTALL_DIR/sentinel-agent-reconfig" "→ sentinel-agent"
        printf "  %-12s %-46s %s\n" "Symlink"  "$INSTALL_DIR/sentinel-agent-notify" "→ sentinel-agent"
        printf "  %-12s %-46s %s\n" "Config"   "/etc/valkey/sentinel-agent.yaml" "(640, $VALKEY_USER:$VALKEY_GROUP)"
    fi

    echo -e "  ${DIM}────────────────────────────────────────────────────────────────${NC}"
}

# ---------- Install sentinel-manager ----------
install_manager() {
    echo ""
    echo -e "${CYAN}=== Installing sentinel-manager ===${NC}"

    build_component "sentinel-manager"

    local SERVICE_USER="sentinel-manager"
    local CONFIG_DIR="/etc/sentinel-manager"
    local LOG_DIR="/var/log/sentinel-manager"

    # 1. Create system user
    if ! id "$SERVICE_USER" &>/dev/null; then
        useradd -r -s /usr/sbin/nologin -d /nonexistent "$SERVICE_USER" 2>/dev/null || \
        useradd -r -s /sbin/nologin "$SERVICE_USER"
        info "Created user: $SERVICE_USER"
    else
        info "User exists: $SERVICE_USER"
    fi

    # 2. Install binary
    install -m 755 "$SCRIPT_DIR/bin/sentinel-manager" "$INSTALL_DIR/sentinel-manager"
    info "Installed: $INSTALL_DIR/sentinel-manager"

    # 3. Config directory
    mkdir -p "$CONFIG_DIR"
    if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
        if [ -f "$SCRIPT_DIR/config.yaml.example" ]; then
            cp "$SCRIPT_DIR/config.yaml.example" "$CONFIG_DIR/config.yaml"
            info "Created: $CONFIG_DIR/config.yaml"
        else
            warn "config.yaml.example not found, skipping config copy"
        fi
    else
        info "Exists: $CONFIG_DIR/config.yaml (not overwritten)"
    fi
    chown -R "$SERVICE_USER":"$SERVICE_USER" "$CONFIG_DIR"
    chmod 600 "$CONFIG_DIR/config.yaml" 2>/dev/null || true

    # 4. Log directory
    mkdir -p "$LOG_DIR"
    chown -R "$SERVICE_USER":"$SERVICE_USER" "$LOG_DIR"

    # 5. Create systemd service
    cat > /etc/systemd/system/sentinel-manager.service <<UNIT
[Unit]
Description=Valkey Sentinel Manager
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
ExecStart=$INSTALL_DIR/sentinel-manager --config $CONFIG_DIR/config.yaml
Restart=always
RestartSec=5

NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$LOG_DIR $CONFIG_DIR
PrivateTmp=true

LimitNOFILE=65536
LimitNPROC=4096

StandardOutput=journal
StandardError=journal
SyslogIdentifier=sentinel-manager

[Install]
WantedBy=multi-user.target
UNIT

    systemctl daemon-reload
    systemctl enable sentinel-manager
    info "Service: sentinel-manager.service (enabled, daemon-reloaded)"
}

# ---------- Install sentinel-agent ----------
install_agent() {
    echo ""
    echo -e "${CYAN}=== Installing sentinel-agent ===${NC}"

    build_component "sentinel-agent"
    detect_valkey_user

    local AGENT_CONFIG_DIR="/etc/valkey"

    # 1. Install binary
    install -m 755 "$SCRIPT_DIR/bin/sentinel-agent" "$INSTALL_DIR/sentinel-agent"
    chown "$VALKEY_USER":"$VALKEY_GROUP" "$INSTALL_DIR/sentinel-agent"
    info "Installed: $INSTALL_DIR/sentinel-agent"

    # 2. Create symlinks
    ln -sf "$INSTALL_DIR/sentinel-agent" "$INSTALL_DIR/sentinel-agent-reconfig"
    ln -sf "$INSTALL_DIR/sentinel-agent" "$INSTALL_DIR/sentinel-agent-notify"
    info "Symlinks: sentinel-agent-reconfig, sentinel-agent-notify"

    # 3. Config file
    mkdir -p "$AGENT_CONFIG_DIR"
    if [ ! -f "$AGENT_CONFIG_DIR/sentinel-agent.yaml" ]; then
        cat > "$AGENT_CONFIG_DIR/sentinel-agent.yaml" <<AGENTCFG
# Sentinel Agent Configuration
# Called by Valkey Sentinel via notification-script and client-reconfig-script.

# Sentinel Manager server URL
monitor_url: "http://localhost:8000"

# API authentication token (generate from Sentinel Manager UI → Settings → API Token)
api_key: ""

# Unique name for this sentinel node (must match the node name registered in Sentinel Manager)
sentinel_node_name: "sentinel-01"

# Sentinel cluster group name
group_name: "my-cluster"

# HTTP timeout in seconds
timeout_seconds: 10

# Retry count on failure
retry_count: 2
AGENTCFG
        chown "$VALKEY_USER":"$VALKEY_GROUP" "$AGENT_CONFIG_DIR/sentinel-agent.yaml"
        chmod 640 "$AGENT_CONFIG_DIR/sentinel-agent.yaml"
        info "Created: $AGENT_CONFIG_DIR/sentinel-agent.yaml"
    else
        info "Exists: $AGENT_CONFIG_DIR/sentinel-agent.yaml (not overwritten)"
    fi
}

# ---------- Next Steps ----------
print_next_steps() {
    local component="$1"
    echo ""
    echo -e "${BOLD}  Next Steps:${NC}"

    if [ "$component" = "manager" ] || [ "$component" = "all" ]; then
        echo ""
        echo -e "  ${CYAN}sentinel-manager:${NC}"
        echo "    1. Edit config:    sudo vi /etc/sentinel-manager/config.yaml"
        echo "    2. Start service:  sudo systemctl start sentinel-manager"
        echo "    3. Check status:   sudo systemctl status sentinel-manager"
        echo "    4. View logs:      sudo journalctl -u sentinel-manager -f"
        echo "    5. Open browser:   http://<server-ip>:8000/admin/"
        echo "       Default login:  admin / admin"
    fi

    if [ "$component" = "agent" ] || [ "$component" = "all" ]; then
        echo ""
        echo -e "  ${CYAN}sentinel-agent:${NC}"
        echo "    1. Edit config:  sudo vi /etc/valkey/sentinel-agent.yaml"
        echo "    2. Set api_key, sentinel_node_name, group_name, monitor_url"
        echo "    3. Add to sentinel.conf:"
        echo "       sentinel notification-script <master> $INSTALL_DIR/sentinel-agent-notify"
        echo "       sentinel client-reconfig-script <master> $INSTALL_DIR/sentinel-agent-reconfig"
    fi
}

# ---------- Main ----------
detect_os

SUMMARY_TYPE=""

case "$COMPONENT" in
    sentinel-manager)
        install_manager
        SUMMARY_TYPE="manager"
        ;;
    sentinel-agent)
        install_agent
        SUMMARY_TYPE="agent"
        ;;
    all)
        install_manager
        install_agent
        SUMMARY_TYPE="all"
        ;;
esac

echo ""
echo -e "${GREEN}=== Installation complete ===${NC}"
print_summary "$SUMMARY_TYPE"
print_next_steps "$SUMMARY_TYPE"
echo ""
