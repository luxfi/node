# The version is supplied as a build argument rather than hard-coded
# to minimize the cost of version changes.
ARG GO_VERSION=1.21.12

# ============= Compilation Stage ================
# Always use the native platform to ensure fast builds
FROM --platform=$BUILDPLATFORM golang:$GO_VERSION-bookworm AS builder

WORKDIR /build

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
    apt-get update && apt-get install -y gcc-aarch64-linux-gnu && \
    echo "export CC=aarch64-linux-gnu-gcc" > ./build_env.sh \
    ; elif [ "$TARGETPLATFORM" = "linux/amd64" ] && [ "$BUILDPLATFORM" != "linux/amd64" ]; then \
    apt-get update && apt-get install -y gcc-x86-64-linux-gnu && \
    echo "export CC=x86_64-linux-gnu-gcc" > ./build_env.sh \
    ; else \
    echo "export CC=gcc" > ./build_env.sh \
    ; fi

# Build luxd. The build environment is configured with build_env.sh from the step
# enabling cross-compilation.
ARG RACE_FLAG=""
ARG BUILD_SCRIPT=build.sh
ARG LUXD_COMMIT=""
RUN . ./build_env.sh && \
    echo "{CC=$CC, TARGETPLATFORM=$TARGETPLATFORM, BUILDPLATFORM=$BUILDPLATFORM}" && \
    export GOARCH=$(echo ${TARGETPLATFORM} | cut -d / -f2) && \
    export LUXD_COMMIT="${LUXD_COMMIT}" && \
    ./scripts/${BUILD_SCRIPT} ${RACE_FLAG}

# Create this directory in the builder to avoid requiring anything to be executed in the
# potentially emulated execution container.
RUN mkdir -p /luxd/build

# ============= Cleanup Stage ================
# Commands executed in this stage may be emulated (i.e. very slow) if TARGETPLATFORM and
# BUILDPLATFORM have different arches.
FROM debian:12-slim AS execution

# Maintain compatibility with previous images
COPY --from=builder /luxd/build /luxd/build
WORKDIR /luxd/build

# Copy the executables into the container
COPY --from=builder /build/build/ .

CMD [ "./luxd" ]
