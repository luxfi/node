# The version is supplied as a build argument rather than hard-coded
# to minimize the cost of version changes. Must be >= the `go` directive
# in go.mod (1.26.4); the EVM plugin pulls luxfi/upgrade@v1.0.1 which
# floors the toolchain at 1.26.4. Pinned to the latest stable patch so the
# image ships current compiler/stdlib security fixes; the builder stage also
# sets GOTOOLCHAIN=auto so a future floor bump downloads rather than fails.
ARG GO_VERSION=1.26.5

# ============= Go Installation Stage ================
FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS go-installer

RUN apt-get update && apt-get install -y --no-install-recommends \
    wget ca-certificates \
    && rm -rf /var/lib/apt/lists/*

ARG GO_VERSION
ARG BUILDPLATFORM

# Download Go for build platform
RUN BUILDARCH=$(echo ${BUILDPLATFORM} | cut -d / -f2) && \
    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-${BUILDARCH}.tar.gz" && \
    tar -C /usr/local -xzf "go${GO_VERSION}.linux-${BUILDARCH}.tar.gz" && \
    rm "go${GO_VERSION}.linux-${BUILDARCH}.tar.gz"

# ============= Compilation Stage ================
# Always use the native platform to ensure fast builds
FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS builder

# Copy Go from installer stage
COPY --from=go-installer /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

# A go.mod `go` directive newer than the toolchain above is a hard failure
# under GOTOOLCHAIN=local. `auto` lets go fetch the required toolchain
# (checksum-verified via sum.golang.org) instead of dying at the floor.
ENV GOTOOLCHAIN=auto

# Install build dependencies (ca-certificates needed for go mod download)
# libc6-dev-arm64-cross needed for cross-compiling to ARM64
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc libc6-dev make git ca-certificates wget \
    gcc-aarch64-linux-gnu gcc-x86-64-linux-gnu \
    libc6-dev-arm64-cross libc6-dev-amd64-cross \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Skip checksum verification for luxfi + hanzoai packages: both are cross-org
# deps not registered in the public sum.golang.org / proxy (reading e.g.
# hanzoai/vfs@v0.4.1's go.mod via the public sumdb 404s and fails the build).
# GOPRIVATE is the one that governs checksum verification; GONOSUMCHECK predates
# modules and Go ignores it. Our own modules must be listed here: a tag that was
# ever moved no longer matches what the public checksum database recorded for it,
# and every later build that resolves that version dies on
#   verifying go.mod: checksum mismatch / SECURITY ERROR
# even though nothing in the build is wrong. The plugin stages resolve transitive
# luxfi versions we do not choose, so the exclusion has to cover the whole org.
ENV GOPRIVATE=github.com/luxfi/*,github.com/lux-private/*,github.com/hanzoai/*
# GONOPROXY is narrower than GOPRIVATE on purpose: keep fetching through the proxy
# (gonum.org is flaky direct), and go direct only for the modules the proxy cannot
# see. Setting GOPRIVATE alone would silently take everything off the proxy.
ENV GOPROXY=https://proxy.golang.org,direct
ENV GONOPROXY=github.com/lux-private/*,github.com/hanzoai/*
ENV GOFLAGS="-mod=mod"

# Copy and download lux dependencies using go mod.
# Some luxfi/* modules (e.g. corona) are private and require a token to
# resolve via go mod. The token is injected as a BuildKit secret from the
# CI workflow; locally, set DOCKER_BUILDKIT=1 and pass
# `--secret id=ghtok,src=$HOME/.gh-token`. If the secret is absent the
# build still attempts the download (works when all deps are public).
COPY go.mod .
COPY go.sum .
# Every module this builds from is PUBLIC — go.mod names nothing under
# lux-private/ or luxcpp/ — so the module download authenticates to nothing and
# fetches anonymously. The credential below is scoped to the two private orgs
# only, so that a private module added later resolves without this step having
# to be rediscovered, while github.com at large stays anonymous.
#
# Scoping is the point, not tidiness. Three release builds failed here:
#
#   * Twice because the token was interpolated into a URL's authority —
#     `url."https://x-access-token:$(cat …)@github.com/".insteadOf` — where a
#     single `/` in the token ends the authority early and git resolves the
#     credential prefix as a hostname:
#         Could not resolve host: x-access-token
#     which reads as DNS rather than as quoting.
#
#   * Once because the fix for that sent the token as an Authorization header
#     for ALL of github.com. The token is a registry credential: it logs in to
#     ghcr.io, and it cannot read repositories. Presenting it made GitHub answer
#     401 for github.com/hanzoai/vfs — a PUBLIC repository that had been served
#     anonymously all along:
#         fatal: could not read Username for 'https://github.com'
#     A wrong credential is worse than none, because none is a 200.
#
# base64 is used rather than URL interpolation because a header value has no
# character that can truncate it, so this stays correct when the token is next
# rotated to a different shape.
RUN --mount=type=secret,id=ghtok,required=false \
    if [ -s /run/secrets/ghtok ]; then \
        auth="Authorization: Basic $(printf 'x-access-token:%s' "$(cat /run/secrets/ghtok)" | base64 | tr -d '\n')"; \
        git config --global http."https://github.com/lux-private/".extraheader "$auth"; \
        git config --global http."https://github.com/luxcpp/".extraheader "$auth"; \
    fi && \
    sed -i -E '/^github.com\/(luxfi|hanzoai)\//d' go.sum && \
    go mod download -x

# Copy the code into the container
COPY . .

# Ensure pre-existing builds are not available for inclusion in the final image
RUN [ -d ./build ] && rm -rf ./build/* || true

ARG TARGETPLATFORM
ARG BUILDPLATFORM

# Per SCALE_STANDARD.md §2 (https://github.com/hanzoai/hips/blob/main/docs/SCALE_STANDARD.md)
# — every Go production Dockerfile that emits JSON to a client builds
# with GOEXPERIMENT=jsonv2. Verified -12% time / -23% allocs on the
# edge POST roundtrip vs encoding/json v1. Applies to luxd + every
# in-stage VM plugin build below.
ARG GO_EXPERIMENT=jsonv2
ENV GOEXPERIMENT=${GO_EXPERIMENT}

# Configure a cross-compiler if the target platform differs from the build platform.
#
# build_env.sh is used to capture the environmental changes required by the build step since RUN
# environment state is not otherwise persistent.
RUN if [ "$TARGETPLATFORM" = "linux/arm64" ] && [ "$BUILDPLATFORM" != "linux/arm64" ]; then \
    echo "export CC=aarch64-linux-gnu-gcc" > ./build_env.sh \
    ; elif [ "$TARGETPLATFORM" = "linux/amd64" ] && [ "$BUILDPLATFORM" != "linux/amd64" ]; then \
    echo "export CC=x86_64-linux-gnu-gcc" > ./build_env.sh \
    ; else \
    echo "export CC=gcc" > ./build_env.sh \
    ; fi

# Fetch pre-built lux-accel (GPU crypto library). The release assets live in
# the PRIVATE luxcpp/accel repo (resolves to lux-private/accel) — an
# unauthenticated GitHub release-download 404s, so the fetch is authenticated
# with the same `ghtok` BuildKit secret used for private go modules. It is
# also best-effort (matches the cevm/lpm fetch contract below): the library is
# ONLY linked at CGO_ENABLED=1, so a CGO_ENABLED=0 build (the canonical CI/devnet
# build, pure-Go) does not need it and must not fail when it is unreachable.
ARG ACCEL_VERSION=v0.1.0
RUN --mount=type=secret,id=ghtok,required=false \
    ARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
    if [ "$ARCH" = "amd64" ]; then ACCEL_ARCH="linux-x86_64"; else ACCEL_ARCH="linux-arm64"; fi && \
    mkdir -p /usr/local/include /usr/local/lib && \
    AUTH=""; [ -s /run/secrets/ghtok ] && AUTH="--header=Authorization: Bearer $(cat /run/secrets/ghtok)"; \
    ( wget -q ${AUTH:+"$AUTH"} \
        "https://github.com/luxcpp/accel/releases/download/${ACCEL_VERSION}/lux-accel-${ACCEL_ARCH}.tar.gz" \
        -O /tmp/accel.tar.gz \
      && tar -xzf /tmp/accel.tar.gz -C /usr/local \
      && rm /tmp/accel.tar.gz \
      && ldconfig 2>/dev/null \
    ) || echo "WARN: lux-accel ${ACCEL_VERSION} fetch skipped (private/unreachable; GPU accel unused at CGO_ENABLED=0)"

# Fetch pre-built luxcpp/cevm libs (libevm, libevm-gpu, libluxgpu,
# libcevm_precompiles + go_bridge.h). When CGO_ENABLED=1 these libraries are
# linked into the C-Chain plugin via github.com/luxfi/chains/evm/cevm
# and become the default execution backend (parallel + GPU EVM).
#
# The release assets live in the PRIVATE lux-private/cevm repo, so the fetch is
# authenticated with the same `ghtok` secret as the private go modules and
# lux-accel above. Best-effort, like lux-accel: only a build that asks for the
# cevm backend (EVM_CGO=1 below) actually needs these.
ARG CGO_ENABLED=1
ARG CEVM_VERSION=v0.51.10
ARG CEVM_REPO=lux-private/cevm
RUN --mount=type=secret,id=ghtok,required=false \
    if [ "${CGO_ENABLED}" = "1" ]; then \
        ARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
        if [ "$ARCH" = "amd64" ]; then CEVM_ARCH="linux-x86_64"; else CEVM_ARCH="linux-arm64"; fi && \
        AUTH=""; [ -s /run/secrets/ghtok ] && AUTH="--header=Authorization: Bearer $(cat /run/secrets/ghtok)"; \
        ( wget -q ${AUTH:+"$AUTH"} \
            "https://github.com/${CEVM_REPO}/releases/download/${CEVM_VERSION}/luxcpp-cevm-${CEVM_ARCH}.tar.gz" \
            -O /tmp/cevm.tar.gz \
          && tar -xzf /tmp/cevm.tar.gz -C /usr/local \
          && rm /tmp/cevm.tar.gz \
          && ldconfig 2>/dev/null \
        ) || echo "WARN: cevm ${CEVM_VERSION} fetch skipped (unreachable; needed only when EVM_CGO=1)" ; \
    else \
        echo "CGO_ENABLED=0: skipping cevm fetch (pure-Go fallback build)" ; \
    fi

# Build node. CGO_ENABLED=1 (default) links luxcpp/cevm for parallel + GPU EVM.
# Set CGO_ENABLED=0 for portable pure-Go builds without the native libs.
ARG RACE_FLAG=""
ARG BUILD_SCRIPT=build.sh
ARG LUXD_COMMIT=""
ENV CGO_ENABLED=${CGO_ENABLED}
RUN . ./build_env.sh && \
    # `COPY . .` above restored the committed go.sum (stale first-party hashes when
    # a luxfi/hanzoai module was re-tagged). Re-strip first-party lines so -mod=mod
    # re-records the CURRENT content hashes already in the module cache (from the
    # `go mod download` step). Without this, a re-tag => go.sum SECURITY ERROR.
    sed -i -E '/^github.com\/(luxfi|hanzoai)\//d' go.sum && \
    echo "{CC=$CC, TARGETPLATFORM=$TARGETPLATFORM, BUILDPLATFORM=$BUILDPLATFORM, CGO_ENABLED=${CGO_ENABLED}}" && \
    export GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
    export LUXD_COMMIT="${LUXD_COMMIT}" && \
    GOFLAGS="-mod=mod" ./scripts/${BUILD_SCRIPT} ${RACE_FLAG}

# ============= EVM Plugin Stage ================
# Build EVM plugin from source (includes custom precompile registry).
# EVM_VERSION must pin a luxfi/evm release whose go.mod points at a
# luxfi/node version that has the runtime.EngineAddressKey rename
# (LUX_VM_RUNTIME_ENGINE_ADDR → VM_RUNTIME_ENGINE_ADDR landed in
# 4ae211d46d on 2026-05-15). v0.18.14 pins node v1.27.6 which is
# post-rename. Older evm tags (e.g. v0.8.40 → node v1.23.4) ship a
# plugin that os.Getenv()'s the old key, mismatching the host that
# sets the new key, and C-chain never bootstraps.
#
# v0.19.0 (Quasar Edition): EtnaTimestamp Go field + json tag renamed
# to QuasarTimestamp/quasarTimestamp. Strict upgradeBytes decode via
# json.NewDecoder(...).DisallowUnknownFields() — stale etnaTimestamp
# in any deployed upgrade.json now fails parse loudly at boot rather
# than silently disabling the fork.
#
# v0.19.1 bumps luxfi/precompile v0.5.27 → v0.5.35 (commit 794912f):
# each of the 7 EIP-2537 bls12381 sub-configs (G1/G2 × {Add,Mul,MSM} +
# Pairing) now returns its own ConfigKey, fixing a Key() collision
# that forced #114 to drop bls12381 entries from mainnet upgrade.json.
# Re-adding them is gated on this plugin version.
#
# v0.19.2 bumps luxfi/vm v1.1.5 → v1.1.6, which drops the obsolete
# GetNetworkUpgrades() method on the VMContext interface. Required by
# the Etna→Quasar rename in v0.19.0: vm v1.1.5 still expected
# upgrade.Config to implement the old IsEtnaActivated() runtime
# interface, which no longer exists.
#
# v0.19.3 closes the cascade: bumps vm v1.1.6 → v1.1.7 + runtime
# v1.0.1 → v1.1.0. runtime v1.1.0 ripped the always-true
# NetworkUpgrades interface (decomplect 9e6e597) and renamed
# XAssetID → UTXOAssetID (refactor 034ec47). v1.1.6 anticipated the
# rip in rpc/context.go but still pinned the old runtime, leaving
# itself internally inconsistent. v1.1.7 pins the post-rip runtime.
# MUST track node's go.mod luxfi/evm (the C-Chain EVM plugin = the native 0x9999
# receipt/atomic surface). v1.99.31 = the native-atomic seam (precompile v0.5.51,
# geth v1.17.12 CallIndex). Bump this with every evm release or the bundled
# C-Chain plugin silently goes stale vs node's deps.
#
# v1.99.33 (precompile v0.5.53): 0x9999 DEX settlement activates at a SINGLE canonical
# dated fork — extras.DexSettleActivationTime = 1766704800 (Dec 25 2025 00:00:00 UTC) —
# with ZERO per-net config (no dexSettleConfig genesis/upgrade entry; one built-in fork,
# identical on every net). At the fork it BOTH enables dispatch AND installs the standard
# EXTCODESIZE marker (nonce=1 + code) into 0x9999 going forward, so eth_getCode(0x9999)
# !=0x and a typed Solidity IPoolManager(0x9999).swap(...) passes the contract-existence
# guard. The marker is installed FORWARD (never in historical genesis), so pre-Dec-25
# history (the ~/work/lux/state RLP snapshot, replayed via admin.importChain) stays
# byte-identical to canonical state — a pre-fork value transfer to 0x9999 hits a PLAIN
# account, not the precompile. The D-Chain (dexvm) peer is resolved at RUNTIME via the
# consensus-context "D" alias (contract.AtomicState.DChainID()) and the
# protocolFeeController is the built-in DAO treasury. 0x9010 is REMOVED (not a registered
# precompile, no dispatch, no forward); 0x9999 is the SOLE canonical DEX precompile. For a
# fresh net whose genesis ts >= the fork, the marker is present from block 0.
#
# v1.99.34 (precompile v0.5.54): fixes a consensus-divergence on the relaunch/replay path.
# The EVM dispatch gate (core.LuxPrecompileOverrider.PrecompileOverride) used to read the
# process-global params.lastRulesContext (via GetRulesExtra(Rules{})), which is rewritten
# last-writer-wins by every concurrent Rules() call (eth_call/estimateGas/worker). On a
# live, RPC-serving post-fork node replaying a PRE-fork RLP block (admin.importChain), a
# concurrent post-fork eth_call could overwrite the global timestamp between the verify
# goroutine's NewEVM(pre-fork block) and its tx dispatch — making the pre-fork block see
# 0x9999 ENABLED and dispatch SettleContract.Run during plain-account execution => state
# divergence / consensus split. Fix: the dispatch gate now decides from the overrider's OWN
# per-EVM fields (o.chainConfig + o.timestamp) via the pure params.GetExtrasRules, never the
# global — so every replay of a block yields the same enabled set on every validator. Also:
# the registry stateDBBridge.SubBalance fallback now FAILS CLOSED (panic → reverted call)
# instead of returning a zero "previous balance" without debiting (silent native mint); and
# the genesis-config builders (SetAllGenesisPrecompiles/AllGenesisPrecompiles) skip AlwaysOn
# modules so 0x9999 can never get a timestamp-0 genesis config that bypasses the dated fork.
# The money path (V4 swap ABI, marker install, two-phase atomic settle) is byte-for-byte
# unchanged: ONLY the dispatch-path timestamp source, the SubBalance fail-mode, the genesis
# builder guard, and stale 0x9010 comments changed.
#
# v1.99.37 (precompile v0.5.57): wires the 0x9999 ERC-20 Call surface to the DEX
# settlement precompile (commit 2cf30e43d) and gates that Call surface to the DEX
# settlement family 0x9999/0x9996 (commit 9579f2e34). Before this, a CALL into
# 0x9999's ERC-20 settle path saw a nil PrecompileEnv (GetPrecompileEnv == nil) and
# could not resolve the token-transfer Call seam — the two-phase atomic settle's
# ERC-20 leg had no env to execute against. precompile v0.5.57 also adds the
# CALL-only DELEGATECALL guard (commit feeaab5a0) so the settle surface is reachable
# only via CALL (not DELEGATECALL, which would run it in the caller's context). Also
# converges deps to latest patch within v1.x.x (threshold v1.9.9, crypto v1.19.21,
# database v1.20.3, geth v1.17.12, warp v1.19.5, vm v1.2.5, api v1.0.15 — the
# UTXOAssetID rename that fixed the LuxAssetID build break) and removes the dead
# vendored dexConfig upgrade fixtures. The 0x9999 swap ABI + dated-fork activation
# (DexSettleActivationTime = 1766704800) are unchanged from v1.99.34.
#
# v1.99.40 (precompile v0.5.59, chains v1.3.19, consensus v1.25.21): the permissionless
# 0x9999 DEX value path lands end-to-end. precompile v0.5.58/59 = AssetResolver (real
# on-chain canonical resolution, NO admin allowlist), one synchronous router/book/journal,
# minOut on every route, no keeper/venue/live-ZAP/second-book; the env→Call ERC-20 vault
# seam + in-state-vault resolution make a real ERC-20 settle. evm v1.99.39 = the
# reprocess-bind fix: NewBlockChain binds the chain Runtime (networkID/C-Chain id) BEFORE
# startup reprocess, so an unclean restart after a 0x9999 swap re-executes the committed
# swap with the correct identity instead of (0, Empty) — previously that reverted the swap,
# failed ValidateState, and BRICKED the node. Proven on-node: real swap → kill -9 →
# clean reboot, state intact. v1.99.40 = v1.99.39 + deps to latest. consensus v1.25.21 =
# stake-weighted alpha-of-K quorum finality + per-height single-finalize + epoch-bound certs.
# v1.104.9 bumps luxfi/api v1.0.15 -> v1.0.16 (indirect via luxfi/vm). v1.0.16
# APPENDED InitializeResponse.Capabilities (uint64) for the Quasar-export
# handshake (api commit 1f2dc5a). The node pins api v1.0.16 and DECODES that
# field; an EVM plugin built at v1.104.8 (api v1.0.15) omits it, so the node's
# strict struct decode hits "zap decode initialize response: unexpected EOF"
# and EVERY EVM chain (C + L2s hanzo/zoo/pars/spc, all mgj786) fails to
# initialize. The api bump is code-free for plugins (chains v1.7.4->v1.7.5
# adopted it with a go.mod-only diff), so v1.104.9 = v1.104.8 + api alignment.
# C-Chain execution backend. The default is the pure-Go EVM, which is what
# every image has shipped so far. EVM_CGO=1 EVM_TAGS=cevm links luxcpp/cevm
# instead — that pair is what makes AutoEVM resolve to CppEVM. The libraries
# come from the cevm fetch in this same stage above.
ARG EVM_CGO=0
ARG EVM_TAGS=""

# v1.104.22 realigns the plugin with THIS node's own go.mod: api v1.0.16 ->
# v1.1.1, vm v1.2.6 -> v1.3.1, geth v1.17.12 -> v1.20.1. Node main pins api
# v1.1.1, so pinning an evm at api v1.0.16 reintroduces the exact
# InitializeResponse decode mismatch described above, just in the opposite
# direction — keep this ARG and node's go.mod on the same api/vm/geth line.
#
# v1.104.14 is also the FIRST evm tag carrying the C-Chain fee-split seam
# (core/fee_split.go creditTxFee + extras.FeeSplitTimestamp/FeeRewardVault).
# Below it, encoding/json silently DISCARDS the genesis "feeSplitTimestamp"
# key: a chain configured for the 50/50 split instead routes 100% of every fee
# to the block coinbase and the reward vault 0x0100..0002 stays 0 forever, with
# nothing burned. The split stays dormant wherever feeSplitTimestamp is absent
# (mainnet), so this bump is behaviour-preserving there.
ARG EVM_VERSION=v1.104.47
ARG EVM_VM_ID=mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6
# the pinned evm go.mod may pin a dead luxfi/upgrade pseudo-version
# (v1.0.1-0.20260603055252-f51810805436 — commit pruned from origin). Heal it to
# the released upgrade v1.0.1 tag (semver-forward, not a downgrade). Then strip
# first-party go.sum lines so -mod=mod re-records current content hashes for the
# re-published luxfi modules at their pinned versions (integrity repair, no version drift).
RUN --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /luxd/build/plugins && \
    git clone --depth 1 --branch ${EVM_VERSION} https://github.com/luxfi/evm.git /tmp/evm && \
    cd /tmp/evm && \
    . /build/build_env.sh && \
    go mod edit -require=github.com/luxfi/upgrade@v1.0.1 && \
    # evm v1.99.52 = v1.99.51 + the idle-builder bounded-wake backstop
    # (startPendingTxPoll: a 500ms mempool re-poll so a tx after idle wakes the
    # builder within a bounded window — closes the lost-wakeup/subscribe-gap;
    # deterministic, wake-timing only). v1.99.51 = the block-production stall fix
    # (WaitForEvent builder-ready race + target-rate build pacing — un-freezes the
    # C-Chain that stalled at the imported frontier) on top of precompile v0.16.0
    # enable-everything
    # (wallet curves + standard precompiles enabled; fflonk fail-closed; accel
    # byte-identity; DEX big.Rat + determinism). Pin chains v1.4.8 (warp
    # consolidated to one luxfi/warp helper; graphvm genesis-last-accepted fix)
    # to match the chain-VM plugin stage (CHAINS_REF) below.
    go mod edit -require=github.com/luxfi/chains@v1.7.0 && \
    find /tmp/evm -name go.sum -exec sed -i -E '/^github.com\/(luxfi|hanzoai)\//d' {} + && \
    GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) \
    CGO_ENABLED=${EVM_CGO} GOFLAGS=-mod=mod \
    go build -ldflags="-s -w" -tags "${EVM_TAGS}" -o /luxd/build/plugins/${EVM_VM_ID} ./plugin && \
    # Ship the fail-closed offline C-Chain repair tool from the exact same EVM
    # source as the plugin. It is inert during normal node startup; operators can
    # run it from an init container while luxd is stopped to reconcile a legacy
    # pre-certification accepted tip without introducing a second image or an
    # unpinned recovery binary.
    GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) \
    CGO_ENABLED=0 GOFLAGS=-mod=mod \
    go build -ldflags="-s -w" -o /luxd/build/repair-cchain ./cmd/repair-cchain && \
    chmod +x /luxd/build/plugins/${EVM_VM_ID} && \
    chmod +x /luxd/build/repair-cchain && \
    # SEAM PIN ASSERTION — the C-Chain half. See the D-Chain twin at the dex build.
    #
    # The node serves each chain's shared memory to its plugin, so SharedMemory here is
    # the ZAP client, and atomiczap.Apply REFUSES a database batch outright
    # (ErrBatchUnsupported). A plugin pinned below the batch-less Accept therefore turns
    # every block that stages an atomic op into a FATAL Accept — and Accept failure is
    # fail-closed in the engine ("refusing to advance finality past the VM's applied
    # state"), so the first 0x9999 settlement would STOP THE CHAIN. That is exactly what
    # v1.36.60 shipped: shared memory served, plugins pinned pre-fix.
    #
    # This is a BUILD CHECK, not a runtime gate. It cannot skip, defer or disable
    # anything; it only refuses to produce an image whose pins contradict the transport.
    # Positive signal AND negative signal, so neither a rename nor a deletion passes
    # silently.
    grep -qE 'atomicSM\.Apply\(atomicReqs\)' plugin/evm/block.go \
        || { echo "FATAL: evm ${EVM_VERSION} plugin/evm/block.go has no batch-less 'atomicSM.Apply(atomicReqs)'. A pin below the fix halts the chain on the first atomic op."; exit 1; } && \
    ! grep -nE '\.Apply\([^)]+,' plugin/evm/block.go \
        || { echo "FATAL: evm ${EVM_VERSION} passes a second argument to Apply — atomiczap refuses a batch across the process boundary (ErrBatchUnsupported)."; exit 1; } && \
    rm -rf /tmp/evm

# ============= Chain VM Plugin Stage ================
# Build the 10 chain VM plugins that github.com/luxfi/chains implements.
#
# The D-Chain is NOT among them. Its slot is filled by luxfi/dex cmd/dexd in the
# next stage, so dexvm is absent from both the table and the build list below. A
# slot holds one binary: adding a D build here would not add a fallback, it would
# race the matcher and win or lose by ordering.
#
# VM ID table (CB58-encoded ids.ID byte arrays from each factory.go):
#   aivm         -> juFxSrbCM4wszxddKepj1GWwmrn9YgN1g4n3VUWPpRo9JjERA
#   bridgevm     -> kMhHABHM8j4bH94MCc4rsTNdo5E9En37MMyiujk4WdNxgXFsY
#   graphvm      -> nZQm4Dmg1rjX18rb8maL9gamYyXPf1xCvF7ymWzxp6a1nSQTt
#   identityvm   -> oR6tnZHezwogyf9fRnomNXC9ojwCEBAU6jdUzpgy2PB1tD7fM
#   keyvm        -> pJJCSV7hHYVY6TUZwR8qUPAfuhX8JLb2C1AzNSezrYNbgau8M
#   oraclevm     -> r5m1ujrmXxVcQetG3CQfuDLHp2RHKh6vCDaFgBRQfUcTZh7eS
#   quantumvm    -> ry9Sg8rZdT26iEKvJDmC2wkESs4SDKgZEhk5BgLSwg1EpcNug
#   relayvm      -> sP6dLqrrBR9w3soP18fbJ3YzZecZdD7DDdfH2cFhhLq7Hy9bz
#   mpcvm        -> qCURact1n41FcoNBch8iMVBwc9AWie48D118ZNJ5tBdWrvryS
#   zkvm         -> vv3qPfyTVXZ5ArRZA9Jh4hbYDTBe43f7sgQg4CHfNg1rnnvX9

# This ref MUST equal node's go.mod luxfi/chains pin. go.mod is the authority;
# naming the release's contents here is what let the two drift apart unnoticed.
#
# The plugins are baked into the image and nothing else delivers them, so a ref
# behind go.mod ships VMs the host node was not built against and no operator
# action can correct it short of a new image. It reached eleven tags behind: the
# mpcvm plugin was built from v1.7.6, which predates M-Chain's state and custody
# implementations entirely and still declared the retired `thresholdvm` VMID.
# That last part stayed harmless only because the node resolves a plugin by its
# FILENAME (vms/registry/registry.go), never by the ID the binary declares.
# READ from go.mod rather than restate it. The comment above already said go.mod
# was the authority and that naming the ref here is what let them drift — and it
# drifted anyway, to eleven tags at the worst and to five security commits at the
# time this was replaced. A rule written in a comment beside the value it governs
# is not a rule; deriving the value is.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CHAINS_REF="$(awk '$1=="github.com/luxfi/chains"{print $2; exit}' /build/go.mod)" && \
    if [ -z "${CHAINS_REF}" ]; then echo "go.mod names no luxfi/chains version" >&2; exit 1; fi && \
    echo "building chain VM plugins from go.mod pin ${CHAINS_REF}" && \
    git clone --depth 1 --branch ${CHAINS_REF} https://github.com/luxfi/chains.git /tmp/chains && \
    find /tmp/chains -name go.sum -exec sed -i -E '/^github.com\/(luxfi|hanzoai)\//d' {} +

# F-Chain ships as the SAME mpcvm binary under a second filename. mpcvm backs
# both surfaces -- M-Chain's MPC signing and F-Chain's FHE compute (mpcvm
# service.go: "the one VM backs all three surfaces"; LP-8200: "F-Chain runs on
# the shared mpcvm substrate"). The node resolves a plugin by FILENAME, never by
# the id the binary declares, so installing it under M's id alone is what made
# F-Chain report "VM plugin not loaded" and opt out on every node of every
# network. It is copied rather than built twice so the two names stay
# byte-identical: one implementation, two ids, not two builds that can drift.
#
# Each VM is an independent Go module under /tmp/chains/<vm>/.
# Build each plugin binary and place it at /luxd/build/plugins/<cb58-vm-id>.
#
# The recursive go.sum strip above lets -mod=mod re-record current content hashes
# for re-published first-party modules at their pinned versions (integrity repair).
# At a tagged CHAINS_REF (v1.3.11) the sibling go.mod replace directives that
# affect `main` are absent, so every VM module builds in isolation. The bridgevm
# plugin (B-Chain) MUST build — it blank-imports crypto/threshold/bls to register
# the BLS threshold scheme; a miss reintroduces the v1.30.16 B-Chain init failure.
RUN --mount=type=cache,target=/root/.cache/go-build \
    . /build/build_env.sh && \
    export GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
    mkdir -p /luxd/build/plugins && \
    ( cd /tmp/chains/aivm && CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
        -o /luxd/build/plugins/juFxSrbCM4wszxddKepj1GWwmrn9YgN1g4n3VUWPpRo9JjERA ./cmd/plugin ) || echo "WARN: aivm plugin build skipped" ; \
    ( cd /tmp/chains/bridgevm && CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
        -o /luxd/build/plugins/kMhHABHM8j4bH94MCc4rsTNdo5E9En37MMyiujk4WdNxgXFsY ./cmd/plugin ) ; \
    ( cd /tmp/chains/graphvm && CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
        -o /luxd/build/plugins/nZQm4Dmg1rjX18rb8maL9gamYyXPf1xCvF7ymWzxp6a1nSQTt ./cmd/plugin ) || echo "WARN: graphvm plugin build skipped" ; \
    ( cd /tmp/chains/identityvm && CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
        -o /luxd/build/plugins/oR6tnZHezwogyf9fRnomNXC9ojwCEBAU6jdUzpgy2PB1tD7fM ./cmd/plugin ) || echo "WARN: identityvm plugin build skipped" ; \
    ( cd /tmp/chains/keyvm && CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
        -o /luxd/build/plugins/pJJCSV7hHYVY6TUZwR8qUPAfuhX8JLb2C1AzNSezrYNbgau8M ./cmd/plugin ) || echo "WARN: keyvm plugin build skipped" ; \
    ( cd /tmp/chains/oraclevm && CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
        -o /luxd/build/plugins/r5m1ujrmXxVcQetG3CQfuDLHp2RHKh6vCDaFgBRQfUcTZh7eS ./cmd/plugin ) || echo "WARN: oraclevm plugin build skipped" ; \
    ( cd /tmp/chains/quantumvm && CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
        -o /luxd/build/plugins/ry9Sg8rZdT26iEKvJDmC2wkESs4SDKgZEhk5BgLSwg1EpcNug ./cmd/plugin ) || echo "WARN: quantumvm plugin build skipped" ; \
    ( cd /tmp/chains/relayvm && CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
        -o /luxd/build/plugins/sP6dLqrrBR9w3soP18fbJ3YzZecZdD7DDdfH2cFhhLq7Hy9bz ./cmd/plugin ) || echo "WARN: relayvm plugin build skipped" ; \
    ( cd /tmp/chains/mpcvm && CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
        -o /luxd/build/plugins/qCURact1n41FcoNBch8iMVBwc9AWie48D118ZNJ5tBdWrvryS ./cmd/plugin ) || echo "WARN: mpcvm plugin build skipped" ; \
    ( cp /luxd/build/plugins/qCURact1n41FcoNBch8iMVBwc9AWie48D118ZNJ5tBdWrvryS \
         /luxd/build/plugins/n6sSsSfbpQBrU9sY4R29U6z8VrmnTo2CntW6da4rRS7qmnGdv ) || echo "WARN: fhevm install skipped" ; \
    ( cd /tmp/chains/zkvm && CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
        -o /luxd/build/plugins/vv3qPfyTVXZ5ArRZA9Jh4hbYDTBe43f7sgQg4CHfNg1rnnvX9 ./cmd/plugin ) || echo "WARN: zkvm plugin build skipped" ; \
    ( chmod +x /luxd/build/plugins/* 2>/dev/null || true ) && \
    for p in \
        juFxSrbCM4wszxddKepj1GWwmrn9YgN1g4n3VUWPpRo9JjERA \
        kMhHABHM8j4bH94MCc4rsTNdo5E9En37MMyiujk4WdNxgXFsY \
        nZQm4Dmg1rjX18rb8maL9gamYyXPf1xCvF7ymWzxp6a1nSQTt \
        oR6tnZHezwogyf9fRnomNXC9ojwCEBAU6jdUzpgy2PB1tD7fM \
        pJJCSV7hHYVY6TUZwR8qUPAfuhX8JLb2C1AzNSezrYNbgau8M \
        r5m1ujrmXxVcQetG3CQfuDLHp2RHKh6vCDaFgBRQfUcTZh7eS \
        ry9Sg8rZdT26iEKvJDmC2wkESs4SDKgZEhk5BgLSwg1EpcNug \
        sP6dLqrrBR9w3soP18fbJ3YzZecZdD7DDdfH2cFhhLq7Hy9bz \
        qCURact1n41FcoNBch8iMVBwc9AWie48D118ZNJ5tBdWrvryS \
        n6sSsSfbpQBrU9sY4R29U6z8VrmnTo2CntW6da4rRS7qmnGdv \
        vv3qPfyTVXZ5ArRZA9Jh4hbYDTBe43f7sgQg4CHfNg1rnnvX9 ; do \
        test -s /luxd/build/plugins/$p \
            || { echo "FATAL: required chain-VM plugin $p missing/empty — its build failed above (see the matching WARN line); the runtime-stage hard COPY would otherwise fail cryptically. Surface & fix the real Go build error, or remove the plugin from BOTH the build list and the runtime COPY."; exit 1; } ; \
    done && \
    rm -rf /tmp/chains

# ============= Native D-Chain DEX VM Plugin Stage ================
# The D-Chain (dexvm slot, vmID mDVT5EWMumBp3LCqvKwuyZQeY1VXr1jvjGNAt8nL4UFiXvqXr)
# is the NATIVE consensus matcher VM: github.com/luxfi/dex/pkg/dchain implements
# block.ChainVM and runs the lx.OrderBook matcher INSIDE luxd consensus
# (Block.Verify against a versiondb overlay; Block.Accept commits) — the trade IS
# the D-Chain state transition, sequenced by luxd's multi-validator engine. This
# REPLACES the chains/dexvm proxy (which relayed clob_* over ZAP to a standalone
# dchain-venue): there is no DexZapEndpoint and no standalone venue in the trading
# path. That proxy is deleted — it kept building for this slot after losing it, and
# its clob_* wire had stopped matching the venue's dex_* names, so installing it
# produced a node that loaded, passed health, and answered nothing.
#
# cmd/dexd (renamed from cmd/dchain in the v1.14 line — it is the ONE D-Chain
# binary, plugin mode by default) wraps the VM in the SAME rpc.Serve plugin
# harness luxfi/evm boots through, and is pure-Go (CGO=0) — the optional GPU AMM
# accelerator in pkg/lx is a separate concern gated by its own cuda/metal tags and
# is NOT linked here. v1.5.10 is the first tag whose cmd/dchain builds CGO=0
# (drops the phantom dchain+cgo gate); v1.5.11 wires CLOB order ingestion over the
# node HTTP router (VM.CreateHandlers -> /v1/bc/D/dex/<method>, pkg/dchain/ingest.go)
# so an order POSTed to the node flows submitTx -> mempool -> consensus -> Verify
# (match) -> Accept; v1.5.12 persists the head block so the VM survives a restart
# once advanced past genesis (GetBlock(lastAccepted) no longer ErrNotFound);
# v1.5.13 indexes processing blocks so the plugin transport's ID-only Accept
# (GetBlock(builtID)) resolves the just-built block — without it the engine's
# self-finalize Accept is a silent no-op and the clob submitTx waiter hangs (no
# D-Chain blocks). v1.5.14 consumes luxfi/database v1.20.4, which fixes prefixdb
# returning ZERO rows for a nil-start prefix scan over a prefixdb-wrapped chain
# DB (every plugin VM via vm/rpc) — that stranded the native D-Chain order book
# (rebuildBookFromDB folded empty -> 0 fills despite committed asks). v1.5.15 adds
# the committed-state READ surface (clob_get_trades/orders/markets/book over
# /v1/bc/D/dex/<method>, pkg/dchain/read.go): read-only JSON of the durable trade
# log / resting book / markets, served beside the writes with ZERO consensus
# impact. Needed to VERIFY a fill replicated identically across validators (query
# every node, diff the trade rows + head root) and to feed markets-display (native
# fills are trade: rows). Bump with every dex release that changes the VM, like
# CHAINS_REF for the other 10 VMs.
#
# v1.5.15 -> v1.14.4 IS A LINEAGE CHANGE, NOT A PATCH. v1.5.15 is not an ancestor
# of main (278 commits diverged) and its pkg/dchain has NO atomic.go and NO
# drive.go: that D-Chain cannot import a C->D intent or export a D->C fill at
# all. Every fleet running it has a structurally absent settlement seam, so
# fixing the ZAP atomic transport and the intent discovery trait changes nothing
# while this pin stands. The v1.14 line is where the seam lives, and v1.14.4
# pins luxfi/vm v1.3.6 so the plugin actually receives shared memory.
#
# Treat this as a D-Chain VM upgrade and roll it like one: it changes block
# production (the autonomous seam drive emits import/export txs inside
# BuildBlock), so a mixed fleet across this line does NOT agree on block
# contents. One validator at a time, D-Chain only, and never below quorum.
#
# v1.14.4 -> v1.14.6 IS A WIRE FLAG DAY, and two separate things ride on it.
#
# (1) THE SEAM ONLY NOW PLACES AN ORDER. Through v1.14.5 the C->D object carried
# VALUE only, so the D-Chain consumed an intent, credited the taker, and stopped.
# Nothing crossed, nothing was exported, and 0x9999 emitted zero events on every
# chain since genesis. v1.14.6 carries the taker's operation (market, side,
# limit, size) in the object and builds their order from it. It REQUIRES
# precompile >= v0.19.7 on the C side (hence EVM_VERSION v1.104.26): the two
# repos pin the same 118-byte golden vector and the same .v2 discovery trait.
#
# (2) THE TxImport BODY WIDENED, 32 -> 150 bytes, because the object's own bytes
# now ride in the transaction (execution that reads shared memory cannot be
# replayed: the consuming Remove lands at Accept, so the next node to sync finds
# the object gone and dies on `execution root mismatch`). An OLD node's
# bodySize(TxImport) is 32, so a new seam block fails ParseBlock outright — it is
# rejected as UNPARSEABLE, never executed differently. A mixed fleet across this
# line STALLS on seam blocks; it cannot fork. No historical block on any network
# contains a TxImport, so there is no retroactive effect and no re-sync risk
# below the line.
#
# Roll every D validator to this image BEFORE the first v2 intent is minted on C.
# Until then the drive finds nothing (it enumerates the .v2 trait, which no
# pre-v0.19.7 C node writes) and emits no seam tx at all.
#
# THE DRIVE IS STATUS-DRIVEN as of v1.14.10, and that is a correctness fix, not a
# refactor: through v1.14.9 the drive emitted the import and the taker's order as a PAIR
# in one block, so a proposer that included the import and omitted the order produced a
# valid block whose escrow was stuck open forever — the taker's value stranded in a
# keyless D account while C's deadline reclaim refunded them. Any proposer could do it to
# any swap, for free. Do not pin a DEX_REF below v1.14.10 with the seam enabled.
#
# THE DEPLOYED FLEET IS NOT ON THE PIN ABOVE IT. Probing devnet's running plugin
# (grep the vendored module strings out of /data/plugins/mDVT5...) shows luxfi/dex
# v1.5.15, not v1.14.x — the image predates this ARG. That matters twice:
#
#   - v1.5.15 HAS the HTTP surface, under the OLD method names: /v1/bc/D/dex/clob_*
#     answers 200 today (clob_get_markets returns height 0, markets []). A probe of
#     /v1/bc/D/dex/dex_* returning 404 means "old names", NOT "no surface". The
#     rename landed in dex 28970d8 (clob_* -> dex_*).
#   - v1.5.15 has NO atomic.go and NO drive.go, so that D-Chain cannot import a C->D
#     intent or export a D->C fill AT ALL. Every fleet on it has a structurally
#     absent seam, which is the real reason nothing has ever traded — not the wire,
#     not the trait, not funding.
#
# So this bump is a D-Chain LINEAGE change on the running fleet (v1.5.15 -> v1.14.x),
# not a patch. Devnet's D-Chain is at height 0 with 0 markets on all 5 validators, so
# there is no accumulated state for it to disagree with.
# v1.14.31, not v1.14.25. Two things landed in between that a fleet must not run
# without:
#
#   1. executeImport guarded only the C chain id while executeExport also guarded
#      shared memory. The halves disagreed, and Block.verifyImports returned nil
#      when shared memory was nil on the reasoning that "executeImport rejects
#      without a C chain id" — it rejects without a chain ID, not without shared
#      memory. Both ends guard both conditions now.
#
#   2. A duplicate delivery of an already-consumed claim read as unbacked, so the
#      block was rejected, Reject requeued every tx, and the next build failed
#      identically — a permanent halt, measured at four rounds stuck on one
#      height. Delivery is permissionless by design, so duplicates are the steady
#      state rather than an attack. verifyImports rejects the block for safety;
#      BuildBlock holds the delivery and retries for liveness.
#
# The genesis construction is byte-identical across that range — pkg/dchain
# genesis.go and state.go do not differ between v1.14.25 and v1.14.31, and all
# twelve genesis vectors including TestGenesisLiveFleets pass at v1.14.31. So the
# founding values published for the re-foundation are unchanged by this bump.
ARG DEX_REF=v1.14.31
RUN --mount=type=cache,target=/root/.cache/go-build \
    git clone --depth 1 --branch ${DEX_REF} https://github.com/luxfi/dex.git /tmp/dex && \
    find /tmp/dex -name go.sum -exec sed -i -E '/^github.com\/(luxfi|hanzoai)\//d' {} + && \
    cd /tmp/dex && \
    # dex v1.14.4 pins luxfi/api v1.1.3 and luxfi/vm v1.3.6 itself, so the ZAP
    # wire aligns with the node without an override here. The former forced
    # api@v1.0.16 is GONE: forcing an api version below what luxfi/vm requires
    # would strip the cross-chain atomic messages back out of the dexvm plugin
    # and re-dark the seam this bump exists to open.
    . /build/build_env.sh && \
    GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) \
    CGO_ENABLED=0 GOFLAGS=-mod=mod \
    go build -ldflags="-s -w" \
        -o /luxd/build/plugins/mDVT5EWMumBp3LCqvKwuyZQeY1VXr1jvjGNAt8nL4UFiXvqXr ./cmd/dexd && \
    chmod +x /luxd/build/plugins/mDVT5EWMumBp3LCqvKwuyZQeY1VXr1jvjGNAt8nL4UFiXvqXr && \
    test -s /luxd/build/plugins/mDVT5EWMumBp3LCqvKwuyZQeY1VXr1jvjGNAt8nL4UFiXvqXr \
        || { echo "FATAL: native D-Chain (dexvm) plugin missing — D-Chain cannot start"; exit 1; } && \
    # SEAM PIN ASSERTION — the D-Chain half. Twin of the C-Chain check at the evm build;
    # the same ErrBatchUnsupported refusal halts D on its first seam settle.
    grep -qE 'sm\.Apply\(ar\.reqs\)' pkg/dchain/atomic.go \
        || { echo "FATAL: dex ${DEX_REF} pkg/dchain/atomic.go has no batch-less 'sm.Apply(ar.reqs)'. A pin below v1.14.5 halts D-Chain on the first seam settle."; exit 1; } && \
    ! grep -nE '\.Apply\([^)]+,' pkg/dchain/atomic.go \
        || { echo "FATAL: dex ${DEX_REF} passes a second argument to Apply — atomiczap refuses a batch across the process boundary (ErrBatchUnsupported)."; exit 1; } && \
    # The D-side reproducibility defect: executeImport must take the object as a
    # PARAMETER (it travels in the tx body), never read it from shared memory during
    # execute(). execute() is compared against the block's execRoot, and sm.Get reads
    # live cross-chain memory outside that root — a node re-executing an accepted block
    # finds the object already consumed, derives a different root, and can never sync
    # past the first swap the seam settles. Fixed in v1.14.10; pinned here so it stays fixed.
    grep -qE 'func \(vm \*VM\) executeImport\(.*object \[\]byte' pkg/dchain/atomic.go \
        || { echo "FATAL: dex ${DEX_REF} executeImport does not carry the object as a parameter — a re-executing node will fail 'execution root mismatch' and can never bootstrap past a settle."; exit 1; } && \
    # WIRE NAMESPACE ASSERTION. A VM is reachable only through the method names it
    # answers, and a plugin that answers names nobody dials still loads and still
    # passes health — every order is a silent no-op on a chain that reports up. The
    # venue registers dex_*; clob_* was the wire of a proxy (formerly chains/dexvm)
    # that spoke to nothing and has been deleted. Checked here because this is where
    # the source exists; no runtime probe distinguishes the two.
    grep -rq '"dex_place"' pkg/ \
        || { echo "FATAL: dex ${DEX_REF} does not register the dex_* wire namespace — a D-Chain plugin built from it would load into the slot and answer nothing."; exit 1; } && \
    ! grep -rq '"clob_' pkg/ \
        || { echo "FATAL: dex ${DEX_REF} carries the dead clob_* wire namespace. Nothing answers those names on either side."; exit 1; } && \
    # GENESIS ASSERTION. The D-Chain's height-0 block must come from the document
    # recorded in its P-Chain CreateChainTx, never from this binary. A pin that
    # derives genesis from compiled-in defaults gives every wiped node a chain of
    # its own, silently and while reporting healthy — and wiping chainData is the
    # ordinary repair, so the ordinary repair is what strands the validator. Both
    # halves are required: read the delivered document, and refuse to found from
    # one that never arrived. Asserted here because this line selects the source
    # and no runtime probe distinguishes a right genesis from a lucky one.
    grep -q 'refusing to found a chain with no creation document' pkg/dchain/genesis.go \
        || { echo "FATAL: dex ${DEX_REF} founds a chain from an empty creation document — a wiped node would adopt this binary's genesis and partition alone. Pin v1.14.25 or later."; exit 1; } && \
    rm -rf /tmp/dex

# lpm (Lux Plugin Manager) -- optional, skip if build fails
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/root/go/pkg/mod \
    GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
    git clone --depth 1 https://github.com/luxfi/lpm.git /tmp/lpm && \
    cd /tmp/lpm && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /luxd/build/lpm ./main && \
    rm -rf /tmp/lpm || echo "WARN: lpm build skipped (non-critical)"

# Create this directory in the builder to avoid requiring anything to be executed in the
# potentially emulated execution container.
RUN mkdir -p /luxd/build

# ============= Runtime Stage ================
# Commands executed in this stage may be emulated (i.e. very slow) if TARGETPLATFORM and
# BUILDPLATFORM have different arches.
FROM debian:12-slim AS execution

# Install runtime dependencies (curl for RPC, git for lpm source installs, ca-certificates for TLS)
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl ca-certificates git \
    && rm -rf /var/lib/apt/lists/*

# GPU crypto library (optional -- only present when built with CGO_ENABLED=1 + luxcpp).
# Pure Go fallbacks are used when the library is absent.
RUN ldconfig 2>/dev/null || true

# Maintain compatibility with previous images.
# In the builder stage /luxd/build contains ONLY plugins/ (the luxd + lpm binaries
# live at /build/build and are COPYed separately below). The plugin set (~192MB of
# 12 VM plugins) is split across several COPY layers so each blob stays well under
# ~100MB: a single monolithic plugins layer cannot reliably complete its registry
# blob upload over a contended uplink, whereas sub-100MB layers push reliably (and
# resume independently by digest).
# plugin group 1
COPY --from=builder \
    /luxd/build/plugins/mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6 \
    /luxd/build/plugins/juFxSrbCM4wszxddKepj1GWwmrn9YgN1g4n3VUWPpRo9JjERA \
    /luxd/build/plugins/kMhHABHM8j4bH94MCc4rsTNdo5E9En37MMyiujk4WdNxgXFsY \
    /luxd/build/plugins/
# plugin group 2
COPY --from=builder \
    /luxd/build/plugins/mDVT5EWMumBp3LCqvKwuyZQeY1VXr1jvjGNAt8nL4UFiXvqXr \
    /luxd/build/plugins/nZQm4Dmg1rjX18rb8maL9gamYyXPf1xCvF7ymWzxp6a1nSQTt \
    /luxd/build/plugins/oR6tnZHezwogyf9fRnomNXC9ojwCEBAU6jdUzpgy2PB1tD7fM \
    /luxd/build/plugins/pJJCSV7hHYVY6TUZwR8qUPAfuhX8JLb2C1AzNSezrYNbgau8M \
    /luxd/build/plugins/
# plugin group 3
COPY --from=builder \
    /luxd/build/plugins/r5m1ujrmXxVcQetG3CQfuDLHp2RHKh6vCDaFgBRQfUcTZh7eS \
    /luxd/build/plugins/ry9Sg8rZdT26iEKvJDmC2wkESs4SDKgZEhk5BgLSwg1EpcNug \
    /luxd/build/plugins/sP6dLqrrBR9w3soP18fbJ3YzZecZdD7DDdfH2cFhhLq7Hy9bz \
    /luxd/build/plugins/qCURact1n41FcoNBch8iMVBwc9AWie48D118ZNJ5tBdWrvryS \
    /luxd/build/plugins/vv3qPfyTVXZ5ArRZA9Jh4hbYDTBe43f7sgQg4CHfNg1rnnvX9 \
    /luxd/build/plugins/
# plugin group 4 — F-Chain. Same bytes as mpcvm in group 3, under F's id,
# because one binary serves both surfaces and the registry resolves by filename.
# It needs its own COPY: the builder stage writes it, but only files named here
# reach the final image, which is why building it was not enough to ship it.
COPY --from=builder \
    /luxd/build/plugins/n6sSsSfbpQBrU9sY4R29U6z8VrmnTo2CntW6da4rRS7qmnGdv \
    /luxd/build/plugins/
WORKDIR /luxd/build

# Copy the executables into the container
COPY --from=builder /build/build/ .
COPY --from=builder /luxd/build/repair-cchain ./repair-cchain

# Create plugins directory and lpm state directory
RUN mkdir -p /luxd/build/plugins /root/.lpm /root/.lux/plugins

# Add lpm to PATH
ENV PATH="/luxd/build:${PATH}"

EXPOSE 9630 9631

CMD [ "./luxd" ]
