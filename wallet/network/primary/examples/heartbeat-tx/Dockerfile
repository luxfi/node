# chain-heartbeat: tiny CronJob image that submits one self-tx per
# configured EVM chain so Etna's "empty-block-production-off" mode still
# advances eth_blockNumber. Built from luxfi/node so the binary picks up
# the same luxfi/geth + luxfi/crypto + luxfi/go-bip{32,39} versions as
# the rest of the stack.

ARG GO_VERSION=1.26.3

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build

# luxfi tags get rewritten; bypass the proxy sum DB for those modules.
ENV GONOSUMCHECK=github.com/luxfi/*
ENV GOSUMDB=off

WORKDIR /src

# Copy the whole node module so the example's dependency graph resolves
# against the same go.mod/go.sum that the rest of luxd uses.
COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" \
      -o /out/heartbeat-tx \
      ./wallet/network/primary/examples/heartbeat-tx

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/heartbeat-tx /usr/local/bin/heartbeat-tx
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/heartbeat-tx"]
