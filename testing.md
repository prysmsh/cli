# Mesh Route E2E Test Memory

Date: 2026-02-23

## Goal
Validate `mesh connect` + subnet/TUN routing by curling an active mesh route directly (no SOCKS proxy).

## Known-good route
- Host: `teste2e.frank.mesh`
- Port: `30000`
- Expected body: `Mesh route OK from api`
- Expected status: `HTTP 200`

## Reproduction Steps
1. Build CLI:
```bash
cd /home/alessio/prysm/cli
GOWORK=off go build -o /tmp/prysm-dev ./cmd/prysm/
```

2. Connect to mesh using subnet routing (no socks5):
```bash
sudo /tmp/prysm-dev mesh connect --foreground --socks5-port 0
```

3. In another shell, curl route directly:
```bash
curl -v http://teste2e.frank.mesh:30000/
```

## If You Get "Connection Refused"
Likely stale iptables NAT redirect rule from an old mesh session is matching before the active redirect.

### Diagnose
```bash
sudo iptables -t nat -L OUTPUT -n | grep 10.233
sudo ss -tlnp | grep prysm
```

### Fix
Delete stale redirect rule(s) that point to ports with no listener:
```bash
sudo iptables -t nat -D OUTPUT -p tcp -d 10.233.0.11 -j REDIRECT --to-ports <stale-port>
```

Then retry:
```bash
curl -v http://teste2e.frank.mesh:30000/
```

## Root Cause Note
If `mesh connect` is killed without running `mesh disconnect`, cleanup may not run and stale NAT rules can remain.

## Cleanup
Always disconnect cleanly:
```bash
sudo /tmp/prysm-dev mesh disconnect
```
