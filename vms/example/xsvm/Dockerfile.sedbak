# The version is supplied as a build argument rather than hard-coded
# to minimize the cost of version changes.
ARG GO_VERSION

# LUXD_NODE_IMAGE needs to identify an existing node image and should include the tag
ARG LUXD_NODE_IMAGE

# ============= Compilation Stage ================
FROM golang:$GO_VERSION-bullseye AS builder

WORKDIR /build

# Copy and download lux dependencies using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Copy the code into the container
COPY . .

# Build xsvm
RUN ./scripts/build_xsvm.sh

# ============= Cleanup Stage ================
FROM $LUXD_NODE_IMAGE AS execution

# Copy the xsvm binary to the default plugin path
RUN mkdir -p /root/.node/plugins
COPY --from=builder /build/build/xsvm /root/.node/plugins/v3m4wPxaHpvGr8qfMeyK6PRW3idZrPHmYcMTt7oXdK47yurVH

# The node image's entrypoint will be reused.
