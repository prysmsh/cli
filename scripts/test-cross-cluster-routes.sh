#!/usr/bin/env bash
# Integration test: cross-cluster routes CRUD + mesh route SOCKS5 proxy.
#
# Tests the full flow:
#   0. Verify auth + clusters
#   1. Deploy a test nginx service in the target cluster
#   --- Cross-cluster route CRUD ---
#   2. Create a cross-cluster route pointing to the test service
#   3. List / toggle / verify CRUD
#   4. Delete cross-cluster route + verify
#   --- Mesh route via .mesh URL ---
#   5. Create a mesh route on the target cluster
#   6. Start mesh with SOCKS5 proxy
#   7. Curl the service via <route>.<cluster>.mesh through SOCKS5
#   8. Clean up (delete mesh route, remove test service, disconnect mesh)
#
# Note: Cross-cluster routes and mesh routes are SEPARATE systems.
#   - CCRs: cluster-to-cluster TCP tunnels via DERP (agent local port listener)
#   - Mesh routes: exit peer resolution via .mesh URLs (SOCKS5 proxy → DERP → agent → K8s service)
#
# Prerequisites:
#   - prysm login (authenticated session)
#   - At least two clusters connected, target with exit enabled
#   - kubectl contexts matching k3d-<cluster-name>
#
# Usage:
#   ./scripts/test-cross-cluster-routes.sh <source-cluster> <target-cluster>
#   PRYSM_BIN=./prysm-test ./scripts/test-cross-cluster-routes.sh frank hp
#   BOOTSTRAP_K3D=1 PRYSM_BIN=./prysm-test ./scripts/test-cross-cluster-routes.sh e2e-src e2e-tgt
#
# Environment:
#   PRYSM_BIN      - path to prysm binary (default: prysm)
#   SOCKS_PORT     - local SOCKS5 port (default: 1080)
#   KUBE_CTX_FMT   - kubectl context format (default: k3d-%s)
#   SKIP_MESH      - set to 1 to skip mesh connect/curl steps
#   SKIP_DEPLOY    - set to 1 to skip test service deployment (assumes it exists)
#   SKIP_CCR       - set to 1 to skip cross-cluster route CRUD tests
#   BOOTSTRAP_K3D  - set to 1 to create/onboard k3d clusters before tests
#   DELETE_K3D_ON_EXIT - set to 1 to delete k3d clusters created by BOOTSTRAP_K3D (default: same as BOOTSTRAP_K3D)
#   K3D_IMAGE      - optional image override passed to k3d cluster create
#   K3D_SERVERS    - number of k3d servers when creating clusters (default: 1)
#   K3D_AGENTS     - number of k3d agents when creating clusters (default: 1)
#   K3D_NODE_TIMEOUT - kubectl wait timeout for node readiness (default: 120s)
#   ONBOARD_TIMEOUT - helm timeout for `prysm clusters onboard kube` (default: 180s)
#   ONBOARD_SKIP_POLL - set to 1 to pass --skip-poll to onboarding
#   CLUSTER_CONNECT_TIMEOUT - seconds to wait for clusters to become connected (default: 240)

set -euo pipefail

PRYSM="${PRYSM_BIN:-prysm}"
SOURCE_CLUSTER="${1:-}"
TARGET_CLUSTER="${2:-}"
SOCKS_PORT="${SOCKS_PORT:-1080}"
KUBE_CTX_FMT="${KUBE_CTX_FMT:-k3d-%s}"
SKIP_MESH="${SKIP_MESH:-0}"
SKIP_DEPLOY="${SKIP_DEPLOY:-0}"
SKIP_CCR="${SKIP_CCR:-0}"
BOOTSTRAP_K3D="${BOOTSTRAP_K3D:-0}"
DELETE_K3D_ON_EXIT="${DELETE_K3D_ON_EXIT:-$BOOTSTRAP_K3D}"
K3D_IMAGE="${K3D_IMAGE:-}"
K3D_SERVERS="${K3D_SERVERS:-1}"
K3D_AGENTS="${K3D_AGENTS:-1}"
K3D_NODE_TIMEOUT="${K3D_NODE_TIMEOUT:-120s}"
ONBOARD_TIMEOUT="${ONBOARD_TIMEOUT:-180s}"
ONBOARD_SKIP_POLL="${ONBOARD_SKIP_POLL:-0}"
CLUSTER_CONNECT_TIMEOUT="${CLUSTER_CONNECT_TIMEOUT:-240}"

