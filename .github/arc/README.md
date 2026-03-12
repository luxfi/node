# ARC (Actions Runner Controller) - Self-Hosted Runners

Self-hosted GitHub Actions runners on lux-k8s for fast native builds.

## Status

- Controller: `arc-system` namespace, running
- Runners: `arc-runners` namespace, **needs auth setup**

## Quick Setup

### 1. Create GitHub App (recommended)

Go to <https://github.com/organizations/luxfi/settings/apps/new>:

| Field | Value |
|-------|-------|
| Name | `lux-arc-runner` |
| Homepage | `https://lux.network` |
| Webhook | Uncheck "Active" |

**Permissions (Organization):**
- Self-hosted runners: Read & Write
- Administration: Read-only

**Permissions (Repository):**
- Actions: Read-only
- Metadata: Read-only

After creating:
1. Note the **App ID** (shown on app page)
2. Generate a **private key** (.pem download)
3. Install the app on **luxfi** organization (all repos)
4. Note the **Installation ID** from the URL: `github.com/organizations/luxfi/settings/installations/<ID>`

### 2. Install Runner Scale Set

```bash
export GITHUB_APP_ID=<app-id>
export GITHUB_APP_INSTALLATION_ID=<installation-id>
export GITHUB_APP_PRIVATE_KEY_FILE=<path-to-pem>

.github/arc/setup.sh runners
```

### 3. Use in Workflows

```yaml
jobs:
  build:
    runs-on: [self-hosted, linux, amd64, lux-build]
    steps:
      - uses: actions/checkout@v4
      # ... build steps
```

## Alternative: PAT Auth

If using a Personal Access Token instead of GitHub App:

```bash
kubectl --context do-sfo3-lux-k8s create secret generic arc-github-pat \
  --namespace arc-runners \
  --from-literal=github_token="ghp_xxxxx"

helm --kube-context do-sfo3-lux-k8s upgrade --install arc-runner-set \
  oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set \
  --namespace arc-runners \
  --set githubConfigUrl="https://github.com/luxfi" \
  --set githubConfigSecret=arc-github-pat \
  --set minRunners=1 \
  --set maxRunners=6
```

PAT needs scopes: `admin:org`, `repo`

## Architecture

```
lux-k8s cluster (4x linux/amd64)
├── arc-system/       (ARC controller)
└── arc-runners/      (ephemeral runner pods)
    ├── 1-6 pods auto-scaled by job queue depth
    ├── Go cache (emptyDir, per-pod)
    └── 20Gi workspace per runner
```

## Platform Coverage

| Platform | Runner | Type |
|----------|--------|------|
| Linux amd64 | ARC self-hosted | Native |
| Linux arm64 | GitHub-hosted or cross-compile | CGO_ENABLED=0 |
| macOS arm64 | GitHub-hosted `macos-latest` | Native |
| Windows x64 | GitHub-hosted `windows-latest` | Native |
