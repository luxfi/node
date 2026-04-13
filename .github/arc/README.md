# Native Build Infrastructure

## Cluster Ownership (NO MIXING)

| Cluster | Org | Purpose |
|---------|-----|---------|
| **lux-k8s** | `luxfi` | Lux blockchain infrastructure + CI runners |
| **hanzo-k8s** | `hanzoai` | Hanzo AI services + CI runners |

## lux-k8s ARC Setup

```
lux-k8s (do-sfo3-lux-k8s)
├── arc-system/                        (ARC v0.13.1 controller)
│   └── lux-build listener             (luxfi org)
└── lux-runners-amd64/                 (1-4 auto-scaling nodes)
    └── g-8vcpu-32gb nodes             (dedicated CI, NoSchedule taint)
        └── Ephemeral runner pods      (Docker-in-Docker, 8 CPU / 28Gi)
```

## Platform Matrix — All Native

| Platform | Runner | Type |
|----------|--------|------|
| Linux amd64 | `lux-build` | Self-hosted on lux-k8s (8 CPU, 32Gi) |
| Linux arm64 | `ubuntu-24.04-arm` | GitHub-hosted native Ampere |
| macOS arm64 | `macos-latest` | GitHub-hosted native Apple Silicon |
| Windows x64 | `windows-latest` | GitHub-hosted native |

**Note**: DOKS sfo3 has no real ARM64 nodes (`g6_5` is premium AMD64, not Ampere).
Native arm64 builds use GitHub's Arm runners instead.

## Helm Releases (lux-k8s arc-system)

| Release | Chart | Purpose |
|---------|-------|---------|
| `arc` | gha-runner-scale-set-controller | ARC controller |
| `lux-build` | gha-runner-scale-set | amd64 runners for luxfi |

## Values

- `values-amd64.yaml` — lux-build runner config (checked into repo)
