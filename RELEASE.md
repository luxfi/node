# Lux release — build + publish via platform.hanzo.ai (the ONE canonical way)

This is the single, repeatable way to build and publish the Lux release
artifacts. It runs entirely on our own infrastructure — **the PaaS
(platform.hanzo.ai) + self-hosted arcd runners + DOKS/fleet**. There is **no
GitHub Actions build path** (the `.github/workflows/*` build/release workflows
are retired — see [§Retire](#retire-the-github-actions-build-workflows)).

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
git tag vX.Y.Z (push)
        │  GitHub App webhook ─▶ https://platform.hanzo.ai/v1/github-webhook
        ▼
  platform BuildScheduler  ── reads hanzo.yml @ tag, validates, enqueues
        │                      one build_job per matrix entry
        ▼
  native long-poll fabric (build-queue.ts)        NO GitHub Actions hop
        │  arcd runner POST /v1/arcd/poll  (HMAC)
        ▼
  arcd runner on pool lux-build-linux-<arch>
        │  git checkout @ tag → docker build -f Dockerfile . → docker push
        ▼
  ghcr.io/luxfi/node:vX.Y.Z         (artifact 1)
        │  POST /v1/arcd/complete (status, image_digest)
        ▼
  build_job (DB, system-of-record)
```

- **PaaS**: `platform.hanzo.ai` (`~/work/hanzo/platform`,
  `pkg/platform/src/services/ci/`). Owns the schema, the scheduler, the durable
  `build_job` record, and the native long-poll dispatch.
- **Build muscle**: self-hosted **arcd** runner pools, `lux-build-linux-amd64`
  and `lux-build-linux-arm64` (one daemon per fleet host; `spark` = linux/arm64,
  amd64 via buildx). NO GitHub-hosted runners; NO GitHub Actions orchestration
  on the native path.
- **Contract**: `~/work/hanzo/platform/docs/PLATFORM_CI.md`.

## Build — the one command

A release is a semver tag push. The declarative entrypoint is this repo's
[`hanzo.yml`](./hanzo.yml); the trigger is one of:

**(a) Tag push (normal release).** Cutting the tag IS the release:

```bash
git tag v1.30.41 && git push origin v1.30.41
```

The webhook maps `refs/tags/v1.30.41` → `branch=v1.30.41`; `hanzo.yml`'s
`tag-pattern: "{{git.branch}}"` yields the image tag `v1.30.41`. Platform
schedules the amd64 + arm64 builds onto the live `lux-build-*` arcd pools and
pushes the multi-arch image to GHCR.

**(b) On-demand (re-release / backfill).** The platform `buildJob.trigger`
tRPC mutation schedules the same build for an explicit ref, no push required:

```
buildJob.trigger({
  installationId: "<luxfi GitHub App installation id>",
  repo: "luxfi/node",
  sha:  "<commit at the tag>",
  ref:  "refs/tags/v1.30.41",
  branch: "v1.30.41"            // → image tag via {{git.branch}}
})
```

Track it: `buildJob.list` / `buildJob.one` / `buildJob.logs` (org-scoped).

> A pool goes **native** the moment an arcd runner self-registers for it
> (`arcd_runner.lastSeen` within 90s); until then platform transparently falls
> back to `workflow_dispatch` so a build is never stranded. To run a release
> fully GitHub-free, ensure a `lux-build-linux-{amd64,arm64}` runner is live
> (`tRPC arcd` / the `arcd_runner` table). Set platform env
> `WORKFLOW_DISPATCH_FALLBACK=false` to forbid the legacy hop.

### What the runner runs (identical on a fleet host, for manual/DR builds)

The native path runs exactly the repo's `Dockerfile`. To reproduce on a fleet
host directly (e.g. `spark`), with no platform and no GitHub:

```bash
# on spark (linux/arm64; amd64 via buildx)
git clone --branch v1.30.41 git@github.com:luxfi/node.git && cd node
docker buildx build --platform linux/amd64 \
  --build-arg CGO_ENABLED=0 \
  -t ghcr.io/luxfi/node:v1.30.41 -f Dockerfile --push .
```

## Publish the plugin set — step 2

After the image exists, publish artifact 2 from it (one command, idempotent,
no second compile). Run on any fleet host or a DOKS Job that has `crane`/docker
+ `mc`; typically the same arcd runner that just built the image:

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

## Retire the GitHub Actions build workflows

These `.github/workflows/*` build/release/CI workflows are superseded by this
flow and must be removed/disabled (platform owns build; the native long-poll
owns dispatch). Delete them once a `lux-build-*` arcd runner is live:

| Workflow | Replaced by |
|----------|-------------|
| `docker.yml` (built `ghcr.io/luxfi/node` on the `lux-build` ARC pool) | `hanzo.yml` (artifact 1) — native long-poll, NO GitHub Actions |
| `release.yml` | the tag-push trigger above + `scripts/publish_plugin_set.sh` |
| `build.yml`, `ci.yml` | platform CI test step (runner runs `go test` pre-build) |
| `build-linux-binaries.yml` | `Dockerfile` builder stage (luxd binary) |
| `build-ubuntu-amd64-release.yml`, `build-ubuntu-arm64-release.yml` | `Dockerfile` + buildx multi-arch |
| `build-macos-release.yml`, `build-win-release.yml`, `build-and-test-mac-windows.yml` | arcd `lux-build-{macos,windows}-*` pools (matrix in `hanzo.yml` when desired) |
| `build-deb-pkg.sh`, `build-tgz-pkg.sh` (under `.github/workflows/`) | packaging step on the arcd runner (post-build), not GitHub Actions |
| `codeql-analysis.yml`, `fuzz.yml`, `fuzz_merkledb.yml`, `test-database-replay.yml` | scheduled jobs on arcd / DOKS (not a build dependency) |
| `buf-lint.yml`, `buf-push.yml`, `labels.yml`, `stale.yml` | repo-hygiene; migrate to arcd cron or drop |

The same retirement applies to the equivalent build/release workflows in the
plugin-source repos (`luxfi/evm`, `luxfi/chains`, `luxfi/dex`): their artifacts
are built from source by THIS repo's `Dockerfile` at the pinned refs, so those
repos need no independent image/release CI — only their tags. Migrate each by
adding a `hanzo.yml` (if it ships its own image) or deleting its build CI (if it
is consumed only as a Go module / plugin source here).
