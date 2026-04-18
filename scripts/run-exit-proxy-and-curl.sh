#!/bin/bash
# Start the SOCKS5 exit proxy in the background, wait for it to listen, then run curl.
# Usage: ./scripts/run-exit-proxy-and-curl.sh [curl URL]
# Example: ./scripts/run-exit-proxy-and-curl.sh http://derp.frank-local:30000/health

set -e
CDIR="$(cd "$(dirname "$0")" && pwd)"
CLI="${CDIR}/../prysm"
URL="${1:-http://derp.frank-local:30000/health}"
PORT=1080

# Start proxy in background
"$CLI" mesh exit use --port "$PORT" &
PID=$!
trap "kill $PID 2>/dev/null" EXIT

# Wait for port to be listening
wait_for_port() {
  for i in $(seq 1 15); do
    if (ss -tlnp 2>/dev/null || netstat -tln 2>/dev/null) | grep -q "[:.]$PORT "; then
      return 0
    fi
    sleep 1
  done
  return 1
}
wait_for_port || { echo "Proxy did not start on port $PORT"; kill $PID 2>/dev/null; exit 1; }

echo "Proxy ready on 127.0.0.1:$PORT, running curl..."
curl --proxy "socks5h://127.0.0.1:$PORT" "$URL"
