# The version is supplied as a build argument rather than hard-coded
# to minimize the cost of version changes.
ARG GO_VERSION=1.26.1

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

# Install build dependencies (ca-certificates needed for go mod download)
# libc6-dev-arm64-cross needed for cross-compiling to ARM64
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc libc6-dev make git ca-certificates wget \
    gcc-aarch64-linux-gnu gcc-x86-64-linux-gnu \
    libc6-dev-arm64-cross libc6-dev-amd64-cross \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Skip checksum verification for luxfi packages (tags may be rewritten)
ENV GONOSUMCHECK=github.com/luxfi/*
ENV GONOSUMDB=github.com/luxfi/*
# Use Go proxy for most deps (gonum.org is flaky via direct), direct only for luxfi
ENV GOPROXY=https://proxy.golang.org,direct
ENV GONOPROXY=github.com/luxfi/*
ENV GOFLAGS="-mod=mod"

# Copy and download lux dependencies using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy the code into the container
COPY . .

# Ensure pre-existing builds are not available for inclusion in the final image
RUN [ -d ./build ] && rm -rf ./build/* || true

ARG TARGETPLATFORM
ARG BUILDPLATFORM

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

# Fetch pre-built lux-accel (GPU crypto library)
ARG ACCEL_VERSION=v0.1.0
RUN ARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
    if [ "$ARCH" = "amd64" ]; then ACCEL_ARCH="linux-x86_64"; else ACCEL_ARCH="linux-arm64"; fi && \
    mkdir -p /usr/local/include /usr/local/lib && \
    wget -q "https://github.com/luxcpp/accel/releases/download/${ACCEL_VERSION}/lux-accel-${ACCEL_ARCH}.tar.gz" \
        -O /tmp/accel.tar.gz && \
    tar -xzf /tmp/accel.tar.gz -C /usr/local && \
    rm /tmp/accel.tar.gz && \
    ldconfig 2>/dev/null || true

# Build node. CGO_ENABLED=0 for portable builds (GPU accel uses pure Go fallbacks).
# Set CGO_ENABLED=1 + install libluxaccel from luxcpp for GPU acceleration.
ARG RACE_FLAG=""
ARG BUILD_SCRIPT=build.sh
ARG LUXD_COMMIT=""
ENV CGO_ENABLED=0
RUN . ./build_env.sh && \
    echo "{CC=$CC, TARGETPLATFORM=$TARGETPLATFORM, BUILDPLATFORM=$BUILDPLATFORM}" && \
    export GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
    export LUXD_COMMIT="${LUXD_COMMIT}" && \
    GOFLAGS="-mod=mod" ./scripts/${BUILD_SCRIPT} ${RACE_FLAG}

# ============= EVM Plugin Stage ================
# Build EVM plugin from source (includes custom precompile registry)
ARG EVM_VERSION=v0.8.40
ARG EVM_VM_ID=mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6
RUN --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /luxd/build/plugins && \
    git clone --depth 1 --branch ${EVM_VERSION} https://github.com/luxfi/evm.git /tmp/evm && \
    cd /tmp/evm && \
    . /build/build_env.sh && \
    GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) \
    CGO_ENABLED=0 GOFLAGS=-mod=mod \
    go build -ldflags="-s -w" -o /luxd/build/plugins/${EVM_VM_ID} ./plugin && \
    chmod +x /luxd/build/plugins/${EVM_VM_ID} && \
    rm -rf /tmp/evm

# ============= Chain VM Plugin Stage ================
# Build all 11 chain VM plugins from github.com/luxfi/chains
#
# VM ID table (CB58-encoded ids.ID byte arrays from each factory.go):
#   aivm         -> juFxSrbCM4wszxddKepj1GWwmrn9YgN1g4n3VUWPpRo9JjERA
#   bridgevm     -> kMhHABHM8j4bH94MCc4rsTNdo5E9En37MMyiujk4WdNxgXFsY
#   dexvm        -> mDVT5EWMumBp3LCqvKwuyZQeY1VXr1jvjGNAt8nL4UFiXvqXr
#   graphvm      -> nZQm4Dmg1rjX18rb8maL9gamYyXPf1xCvF7ymWzxp6a1nSQTt
#   identityvm   -> oR6tnZHezwogyf9fRnomNXC9ojwCEBAU6jdUzpgy2PB1tD7fM
#   keyvm        -> pJJCSV7hHYVY6TUZwR8qUPAfuhX8JLb2C1AzNSezrYNbgau8M
#   oraclevm     -> r5m1ujrmXxVcQetG3CQfuDLHp2RHKh6vCDaFgBRQfUcTZh7eS
#   quantumvm    -> ry9Sg8rZdT26iEKvJDmC2wkESs4SDKgZEhk5BgLSwg1EpcNug
#   relayvm      -> sP6dLqrrBR9w3soP18fbJ3YzZecZdD7DDdfH2cFhhLq7Hy9bz
#   thresholdvm  -> tGVBwRxpmD2aFdg3iYjgRvrCe8Jcmq9UNKxyHMus2NZ8WcD8t
#   zkvm         -> vv3qPfyTVXZ5ArRZA9Jh4hbYDTBe43f7sgQg4CHfNg1rnnvX9

ARG CHAINS_REF=main
RUN --mount=type=cache,target=/root/.cache/go-build \
    git clone --depth 1 --branch ${CHAINS_REF} https://github.com/luxfi/chains.git /tmp/chains

# Each VM is an independent Go module under /tmp/chains/<vm>/.
# Build each plugin binary and place it at /luxd/build/plugins/<cb58-vm-id>.
RUN --mount=type=cache,target=/root/.cache/go-build \
    . /build/build_env.sh && \
    export GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
    set -e && \
    cd /tmp/chains/aivm && \
    CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
      -o /luxd/build/plugins/juFxSrbCM4wszxddKepj1GWwmrn9YgN1g4n3VUWPpRo9JjERA \
      ./cmd/plugin && \
    cd /tmp/chains/bridgevm && \
    CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
      -o /luxd/build/plugins/kMhHABHM8j4bH94MCc4rsTNdo5E9En37MMyiujk4WdNxgXFsY \
      ./cmd/plugin && \
    cd /tmp/chains/dexvm && \
    CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
      -o /luxd/build/plugins/mDVT5EWMumBp3LCqvKwuyZQeY1VXr1jvjGNAt8nL4UFiXvqXr \
      ./cmd/plugin && \
    cd /tmp/chains/graphvm && \
    CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
      -o /luxd/build/plugins/nZQm4Dmg1rjX18rb8maL9gamYyXPf1xCvF7ymWzxp6a1nSQTt \
      ./cmd/plugin && \
    cd /tmp/chains/identityvm && \
    CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
      -o /luxd/build/plugins/oR6tnZHezwogyf9fRnomNXC9ojwCEBAU6jdUzpgy2PB1tD7fM \
      ./cmd/plugin && \
    cd /tmp/chains/keyvm && \
    CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
      -o /luxd/build/plugins/pJJCSV7hHYVY6TUZwR8qUPAfuhX8JLb2C1AzNSezrYNbgau8M \
      ./cmd/plugin && \
    cd /tmp/chains/oraclevm && \
    CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
      -o /luxd/build/plugins/r5m1ujrmXxVcQetG3CQfuDLHp2RHKh6vCDaFgBRQfUcTZh7eS \
      ./cmd/plugin && \
    cd /tmp/chains/quantumvm && \
    CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
      -o /luxd/build/plugins/ry9Sg8rZdT26iEKvJDmC2wkESs4SDKgZEhk5BgLSwg1EpcNug \
      ./cmd/plugin && \
    cd /tmp/chains/relayvm && \
    CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
      -o /luxd/build/plugins/sP6dLqrrBR9w3soP18fbJ3YzZecZdD7DDdfH2cFhhLq7Hy9bz \
      ./cmd/plugin && \
    cd /tmp/chains/thresholdvm && \
    CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
      -o /luxd/build/plugins/tGVBwRxpmD2aFdg3iYjgRvrCe8Jcmq9UNKxyHMus2NZ8WcD8t \
      ./cmd/plugin && \
    cd /tmp/chains/zkvm && \
    CGO_ENABLED=0 GOFLAGS=-mod=mod go build -ldflags="-s -w" \
      -o /luxd/build/plugins/vv3qPfyTVXZ5ArRZA9Jh4hbYDTBe43f7sgQg4CHfNg1rnnvX9 \
      ./cmd/plugin && \
    chmod +x /luxd/build/plugins/* && \
    rm -rf /tmp/chains

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

# Maintain compatibility with previous images
COPY --from=builder /luxd/build /luxd/build
WORKDIR /luxd/build

# Copy the executables into the container
COPY --from=builder /build/build/ .

# Create plugins directory and lpm state directory
RUN mkdir -p /luxd/build/plugins /root/.lpm /root/.lux/plugins

# Add lpm to PATH
ENV PATH="/luxd/build:${PATH}"

EXPOSE 9630 9631

CMD [ "./luxd" ]
