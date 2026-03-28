#!/bin/sh
set -e

# Required env vars:
#   SENTINEL_MASTERS  - comma-separated "name:host:port" (e.g. "mymaster-v7:valkey7-primary:6379,mymaster-v8:valkey8-primary:6379")
#   SENTINEL_QUORUM   - quorum count (default: 2)
#   SMGR_MONITOR_URL  - sentinel-manager URL
#   SMGR_GROUP_NAME   - sentinel cluster group name
#   SMGR_SENTINEL_NODE_NAME - unique sentinel node name

QUORUM="${SENTINEL_QUORUM:-2}"
CONF="/tmp/sentinel.conf"

cat > "$CONF" <<EOF
port 26379
dir /tmp
SENTINEL resolve-hostnames yes
SENTINEL announce-hostnames yes
EOF

# Read API token from shared volume (written by setup container)
API_KEY=""
if [ -f /shared/api_token ]; then
    API_KEY=$(cat /shared/api_token)
    echo "API key loaded from shared volume"
fi

# Write agent config
mkdir -p /etc/valkey
cat > /etc/valkey/sentinel-agent.yaml <<YAML
monitor_url: "${SMGR_MONITOR_URL:-http://sentinel-manager:8000}"
api_key: "${API_KEY}"
sentinel_node_name: "${SMGR_SENTINEL_NODE_NAME:-sentinel-01}"
group_name: "${SMGR_GROUP_NAME:-cluster-a}"
timeout_seconds: 10
retry_count: 3
YAML

# Parse SENTINEL_MASTERS and add each master
IFS=','
for entry in $SENTINEL_MASTERS; do
    master_name=$(echo "$entry" | cut -d: -f1)
    master_host=$(echo "$entry" | cut -d: -f2)
    master_port=$(echo "$entry" | cut -d: -f3)
    cat >> "$CONF" <<EOF

sentinel monitor ${master_name} ${master_host} ${master_port} ${QUORUM}
sentinel down-after-milliseconds ${master_name} 5000
sentinel failover-timeout ${master_name} 10000
sentinel client-reconfig-script ${master_name} /usr/local/bin/sentinel-agent-reconfig
sentinel notification-script ${master_name} /usr/local/bin/sentinel-agent-notify
EOF
done

echo "=== Sentinel Config ==="
cat "$CONF"
echo "=== Starting Sentinel ==="

exec valkey-sentinel "$CONF"
