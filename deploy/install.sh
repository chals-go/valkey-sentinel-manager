#!/usr/bin/env bash
set -euo pipefail

# Valkey Sentinel Manager — Install Script
# Usage: sudo bash deploy/install.sh

BINARY="bin/sentinel-manager"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/sentinel-manager"
LOG_DIR="/var/log/sentinel-manager"
SERVICE_USER="sentinel-manager"

echo "=== Valkey Sentinel Manager Installer ==="

# Check root
if [ "$(id -u)" -ne 0 ]; then
    echo "Error: Run as root (sudo bash deploy/install.sh)"
    exit 1
fi

# Check binary
if [ ! -f "$BINARY" ]; then
    echo "Error: $BINARY not found. Run 'make build' first."
    exit 1
fi

# 1. Create user
if ! id "$SERVICE_USER" &>/dev/null; then
    useradd -r -s /usr/sbin/nologin "$SERVICE_USER"
    echo "  Created user: $SERVICE_USER"
fi

# 2. Install binary
cp "$BINARY" "$INSTALL_DIR/sentinel-manager"
chmod 755 "$INSTALL_DIR/sentinel-manager"
echo "  Installed: $INSTALL_DIR/sentinel-manager"

# 3. Config directory
mkdir -p "$CONFIG_DIR"
if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
    cp config.yaml.example "$CONFIG_DIR/config.yaml"
    echo "  Created: $CONFIG_DIR/config.yaml (edit before starting)"
else
    echo "  Exists: $CONFIG_DIR/config.yaml (not overwritten)"
fi
chown -R "$SERVICE_USER":"$SERVICE_USER" "$CONFIG_DIR"
chmod 600 "$CONFIG_DIR/config.yaml"

# 4. Log directory
mkdir -p "$LOG_DIR"
chown -R "$SERVICE_USER":"$SERVICE_USER" "$LOG_DIR"
echo "  Log dir: $LOG_DIR"

# 5. systemd service
cp deploy/sentinel-manager.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable sentinel-manager
echo "  Service: sentinel-manager.service enabled"

echo ""
echo "=== Done ==="
echo ""
echo "Next steps:"
echo "  1. Edit config:    sudo vi $CONFIG_DIR/config.yaml"
echo "  2. Start service:  sudo systemctl start sentinel-manager"
echo "  3. Check status:   sudo systemctl status sentinel-manager"
echo "  4. View logs:      sudo journalctl -u sentinel-manager -f"
echo "  5. Open browser:   http://<server-ip>:8000/admin/"
echo "     Default login:  admin / admin"
