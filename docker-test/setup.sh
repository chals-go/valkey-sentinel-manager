#!/bin/sh
# Setup script: pre-configures store-valkey with API token and DNS provider.
set -e

API_TOKEN="smgr_docker_test_token_2026"
SHARED_DIR="/shared"

echo "Waiting for store-valkey..."
until valkey-cli -h store-valkey ping 2>/dev/null | grep -q PONG; do
    sleep 1
done

# 1. API Token
echo "Setting API token..."
valkey-cli -h store-valkey SET "smgr:api:token" "$API_TOKEN"

mkdir -p "$SHARED_DIR"
echo -n "$API_TOKEN" > "$SHARED_DIR/api_token"

# 2. DNS Provider — REST API (mock-dns)
echo "Registering DNS provider (mock-dns)..."
DNS_CFG=$(cat <<'ENDJSON'
{"type":"restapi","base_url":"http://mock-dns:8080","headers":"{\"Content-Type\":\"application/json\"}","update_method":"PUT","update_url":"/api/dns/update","update_body":"{\"hostname\":\"$domain\",\"ip\":\"$ip\",\"ttl\":$ttl,\"type\":\"$record_type\"}","update_multi_method":"PUT","update_multi_url":"/api/dns/update-multi","update_multi_body":"{\"hostname\":\"$domain\",\"ips\":$ips,\"ttl\":$ttl,\"type\":\"$record_type\"}","delete_method":"DELETE","delete_url":"/api/dns/delete","delete_body":"{\"hostname\":\"$domain\",\"ip\":\"$ip\"}","health_method":"GET","health_url":"/health"}
ENDJSON
)
valkey-cli -h store-valkey HSET "smgr:dns:providers" "mock-dns" "$DNS_CFG"

echo "=== Setup complete ==="
echo "  API Token: $API_TOKEN"
echo "  DNS Provider: mock-dns (REST API → http://mock-dns:8080)"