if [ "$BOOTSTRAP_K3D" = "1" ]; then
  # Allow source/target to be omitted in bootstrap mode by generating unique names.
  if [ -z "$SOURCE_CLUSTER" ] || [ -z "$TARGET_CLUSTER" ]; then
    RUN_ID=$(date +%s)
    SOURCE_CLUSTER="${SOURCE_CLUSTER:-e2e-src-$RUN_ID}"
    TARGET_CLUSTER="${TARGET_CLUSTER:-e2e-tgt-$RUN_ID}"
  fi
fi

# Test service config
TEST_NS="default"
TEST_SVC="ccr-echo-test"
TEST_PORT=8080
LOCAL_PORT=9876
CCR_ROUTE_NAME="ccr-e2e-$(date +%s)"
MESH_ROUTE_NAME="mesh-e2e-$(date +%s)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'
PASS=0
FAIL=0

step()  { echo -e "\n${YELLOW}=== $1 ===${NC}"; }
info()  { echo -e "${CYAN}$1${NC}"; }
pass()  { echo -e "${GREEN}PASS${NC}: $1"; PASS=$((PASS + 1)); }
fail()  { echo -e "${RED}FAIL${NC}: $1"; FAIL=$((FAIL + 1)); }

TARGET_CTX=$(printf "$KUBE_CTX_FMT" "$TARGET_CLUSTER")
CREATED_CLUSTERS=()

