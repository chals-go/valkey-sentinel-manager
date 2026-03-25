#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# Valkey Sentinel Manager — Unified Install Script
#
# Usage:
#   sudo bash deploy/install.sh sentinel-manager   # Install sentinel-manager
#   sudo bash deploy/install.sh sentinel-agent      # Install sentinel-agent
#   sudo bash deploy/install.sh all                 # Install both
# ============================================================================

INSTALL_DIR="/usr/local/bin"
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

# ---------- Colors ----------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

# ---------- Usage ----------
usage() {
    echo ""
    echo -e "${CYAN}Valkey Sentinel Manager — Installer${NC}"
    echo ""
    echo "Usage:"
    echo "  sudo bash deploy/install.sh <component>"
    echo ""
    echo "Components:"
    echo "  sentinel-manager   Install Sentinel Manager (web UI + API server)"
    echo "  sentinel-agent     Install Sentinel Agent (reconfig/notify scripts)"
    echo "  all                Install both components"
    echo ""
    exit 1
}

# ---------- Pre-checks ----------
[ "$(id -u)" -ne 0 ] && error "Run as root: sudo bash deploy/install.sh <component>"
[ $# -lt 1 ] && usage

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
            info "Created: $CONFIG_DIR/config.yaml (edit before starting)"
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
    info "Log dir: $LOG_DIR"

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
    info "Service: sentinel-manager.service enabled"

    echo ""
    info "sentinel-manager installed successfully!"
    echo ""
    echo "  Next steps:"
    echo "    1. Edit config:    sudo vi $CONFIG_DIR/config.yaml"
    echo "    2. Start service:  sudo systemctl start sentinel-manager"
    echo "    3. Check status:   sudo systemctl status sentinel-manager"
    echo "    4. View logs:      sudo journalctl -u sentinel-manager -f"
    echo "    5. Open browser:   http://<server-ip>:8000/admin/"
    echo "       Default login:  admin / admin"
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
    info "Installed: $INSTALL_DIR/sentinel-agent ($VALKEY_USER:$VALKEY_GROUP)"

    # 2. Create symlinks
    ln -sf "$INSTALL_DIR/sentinel-agent" "$INSTALL_DIR/sentinel-agent-reconfig"
    ln -sf "$INSTALL_DIR/sentinel-agent" "$INSTALL_DIR/sentinel-agent-notify"
    info "Symlinks: sentinel-agent-reconfig, sentinel-agent-notify"

    # 3. Config file
    mkdir -p "$AGENT_CONFIG_DIR"
    if [ ! -f "$AGENT_CONFIG_DIR/sentinel-agent.yaml" ]; then
        cat > "$AGENT_CONFIG_DIR/sentinel-agent.yaml" <<AGENTCFG
# Sentinel Agent Configuration
# This agent is called by Valkey Sentinel via notification-script and client-reconfig-script.

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
        info "Created: $AGENT_CONFIG_DIR/sentinel-agent.yaml (edit before use)"
    else
        info "Exists: $AGENT_CONFIG_DIR/sentinel-agent.yaml (not overwritten)"
    fi

    echo ""
    info "sentinel-agent installed successfully!"
    echo ""
    echo "  Installed as: $VALKEY_USER:$VALKEY_GROUP"
    echo ""
    echo "  Next steps:"
    echo "    1. Edit config:  sudo vi $AGENT_CONFIG_DIR/sentinel-agent.yaml"
    echo "    2. Set api_key, sentinel_node_name, group_name, monitor_url"
    echo "    3. Configure Sentinel scripts in sentinel.conf:"
    echo "       sentinel notification-script <master> $INSTALL_DIR/sentinel-agent-notify"
    echo "       sentinel client-reconfig-script <master> $INSTALL_DIR/sentinel-agent-reconfig"
}

# ---------- Main ----------
detect_os

case "$COMPONENT" in
    sentinel-manager)
        install_manager
        ;;
    sentinel-agent)
        install_agent
        ;;
    all)
        install_manager
        echo ""
        install_agent
        ;;
esac

echo ""
echo -e "${GREEN}=== Installation complete ===${NC}"
