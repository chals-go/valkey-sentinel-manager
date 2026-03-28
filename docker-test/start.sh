#!/bin/bash
set -e
cd "$(dirname "$0")"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

header() { echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"; echo -e "${BOLD}  $1${NC}"; echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"; }
step()   { echo -e "\n${YELLOW}[$1/$TOTAL]${NC} ${BOLD}$2${NC}"; }
ok()     { echo -e "  ${GREEN}OK${NC} $1"; }
wait_healthy() {
    local name=$1 max=${2:-60} i=0
    while [ $i -lt $max ]; do
        status=$(docker inspect --format='{{.State.Health.Status}}' "$name" 2>/dev/null || echo "missing")
        [ "$status" = "healthy" ] && return 0
        sleep 1; i=$((i+1))
    done
    echo -e "  ${RED}TIMEOUT${NC} $name did not become healthy in ${max}s"
    return 1
}

TOTAL=5

# ============================================================
header "Valkey Sentinel Manager — Test Environment"
# ============================================================

echo -e "${DIM}Building and starting 16 containers...${NC}"

# Step 1: Build
step 1 "Building Docker images..."
docker compose build --quiet 2>&1
ok "All images built"

# Step 2: Start infra (store, valkey nodes, mock-dns)
step 2 "Starting infrastructure..."
docker compose up -d store-valkey valkey7-primary valkey8-primary valkey8x-primary mock-dns 2>&1 | grep -v "^$"
wait_healthy store-valkey
wait_healthy valkey7-primary
wait_healthy valkey8-primary
wait_healthy valkey8x-primary
wait_healthy mock-dns
ok "Storage, Valkey primaries, Mock DNS ready"

# Start replicas
docker compose up -d valkey7-replica1 valkey7-replica2 valkey8-replica1 valkey8x-replica1 2>&1 | grep -v "^$"
sleep 2
ok "Valkey replicas started"

# Step 3: Setup (API token + DNS provider) + Sentinel Manager
step 3 "Running setup & starting Sentinel Manager..."
docker compose up -d setup 2>&1 | grep -v "^$"
# Wait for setup to complete
timeout 30 sh -c 'while [ "$(docker inspect --format="{{.State.Status}}" setup 2>/dev/null)" != "exited" ]; do sleep 1; done' 2>/dev/null || true
ok "API token & DNS provider pre-configured"

docker compose up -d sentinel-manager 2>&1 | grep -v "^$"
wait_healthy sentinel-manager
ok "Sentinel Manager running"

# Step 4: Start sentinels
step 4 "Starting Sentinel clusters..."
docker compose up -d sentinel-a1 sentinel-a2 sentinel-a3 sentinel-b1 sentinel-b2 sentinel-b3 2>&1 | grep -v "^$"
sleep 3
# Verify sentinels are running
RUNNING=0
for s in sentinel-a1 sentinel-a2 sentinel-a3 sentinel-b1 sentinel-b2 sentinel-b3; do
    st=$(docker inspect --format='{{.State.Status}}' "$s" 2>/dev/null || echo "missing")
    [ "$st" = "running" ] && RUNNING=$((RUNNING+1))
done
if [ $RUNNING -eq 6 ]; then
    ok "All 6 sentinels running"
else
    echo -e "  ${RED}WARNING${NC} Only $RUNNING/6 sentinels running"
fi

# Step 5: Summary
step 5 "Verifying environment..."
HEALTH=$(curl -sf http://localhost:8000/api/v1/health 2>/dev/null || echo "{}")
DNS_COUNT=$(echo "$HEALTH" | grep -o '"dns_providers_count":[0-9]*' | cut -d: -f2)
DNS_OK=$(echo "$HEALTH" | grep -o '"dns_providers_healthy":[0-9]*' | cut -d: -f2)

CONTAINERS=$(docker compose ps --format '{{.Name}}' 2>/dev/null | grep -v setup | wc -l)

header "Test Environment Ready!"

echo -e "
${BOLD}${GREEN}Sentinel Manager${NC}
  URL:       ${CYAN}http://localhost:8000/admin/${NC}
  Login:     ${BOLD}admin${NC} / ${BOLD}admin${NC}
  API Token: ${BOLD}smgr_docker_test_token_2026${NC}
  DNS:       ${DNS_COUNT:-0} provider(s), ${DNS_OK:-0} healthy

${BOLD}${GREEN}Containers${NC}: ${CONTAINERS} running

${BOLD}${GREEN}Pre-configured${NC}
  DNS Provider:  ${CYAN}mock-dns${NC} (REST API → http://mock-dns:8080)

${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}
${BOLD}  Register these in the Web UI:${NC}
${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}

${BOLD}${YELLOW}Sentinel Clusters${NC}   (Settings → Sentinel Cluster → Register)
┌─────────────┬──────────────────────────────────────────────────┬──────────────────────────┐
│ Group Name  │ Sentinel Nodes (Host:Port)                      │ Monitors                 │
├─────────────┼──────────────────────────────────────────────────┼──────────────────────────┤
│ cluster-a   │ sentinel-a1:26379, sentinel-a2:26379,           │ mymaster-v7              │
│             │ sentinel-a3:26379                                │                          │
├─────────────┼──────────────────────────────────────────────────┼──────────────────────────┤
│ cluster-b   │ sentinel-b1:26379, sentinel-b2:26379,           │ mymaster-v8, mymaster-v8x│
│             │ sentinel-b3:26379                                │                          │
└─────────────┴──────────────────────────────────────────────────┴──────────────────────────┘

${BOLD}${YELLOW}Replication Groups${NC}  (Replication Group → Register / Load Sentinels)
┌───────────────┬──────────┬──────────────────┬─────────┬─────────────────────────────────┐
│ Monitoring    │ Sentinel │ Primary          │ Valkey  │ Replicas                        │
│ Name          │ Cluster  │ (IP:Port)        │ Version │                                 │
├───────────────┼──────────┼──────────────────┼─────────┼─────────────────────────────────┤
│ mymaster-v7   │cluster-a │valkey7-primary   │  7.2    │ valkey7-replica1, replica2       │
│               │          │  :6379           │         │                                 │
├───────────────┼──────────┼──────────────────┼─────────┼─────────────────────────────────┤
│ mymaster-v8   │cluster-b │valkey8-primary   │  8.0    │ valkey8-replica1                │
│               │          │  :6379           │         │                                 │
├───────────────┼──────────┼──────────────────┼─────────┼─────────────────────────────────┤
│ mymaster-v8x  │cluster-b │valkey8x-primary  │  8.1    │ valkey8x-replica1               │
│               │          │  :6379           │         │                                 │
└───────────────┴──────────┴──────────────────┴─────────┴─────────────────────────────────┘

${DIM}Tip: Use 'Load Sentinels' button to auto-import all masters from a sentinel cluster.${NC}
${DIM}Tip: Select 'mock-dns' as DNS provider when registering replication groups.${NC}

${BOLD}Commands${NC}
  docker compose ps          # Container status
  docker compose logs -f     # Follow all logs
  docker compose down -v     # Tear down everything
"