# routeHostSlug: must match Go routeHostSlug() in internal/cmd/mesh.go
# - lowercase
# - space/underscore/slash/dot → hyphen (no consecutive hyphens)
# - drop everything else (including original hyphens!)
# - trim leading/trailing hyphens
slug() {
  local input="$1"
  local result=""
  local prev=""
  input=$(echo "$input" | tr '[:upper:]' '[:lower:]')
  for (( i=0; i<${#input}; i++ )); do
    c="${input:$i:1}"
    if [[ "$c" =~ [a-z0-9] ]]; then
      result+="$c"
      prev="$c"
    elif [[ "$c" == " " || "$c" == "_" || "$c" == "/" || "$c" == "." ]]; then
      if [ -n "$result" ] && [ "$prev" != "-" ]; then
        result+="-"
        prev="-"
      fi
    fi
    # all other chars (including hyphens) are dropped
  done
  # Trim leading/trailing hyphens
  result="${result#-}"
  result="${result%-}"
  echo "$result"
}

need_cmd() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || { fail "required command not found in PATH: $cmd"; exit 1; }
}

k3d_cluster_exists() {
  local name="$1"
  k3d cluster list 2>/dev/null | awk 'NR > 1 { print $1 }' | grep -Fxq "$name"
}

wait_cluster_connected() {
  local cluster="$1"
  local timeout_s="$2"
  local elapsed=0
  local interval=4
  while [ "$elapsed" -lt "$timeout_s" ]; do
    local out=""
    out=$("$PRYSM" clusters list 2>&1 || true)
    if echo "$out" | grep -qE "$cluster\s+connected"; then
      return 0
    fi
    sleep "$interval"
    elapsed=$((elapsed + interval))
  done
  return 1
}

ensure_k3d_cluster() {
  local name="$1"
  local ctx
  ctx=$(printf "$KUBE_CTX_FMT" "$name")

  if k3d_cluster_exists "$name"; then
    info "k3d cluster '$name' already exists; reusing"
  else
    local create_args=(cluster create "$name" --servers "$K3D_SERVERS" --agents "$K3D_AGENTS")
    if [ -n "$K3D_IMAGE" ]; then
      create_args+=(--image "$K3D_IMAGE")
    fi
    info "Creating k3d cluster '$name'"
    k3d "${create_args[@]}"
    CREATED_CLUSTERS+=("$name")
    pass "k3d cluster '$name' created"
  fi

  info "Waiting for k8s nodes in context '$ctx' to be Ready"
  if kubectl --context "$ctx" wait --for=condition=Ready nodes --all --timeout="$K3D_NODE_TIMEOUT" >/dev/null 2>&1; then
    pass "context '$ctx' nodes are Ready"
  else
    fail "context '$ctx' nodes did not become Ready"
    kubectl --context "$ctx" get nodes -o wide 2>/dev/null || true
    exit 1
  fi
}

onboard_k3d_cluster() {
  local cluster="$1"
  local ctx
  local onboard_args
  ctx=$(printf "$KUBE_CTX_FMT" "$cluster")
  onboard_args=(clusters onboard kube --name "$cluster" --kube-context "$ctx" --wait --timeout "$ONBOARD_TIMEOUT")
  if [ "$ONBOARD_SKIP_POLL" = "1" ]; then
    onboard_args+=(--skip-poll)
  fi

  info "Onboarding '$cluster' via context '$ctx'"
  if "$PRYSM" "${onboard_args[@]}"; then
    pass "onboarded '$cluster'"
  else
    fail "failed to onboard '$cluster'"
    exit 1
  fi
}

cleanup() {
  step "Cleanup"
  if [ -n "${MESH_PID:-}" ]; then
    info "Stopping mesh (PID $MESH_PID)"
    kill "$MESH_PID" 2>/dev/null || true
    "$PRYSM" mesh disconnect 2>/dev/null || true
  fi
  if [ -n "${CCR_ROUTE_ID:-}" ]; then
    info "Deleting CCR route $CCR_ROUTE_ID"
    "$PRYSM" mesh cross-cluster-routes delete "$CCR_ROUTE_ID" 2>/dev/null || true
  fi
  if [ -n "${MESH_ROUTE_ID:-}" ]; then
    info "Deleting mesh route $MESH_ROUTE_ID"
    "$PRYSM" mesh routes delete "$MESH_ROUTE_ID" 2>/dev/null || true
  fi
  if [ "$SKIP_DEPLOY" != "1" ] && [ -n "${TARGET_CLUSTER:-}" ]; then
    info "Removing test service from $TARGET_CTX"
    kubectl --context "$TARGET_CTX" delete deployment "$TEST_SVC" -n "$TEST_NS" --ignore-not-found --timeout=30s 2>/dev/null || true
    kubectl --context "$TARGET_CTX" delete service "$TEST_SVC" -n "$TEST_NS" --ignore-not-found --timeout=30s 2>/dev/null || true
  fi
  if [ "$DELETE_K3D_ON_EXIT" = "1" ] && [ "${#CREATED_CLUSTERS[@]}" -gt 0 ]; then
    for CL in "${CREATED_CLUSTERS[@]}"; do
      info "Removing Prysm registration for '$CL' (best effort)"
      "$PRYSM" clusters remove "$CL" 2>/dev/null || true
      if command -v k3d >/dev/null 2>&1; then
        info "Deleting k3d cluster '$CL'"
        k3d cluster delete "$CL" 2>/dev/null || true
      fi
    done
  fi
}
trap cleanup EXIT

if [ -z "$SOURCE_CLUSTER" ] || [ -z "$TARGET_CLUSTER" ]; then
  echo "Usage: $0 <source-cluster> <target-cluster>"
  echo ""
  echo "Example: PRYSM_BIN=./prysm-test $0 frank hp"
  echo "Example (bootstrap): BOOTSTRAP_K3D=1 PRYSM_BIN=./prysm-test $0 e2e-src e2e-tgt"
  exit 1
fi

# ----------------------------------------------------------------
# Step 0: Verify auth, optionally bootstrap k3d clusters, then verify connectivity
# ----------------------------------------------------------------
step "0. Verify authentication"
if ! CLUSTERS_OUT=$("$PRYSM" clusters list 2>&1); then
  fail "clusters list failed — are you logged in? ($CLUSTERS_OUT)"
  exit 1
fi
pass "authenticated"

if [ "$BOOTSTRAP_K3D" = "1" ]; then
  step "0a. Bootstrap k3d clusters + onboard via CLI"
  need_cmd k3d
  need_cmd kubectl
  need_cmd helm
  ensure_k3d_cluster "$SOURCE_CLUSTER"
  ensure_k3d_cluster "$TARGET_CLUSTER"
  onboard_k3d_cluster "$SOURCE_CLUSTER"
  onboard_k3d_cluster "$TARGET_CLUSTER"

  step "0b. Enable target cluster as exit router"
  if EXIT_OUT=$("$PRYSM" clusters exit enable "$TARGET_CLUSTER" 2>&1); then
    echo "$EXIT_OUT"
    pass "enabled exit on '$TARGET_CLUSTER'"
  else
    echo "$EXIT_OUT"
    fail "failed to enable exit on '$TARGET_CLUSTER'"
    exit 1
  fi
fi

step "0c. Verify clusters are connected"
if ! CLUSTERS_OUT=$("$PRYSM" clusters list 2>&1); then
  fail "clusters list failed after bootstrap"
  exit 1
fi
echo "$CLUSTERS_OUT"

# Verify both clusters are connected
for CL in "$SOURCE_CLUSTER" "$TARGET_CLUSTER"; do
  if [ "$BOOTSTRAP_K3D" = "1" ]; then
    if ! wait_cluster_connected "$CL" "$CLUSTER_CONNECT_TIMEOUT"; then
      CLUSTERS_OUT=$("$PRYSM" clusters list 2>&1 || true)
      echo "$CLUSTERS_OUT"
      fail "cluster '$CL' is not connected"
      exit 1
    fi
  else
    if ! echo "$CLUSTERS_OUT" | grep -qE "$CL\s+connected"; then
      fail "cluster '$CL' is not connected"
      exit 1
    fi
  fi
done
pass "both clusters connected"

# ----------------------------------------------------------------
# Step 1: Deploy test service in target cluster
# ----------------------------------------------------------------
if [ "$SKIP_DEPLOY" != "1" ]; then
  step "1. Deploy test service ($TEST_SVC:$TEST_PORT) in $TARGET_CLUSTER"

  # Clean up any previous test resources
  kubectl --context "$TARGET_CTX" delete deployment "$TEST_SVC" -n "$TEST_NS" --ignore-not-found 2>/dev/null || true
  kubectl --context "$TARGET_CTX" delete service "$TEST_SVC" -n "$TEST_NS" --ignore-not-found 2>/dev/null || true

  # Deploy a simple nginx that responds with a known string
  kubectl --context "$TARGET_CTX" apply -n "$TEST_NS" -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $TEST_SVC
  labels:
    app: $TEST_SVC
spec:
  replicas: 1
  selector:
    matchLabels:
      app: $TEST_SVC
  template:
    metadata:
      labels:
        app: $TEST_SVC
    spec:
      terminationGracePeriodSeconds: 5
      containers:
      - name: nginx
        image: nginx:1.25-alpine
        ports:
        - containerPort: 80
        readinessProbe:
          httpGet:
            path: /
            port: 80
          initialDelaySeconds: 1
          periodSeconds: 2
---
apiVersion: v1
kind: Service
metadata:
  name: $TEST_SVC
spec:
  selector:
    app: $TEST_SVC
  ports:
  - port: $TEST_PORT
    targetPort: 80
    protocol: TCP
EOF

  # Wait for the pod to be ready
  info "Waiting for $TEST_SVC pod to be ready..."
  if kubectl --context "$TARGET_CTX" rollout status deployment/$TEST_SVC -n "$TEST_NS" --timeout=60s 2>&1; then
    pass "test service deployed and ready"
  else
    fail "test service did not become ready"
    kubectl --context "$TARGET_CTX" get pods -n "$TEST_NS" 2>&1
    exit 1
  fi

  # Quick sanity: port-forward and curl locally
  info "Sanity check: port-forward to test service"
  kubectl --context "$TARGET_CTX" port-forward -n "$TEST_NS" "svc/$TEST_SVC" 18899:$TEST_PORT &>/dev/null &
  PF_PID=$!
  sleep 2
  if curl -s --connect-timeout 5 http://127.0.0.1:18899/ | grep -qi "nginx\|welcome"; then
    pass "test service reachable via port-forward"
  else
    fail "test service not reachable via port-forward"
  fi
  kill $PF_PID 2>/dev/null || true
  wait $PF_PID 2>/dev/null || true
else
  step "1. (skipped — SKIP_DEPLOY=1)"
fi

# ================================================================
# Part A: Cross-Cluster Route CRUD
# ================================================================
if [ "$SKIP_CCR" != "1" ]; then

# ----------------------------------------------------------------
# Step 2: Create cross-cluster route
# ----------------------------------------------------------------
step "2. Create cross-cluster route: $SOURCE_CLUSTER → $TARGET_CLUSTER ($TEST_SVC:$TEST_PORT)"
CREATE_OUT=$("$PRYSM" mesh cross-cluster-routes create \
  --name "$CCR_ROUTE_NAME" \
  --source "$SOURCE_CLUSTER" \
  --target "$TARGET_CLUSTER" \
  --service "$TEST_SVC" \
  --namespace "$TEST_NS" \
  --target-port "$TEST_PORT" \
  --local-port "$LOCAL_PORT" \
  --protocol "tcp" 2>&1)
echo "$CREATE_OUT"
if echo "$CREATE_OUT" | grep -qi "created\|route.*[0-9]"; then
  pass "CCR created"
  CCR_ROUTE_ID=$(echo "$CREATE_OUT" | grep -oE 'route [0-9]+' | head -1 | grep -oE '[0-9]+')
  echo "CCR Route ID: ${CCR_ROUTE_ID:-unknown}"
else
  fail "CCR creation failed"
fi

# ----------------------------------------------------------------
# Step 3: List — should include new route
# ----------------------------------------------------------------
step "3. List cross-cluster routes"
LIST_OUT=$("$PRYSM" mesh cross-cluster-routes list 2>&1)
echo "$LIST_OUT"
if echo "$LIST_OUT" | grep -q "$CCR_ROUTE_NAME"; then
  pass "CCR appears in list"
else
  fail "CCR not found in list"
fi

# ----------------------------------------------------------------
# Step 4: Toggle disable / re-enable
# ----------------------------------------------------------------
if [ -n "${CCR_ROUTE_ID:-}" ]; then
  step "4a. Toggle CCR $CCR_ROUTE_ID (disable)"
  TOGGLE_OUT=$("$PRYSM" mesh cross-cluster-routes toggle "$CCR_ROUTE_ID" 2>&1)
  echo "$TOGGLE_OUT"
  if echo "$TOGGLE_OUT" | grep -qi "disabled"; then
    pass "CCR disabled"
  else
    fail "toggle disable unexpected output"
  fi

  step "4b. Toggle CCR $CCR_ROUTE_ID (re-enable)"
  TOGGLE_OUT=$("$PRYSM" mesh cross-cluster-routes toggle "$CCR_ROUTE_ID" 2>&1)
  echo "$TOGGLE_OUT"
  if echo "$TOGGLE_OUT" | grep -qi "enabled"; then
    pass "CCR re-enabled"
  else
    fail "toggle enable unexpected output"
  fi
fi

# ----------------------------------------------------------------
# Step 5: Delete the CCR
# ----------------------------------------------------------------
if [ -n "${CCR_ROUTE_ID:-}" ]; then
  step "5. Delete CCR $CCR_ROUTE_ID"
  DEL_OUT=$("$PRYSM" mesh cross-cluster-routes delete "$CCR_ROUTE_ID" 2>&1)
  echo "$DEL_OUT"
  if echo "$DEL_OUT" | grep -qi "deleted"; then
    pass "CCR deleted"
    CCR_ROUTE_ID=""  # prevent cleanup from trying again
  else
    fail "CCR delete failed"
  fi
fi

# ----------------------------------------------------------------
# Step 6: Verify CCR deletion
# ----------------------------------------------------------------
step "6. Verify CCR deleted"
AFTER_OUT=$("$PRYSM" mesh cross-cluster-routes list 2>&1)
echo "$AFTER_OUT"
if ! echo "$AFTER_OUT" | grep -q "$CCR_ROUTE_NAME"; then
  pass "CCR no longer in list"
else
  fail "CCR still appears after deletion"
fi

else
  step "2-6. (skipped — SKIP_CCR=1)"
fi

# ================================================================
# Part B: Mesh Route via .mesh URL (exit peer SOCKS5 proxy)
# ================================================================
if [ "$SKIP_MESH" != "1" ]; then

# ----------------------------------------------------------------
# Step 7: Create mesh route on target cluster
# ----------------------------------------------------------------
step "7. Create mesh route on $TARGET_CLUSTER for $TEST_SVC:$TEST_PORT"
MESH_CREATE_OUT=$("$PRYSM" mesh routes create \
  --cluster "$TARGET_CLUSTER" \
  --name "$MESH_ROUTE_NAME" \
  --service "$TEST_SVC" \
  --service-port "$TEST_PORT" \
  --protocol "tcp" 2>&1)
echo "$MESH_CREATE_OUT"
if echo "$MESH_CREATE_OUT" | grep -qi "created\|route.*[0-9]"; then
  pass "mesh route created"
  MESH_ROUTE_ID=$(echo "$MESH_CREATE_OUT" | grep -oE '[Rr]oute [0-9]+' | head -1 | grep -oE '[0-9]+')
  echo "Mesh Route ID: ${MESH_ROUTE_ID:-unknown}"
  # Extract the external port from output (e.g. "via ccrechotest.hp.mesh:30001")
  MESH_EXT_PORT=$(echo "$MESH_CREATE_OUT" | grep -oE '\.mesh:[0-9]+' | head -1 | cut -d: -f2)
  if [ -z "${MESH_EXT_PORT:-}" ]; then
    MESH_EXT_PORT=$(echo "$MESH_CREATE_OUT" | grep -oE ':[0-9]{4,5}' | head -1 | tr -d ':')
  fi
  echo "External port: ${MESH_EXT_PORT:-unknown}"
else
  fail "mesh route creation failed"
fi

# ----------------------------------------------------------------
# Step 8: List mesh routes — should include new route
# ----------------------------------------------------------------
step "8. List mesh routes"
MESH_LIST_OUT=$("$PRYSM" mesh routes list --cluster "$TARGET_CLUSTER" 2>&1)
echo "$MESH_LIST_OUT"
if echo "$MESH_LIST_OUT" | grep -q "$TEST_SVC"; then
  pass "mesh route appears in list"
else
  fail "mesh route not found in list"
fi

# ----------------------------------------------------------------
# Step 9: Start mesh with SOCKS5 proxy
# ----------------------------------------------------------------
step "9. Start mesh with SOCKS5 proxy (port $SOCKS_PORT)"

# Kill any previous mesh process on this port
if nc -z 127.0.0.1 "$SOCKS_PORT" 2>/dev/null; then
  info "Port $SOCKS_PORT already in use, disconnecting previous mesh"
  "$PRYSM" mesh disconnect 2>/dev/null || true
  sleep 2
fi

> /tmp/prysm-mesh-test.log
nohup "$PRYSM" mesh connect --foreground --socks5-port "$SOCKS_PORT" >> /tmp/prysm-mesh-test.log 2>&1 </dev/null &
MESH_PID=$!
disown "$MESH_PID" 2>/dev/null || true
echo "Mesh PID $MESH_PID"

# Wait for SOCKS5 to come up
for i in $(seq 1 30); do
  sleep 1
  if nc -z 127.0.0.1 "$SOCKS_PORT" 2>/dev/null; then
    echo "SOCKS5 ready after ${i}s"
    break
  fi
  if [ "$i" -eq 30 ]; then
    fail "SOCKS5 did not come up (check /tmp/prysm-mesh-test.log)"
    tail -20 /tmp/prysm-mesh-test.log 2>/dev/null || true
    exit 1
  fi
done
sleep 3  # give agent time to sync routes

# ----------------------------------------------------------------
# Step 10: Curl via mesh
# ----------------------------------------------------------------
TARGET_SLUG=$(slug "$TARGET_CLUSTER")
ROUTE_SLUG=$(slug "$MESH_ROUTE_NAME")

# Use external port if we extracted it, otherwise fall back to service port
CURL_PORT="${MESH_EXT_PORT:-$TEST_PORT}"
MESH_URL="http://${ROUTE_SLUG}.${TARGET_SLUG}.mesh:${CURL_PORT}/"

step "10. Curl via mesh: $MESH_URL"
info "curl --proxy socks5h://127.0.0.1:$SOCKS_PORT $MESH_URL"
CURL_OUT=$(curl -s -w "\nHTTP_CODE=%{http_code} TIME=%{time_total}s\n" \
  --connect-timeout 20 --max-time 30 \
  --proxy "socks5h://127.0.0.1:$SOCKS_PORT" \
  "$MESH_URL" 2>&1) || true
echo "$CURL_OUT"

if echo "$CURL_OUT" | grep -qi "nginx\|welcome\|HTTP_CODE=200"; then
  pass "curl via .mesh returned nginx response"
elif echo "$CURL_OUT" | grep -qE "HTTP_CODE=[1-5][0-9][0-9]"; then
  HTTP_CODE=$(echo "$CURL_OUT" | grep -oE 'HTTP_CODE=[0-9]+' | cut -d= -f2)
  if [ "$HTTP_CODE" != "000" ]; then
    pass "curl via .mesh got HTTP $HTTP_CODE (route reachable, service responded)"
  else
    fail "curl via .mesh timed out (HTTP 000) — exit peer may not be routing"
    info "Check /tmp/prysm-mesh-test.log for DERP errors"
  fi
else
  fail "curl via .mesh failed — exit node or service unreachable"
  info "Mesh log tail:"
  tail -10 /tmp/prysm-mesh-test.log 2>/dev/null || true
fi

# ----------------------------------------------------------------
# Step 11: Disconnect mesh
# ----------------------------------------------------------------
step "11. Disconnect mesh"
kill "$MESH_PID" 2>/dev/null || true
MESH_PID=""
"$PRYSM" mesh disconnect 2>/dev/null || true
pass "mesh disconnected"

# ----------------------------------------------------------------
# Step 12: Delete mesh route
# ----------------------------------------------------------------
if [ -n "${MESH_ROUTE_ID:-}" ]; then
  step "12. Delete mesh route $MESH_ROUTE_ID"
  MESH_DEL_OUT=$("$PRYSM" mesh routes delete "$MESH_ROUTE_ID" 2>&1)
  echo "$MESH_DEL_OUT"
  if echo "$MESH_DEL_OUT" | grep -qi "deleted"; then
    pass "mesh route deleted"
    MESH_ROUTE_ID=""  # prevent cleanup from trying again
  else
    fail "mesh route delete failed"
  fi
fi

fi  # SKIP_MESH

# ----------------------------------------------------------------
# Summary
# ----------------------------------------------------------------
echo ""
echo "======================================="
echo -e "Results: ${GREEN}${PASS} passed${NC}, ${RED}${FAIL} failed${NC}"
echo "======================================="
[ "$FAIL" -eq 0 ] || exit 1
