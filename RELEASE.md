# Lux release — build + publish (the ONE canonical way)

This is the single, repeatable way to build and publish the Lux release
artifacts. It runs entirely on our own infrastructure — **Hanzo Git Actions
(`git.hanzo.ai`) + the self-hosted runner fleet + DOKS**. GitHub is a mirror,
never the compute.

## What a release produces

ONE `Dockerfile` multi-stage build (this repo) is the single source of truth.
It compiles `luxd` + all 12 VM plugins (CGO_ENABLED=0) and yields TWO
distribution surfaces:

| # | Artifact | Destination | Consumed by |
|---|----------|-------------|-------------|
| 1 | node image (luxd + 12 plugins baked at `/luxd/build/plugins/`) | `ghcr.io/luxfi/node:vX.Y.Z` | operator pod image; `startup.sh cp /luxd/build/plugins/*` |
| 2 | plugin set (the 12 VM-ID binaries + `SHA256SUMS`) | `s3://lux-plugins-<env>/<pluginset>/` | operator `plugin-fetch` init container (LuxNetwork CR `pluginSource`) |

Artifact 2 is **extracted from** artifact 1 — the plugins are never compiled
twice. One build, two surfaces (DRY, orthogonal).

The plugin versions are pinned as Dockerfile build-args, kept in lockstep with
this repo's `go.mod`:

- `EVM_VERSION` (luxfi/evm — C-Chain EVM, the `0x9999` settlement surface)
- `CHAINS_REF` (luxfi/chains — the 10 non-DEX VMs incl. bridgevm)
- `DEX_REF` (luxfi/dex `cmd/dchain` — the native D-Chain DEX VM)

## The machinery

```
git tag vX.Y.Z (push to git.hanzo.ai)
        ▼
  .hanzo/workflows/release.yml        Hanzo Git Actions
        │  runs-on: lux-build-linux-amd64
        ▼
  act_runner on the git-runner fleet
        │  checkout @ tag → buildx build -f Dockerfile . → push
        ▼
  ghcr.io/luxfi/node:vX.Y.Z          (artifact 1)
```

- **Workflow**: [`.hanzo/workflows/release.yml`](./.hanzo/workflows/release.yml)
  — the one thing that builds and pushes this image.
- **Build muscle**: the self-hosted runner fleet registered to `git.hanzo.ai`.
  Label taxonomy: `hanzoai/.github:RUNNERS.md`.

## Build — the one command

A release is a semver tag push. Cutting the tag IS the release:

```bash
git tag v1.30.41 && git push origin v1.30.41
```

`release.yml` builds `ghcr.io/luxfi/node:v1.30.41` and pushes it. The tag is
immutable and semver-only — no `:latest`, no floating tags.

To re-run a release without moving the tag, dispatch the same workflow from
Hanzo Git Actions against the tag ref.

### What the runner runs (identical on a fleet host, for manual/DR builds)

The native path runs exactly the repo's `Dockerfile`. To reproduce on a fleet
host directly (e.g. `spark`), with no CI at all:

```bash
# on spark (linux/arm64; amd64 via buildx)
git clone --branch v1.30.41 https://git.hanzo.ai/luxfi/node.git && cd node
docker buildx build --platform linux/amd64 \
  --build-arg CGO_ENABLED=0 \
  -t ghcr.io/luxfi/node:v1.30.41 -f Dockerfile --push .
```

## Publish the plugin set — step 2

After the image exists, publish artifact 2 from it (one command, idempotent,
no second compile). Run on any fleet host or a DOKS Job that has `crane`/docker
+ `mc`; typically the same runner that just built the image:

```bash
scripts/publish_plugin_set.sh \
  ghcr.io/luxfi/node:v1.30.41 \
  lux-plugins-<env>/<pluginset> \
  lux                          # mc alias for the target MinIO/S3
# e.g. lux-plugins-testnet/v1.3.5
```

It extracts the 12 plugin binaries from the image, writes `SHA256SUMS`, uploads
all to `s3://lux-plugins-<env>/<pluginset>/`, and verifies remote==local sha.

S3 is the in-cluster MinIO (`s3.lux-system.svc.cluster.local:9000`, external
`s3.lux.network`). Configure the `mc` alias once with the `hanzo-s3-secret`
credentials:

```bash
mc alias set lux <endpoint> hanzo "$(kubectl -n lux-system get secret \
  hanzo-s3-secret -o jsonpath='{.data.password}' | base64 -d)" --api s3v4
```

A pluginset prefix is **immutable** — bump `<pluginset>` for a new release,
never overwrite a prefix a live network points at.

## Deploy — step 3 (operator, not this repo)

luxd rollout is owned by the **lux operator** (`~/work/lux/operator`,
`LuxNetwork` CR). Update the CR's `image.tag` (artifact 1) and, when the
network fetches plugins from S3, the `pluginSource.bucket` + per-plugin
`sha256` (artifact 2, from the `SHA256SUMS` you just published). The operator's
`plugin-fetch` init container verifies each sha256 fail-closed. This is
deliberately decoupled from build: `hanzo.yml` has **no `deploy:` block**.

## Reproducibility

- The build is **functionally reproducible**: same source tags + same toolchain
  (Go 1.26.4) + `CGO_ENABLED=0` ⇒ functionally identical plugins, provable by a
  fleet rebuild (verified: `spark` rebuilt evm@v1.99.37 + dexvm@v1.5.15 from the
  same tags). It is **not bit-identical by construction**: the Dockerfile plugin
  stages omit `-trimpath` and use `-mod=mod` with a first-party `go.sum` strip
  (re-resolves luxfi/* deps), so embedded paths + re-tagged module content can
  shift the bytes (Go `BuildID` differs; binary ~16 KB larger). The published
  image is the canonical artifact; verify against ITS baked sha (what
  `publish_plugin_set.sh` records), not a separate fleet build.
- To make releases bit-reproducible (future hardening, patch-only): add
  `-trimpath` to every plugin `go build` and pin `go.sum` (drop the strip +
  `-mod=mod`). Tracked as a follow-up; not required for correctness.
