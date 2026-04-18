#!/usr/bin/env bash
# Test: create route, list routes, then curl via mesh SOCKS5.
# Prereqs: prysm login, cluster with exit enabled (e.g. frank).
# Usage: PRYSM_BIN=./prysm ./scripts/test-mesh-route-curl.sh [cluster-name-or-id] [route-name]
#        Or to only list + curl (no create): SKIP_CREATE=1 TARGET=teste2e.frank.mesh:30000 ./scripts/test-mesh-route-curl.sh

set -e
PRYSM="${PRYSM_BIN:-prysm}"
CLUSTER="${1:-${CLUSTER_ID:-frank}}"
ROUTE_NAME="${2:-test-curl}"
SERVICE="${3:-api}"
SERVICE_PORT="${4:-8080}"
SOCKS_PORT="${SOCKS_PORT:-1080}"

if [ "${SKIP_CREATE:-0}" != "1" ]; then
  echo "=== 1. Create route (cluster=$CLUSTER name=$ROUTE_NAME service=$SERVICE:$SERVICE_PORT) ==="
  CREATE_OUT=$("$PRYSM" mesh routes create \
    --cluster "$CLUSTER" \
    --service "$SERVICE" \
    --service-port "$SERVICE_PORT" \
    --name "$ROUTE_NAME" \
    --external-port 0)
  echo "$CREATE_OUT"
  # Parse "via HOST:PORT" from create output
  TARGET="${TARGET:-$(echo "$CREATE_OUT" | grep -oE 'via [^ ]+:[0-9]+' | sed 's/^via //')}"
fi

echo ""
echo "=== 2. List routes ==="
"$PRYSM" mesh routes list

if [ -z "$TARGET" ]; then
  # Fallback: first route TARGET from list (column 4)
  TARGET=$( "$PRYSM" mesh routes list 2>/dev/null | awk 'NR==2 { print $4 }')
fi
if [ -z "$TARGET" ]; then
  TARGET="derp.$CLUSTER:30000"
fi
echo "TARGET for curl: $TARGET"

echo ""
echo "=== 3. Start mesh with SOCKS5 (port $SOCKS_PORT) in background ==="
MESH_LOG="/tmp/prysm-mesh-test.log"
: > "$MESH_LOG"
CONNECT_ARGS=(mesh connect --foreground)
if "$PRYSM" mesh connect --help 2>&1 | grep -q -- '--socks5-port'; then
  CONNECT_ARGS+=(--socks5-port "$SOCKS_PORT")
else
  echo "mesh connect does not support --socks5-port; using CLI default SOCKS5 port"
fi
nohup "$PRYSM" "${CONNECT_ARGS[@]}" >> "$MESH_LOG" 2>&1 </dev/null &
MESH_PID=$!
disown $MESH_PID 2>/dev/null || true
echo "Mesh PID $MESH_PID"
for i in $(seq 1 30); do
  sleep 1
  if nc -z 127.0.0.1 "$SOCKS_PORT" 2>/dev/null; then
    echo "SOCKS5 ready after ${i}s"
    break
  fi
  [ $i -eq 30 ] && { echo "SOCKS5 did not come up (check ~/.prysm/derp-connect.log or /tmp/prysm-mesh-test.log)"; kill $MESH_PID 2>/dev/null; exit 1; }
done
sleep 2

echo ""
echo "=== 4. Curl via mesh ==="
echo "curl --proxy socks5h://127.0.0.1:$SOCKS_PORT http://$TARGET/"
if curl -s -w "\nHTTP %{http_code} time %{time_total}s\n" --connect-timeout 15 --proxy "socks5h://127.0.0.1:$SOCKS_PORT" "http://$TARGET/"; then
  echo "Curl completed."
else
  echo "Curl failed (exit $?) - exit node or service may be unreachable."
fi

echo ""
echo "=== 5. Stop mesh ==="
kill $MESH_PID 2>/dev/null || true
echo "Done."
