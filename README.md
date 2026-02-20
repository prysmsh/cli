# Prysm CLI

Command-line interface for Prysm zero-trust infrastructure management. Includes DERP-based mesh networking.

## Installation

### From Source (Go Install)

```bash
go install github.com/warp-run/prysm-cli/cmd/prysm@latest
```

This installs the `prysm` binary to `$GOPATH/bin/` (typically `~/go/bin/`). Make sure this directory is in your PATH:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### From Release Binary

```bash
curl -fsSL https://prysm.sh/install/agent | sh
```

### From Package Manager

**Debian/Ubuntu:**
```bash
wget https://github.com/prysmsh/prysm/releases/latest/download/prysm-cli_amd64.deb
sudo dpkg -i prysm-cli_amd64.deb
```

**RHEL/CentOS:**
```bash
wget https://github.com/prysmsh/prysm/releases/latest/download/prysm-cli-x86_64.rpm
sudo rpm -i prysm-cli-x86_64.rpm
```

## Features

- **Authentication**: Browser-based login (GitHub, Apple, or email/password)
- **Cluster Access**: Generate kubeconfigs for zero-trust cluster access
- **Mesh Networking**: Mesh networking with DERP relay
  - `prysm mesh connect` - Join the DERP mesh
  - `prysm mesh peers` - List mesh peers
  - `prysm mesh routes` - Manage mesh routes
- **Session Management**: Cached credentials and organization context
- **Audit Logs**: Access compliance and audit trail

## Quick Start

### 1. Authenticate

```bash
prysm login
# Opens the browser to sign in (GitHub, Apple, or email/password)
# Or: prysm login --github / prysm login --apple  (skip to that provider)
```

### 2. Access Kubernetes Cluster

```bash
prysm connect k8s --cluster my-cluster --output ~/.kube/prysm-config
export KUBECONFIG=~/.kube/prysm-config
kubectl get pods
```

### 3. Set Up Mesh Networking

```bash
# Connect to DERP mesh
prysm mesh connect

# List mesh peers
prysm mesh peers

# Manage mesh routes
prysm mesh routes
```

## Commands

### Authentication
- `prysm login` - Authenticate (opens browser; supports GitHub, Apple, email/password)
- `prysm logout` - Clear session
- `prysm session status` - Show current session info

### Cluster Access
- `prysm connect k8s` - Generate kubeconfig for cluster access

### Mesh Networking
- `prysm mesh connect` - Join DERP mesh
- `prysm mesh peers` - List mesh peers
- `prysm mesh routes` - Manage mesh routes
- `prysm mesh exit enable` - Enable a mesh node as exit node
- `prysm mesh exit disable` - Disable a mesh node as exit node

### Audit
- `prysm audit` - View audit logs

## Configuration

The CLI reads configuration from:
1. Command-line flags
2. Environment variables (prefixed with `PRYSM_`)
3. Config file: `~/.prysm/config.yaml`

### Environment Variables

- `PRYSM_API_URL` - Override API base URL
- `PRYSM_DERP_URL` - Override DERP relay URL
- `PRYSM_COMPLIANCE_URL` - Override compliance API URL

### Config File Example

```yaml
profiles:
  default:
    api_url: https://api.prysm.sh/api/v1
    derp_url: wss://derp.prysm.sh/derp
  staging:
    api_url: https://staging.api.prysm.sh/api/v1
    derp_url: wss://derp.staging.prysm.sh/derp
```

Use a profile:
```bash
prysm --profile staging login
```

## Development

### Build

```bash
cd prysm-cli
go build -o prysm ./cmd/prysm
```

### Test

```bash
go test ./...
```

### Run Without Installing

```bash
go run ./cmd/prysm [command]
```

## Architecture

```
prysm (single binary)
├── CLI commands
│   ├── login, logout
│   ├── connect (kubeconfig)
│   ├── session management
│   └── audit logs
└── mesh subcommands
    ├── connect (DERP mesh)
    ├── peers (list peers)
    ├── routes (mesh routes)
    └── exit (enable/disable exit nodes)
```

## Troubleshooting

### "command not found" after go install

Add Go's bin directory to your PATH:
```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.bashrc
source ~/.bashrc
```

## See Also

- [Mesh Networking Quick Start](../docs/MESH_NETWORKING_QUICKSTART.md)
- [CLI Validation Guide](../spec/CLI_STAGING_VALIDATION.md)
- [Architecture Overview](../spec/architecture.md)

## License

See [LICENSE](../LICENSE) file.
