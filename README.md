# Avalanche gRPC

Now Serving: **Protocol Version 22**

<<<<<<< HEAD
Protobuf files are hosted at [https://buf.build/ava-labs/avalanche](https://buf.build/ava-labs/avalanche) and can be used as dependencies in other projects.
=======
Node implementation for [Lux Network](https://lux.network) - a decentralized
network of blockchains designed for real world asset (RWA)s.
>>>>>>> 1d9864b10 (Update README)

Protobuf linting and generation for this project is managed by [buf](https://github.com/bufbuild/buf).

<<<<<<< HEAD
Please find installation instructions on [https://docs.buf.build/installation/](https://docs.buf.build/installation/) or use `Dockerfile.buf` provided in the `proto/` directory of AvalancheGo.
=======
Lux is an incredibly lightweight protocol, so the minimum computer requirements are quite modest.
Note that as network usage increases, hardware requirements may change.
>>>>>>> 1d9864b10 (Update README)

Any changes made to proto definition can be updated by running `protobuf_codegen.sh` located in the `scripts/` directory of AvalancheGo.

Introduction to `buf` [https://docs.buf.build/tour/introduction](https://docs.buf.build/tour/introduction)

<<<<<<< HEAD
## Protocol Version Compatibility
=======
If you plan to build Lux from source, you will also need the following software:
>>>>>>> 1d9864b10 (Update README)

<<<<<<< HEAD
The protobuf definitions and generated code are versioned based on the [RPCChainVMProtocol](../version/version.go#L13) defined for the RPCChainVM.
Many versions of an Avalanche client can use the same [RPCChainVMProtocol](../version/version.go#L13). But each Avalanche client and subnet vm must use the same protocol version to be compatible.
=======
- [Go](https://golang.org/doc/install) version >= 1.18.1
- [gcc](https://gcc.gnu.org/)
- g++

### Building From Source

#### Clone The Repository

Clone the Node repository:

```sh
git clone git@github.com:luxdefi/node.git
cd node
```

This will clone and checkout the `main` branch.

#### Building Lux Node

Build from source by running the following build script:

```sh
./scripts/build.sh
```

The `luxd` binary is now in the `build` directory. To run:

```sh
./build/luxd
```

### Docker Install

Make sure docker is installed on the machine - so commands like `docker run` etc. are available.

Building the docker image of latest avalanchego branch can be done by running:

```sh
./scripts/build_image.sh
```

To check the built image, run:

```sh
docker image ls
```

The image should be tagged as `luxdefi/node:xxxxxxxx`, where `xxxxxxxx` is the shortened commit of the source it was built from. To run the Lux node, run:

```sh
docker run -ti -p 9650:9650 -p 9651:9651 luxdefi/lux:xxxxxxxx /node/build/luxd
```

## Running Lux

### Connecting to Mainnet

To connect to the Lux Mainnet, run:

```sh
./build/luxd
```

You should see some pretty ASCII art and log messages.

You can use `Ctrl+C` to kill the node.

### Connecting to Testnet

To connect to Testnet, run:

```sh
./build/luxd --network-id=testnet
```

### Creating a Local Testnet

See [this tutorial.](https://docs.lux.network/build/tutorials/platform/create-a-local-test-network/)

## Bootstrapping

A node needs to catch up to the latest network state before it can participate in consensus and serve API calls. This process, called bootstrapping, currently takes several days for a new node connected to Mainnet.

A node will not [report healthy](https://docs.lux.network/build/node-apis/health) until it is done bootstrapping.

Improvements that reduce the amount of time it takes to bootstrap are under development.

The bottleneck during bootstrapping is typically database IO. Using a more powerful CPU or increasing the database IOPS on the computer running a node will decrease the amount of time bootstrapping takes.

## Generating Code

Avalanchego uses multiple tools to generate efficient and boilerplate code.

### Running protobuf codegen

To regenerate the protobuf go code, run `scripts/protobuf_codegen.sh` from the root of the repo.

This should only be necessary when upgrading protobuf versions or modifying .proto definition files.

To use this script, you must have [buf](https://docs.buf.build/installation) (v1.10.0), protoc-gen-go (v1.28.0) and protoc-gen-go-grpc (v1.2.0) installed.

To install the buf dependencies:

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.0
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2.0
```

If you have not already, you may need to add `$GOPATH/bin` to your `$PATH`:

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

If you extract buf to ~/software/buf/bin, the following should work:

```sh
export PATH=$PATH:~/software/buf/bin/:~/go/bin
go get google.golang.org/protobuf/cmd/protoc-gen-go
go get google.golang.org/protobuf/cmd/protoc-gen-go-grpc
scripts/protobuf_codegen.sh
```

For more information, refer to the [GRPC Golang Quick Start Guide](https://grpc.io/docs/languages/go/quickstart/).

### Running protobuf codegen from docker

```sh
docker build -t lux:protobuf_codegen -f api/Dockerfile.buf .
docker run -t -i -v $(pwd):/opt/lux -w/opt/lux lux:protobuf_codegen bash -c "scripts/protobuf_codegen.sh"
```

### Running mock codegen

To regenerate the [gomock](https://github.com/golang/mock) code, run `scripts/mock.gen.sh` from the root of the repo.

This should only be necessary when modifying exported interfaces or after modifying `scripts/mock.mockgen.txt`.

## Versioning

### Version Semantics

Lux Node is first and foremost a client for the Lux network. The versioning of Lux Node follows that of the Lux network.

- `v0.x.x` indicates a development network version.
- `v1.x.x` indicates a production network version.
- `vx.[Upgrade].x` indicates the number of network upgrades that have occurred.
- `vx.x.[Patch]` indicates the number of client upgrades that have occurred since the last network upgrade.

### Library Compatibility Guarantees

Because Lux Node's version denotes the network version, it is expected that interfaces exported may change in `Patch` version updates.

### API Compatibility Guarantees

APIs exposed when running Lux Node will maintain backwards compatibility, unless the functionality is explicitly deprecated and announced when removed.

## Supported Platforms

Lux Node can run on different platforms, with different support tiers:

- **Tier 1**: Fully supported by the maintainers, guaranteed to pass all tests including e2e and stress tests.
- **Tier 2**: Passes all unit and integration tests but not necessarily e2e tests.
- **Tier 3**: Builds but lightly tested (or not), considered _experimental_.
- **Not supported**: May not build and not tested, considered _unsafe_. To be supported in the future.

The following table lists currently supported platforms and their corresponding support tiers:

| Architecture | Operating system | Support tier  |
| :----------: | :--------------: | :-----------: |
|    amd64     |      Darwin      |       1       |
|    amd64     |      Linux       |       1       |
|    arm64     |      Darwin      |       1       |
|    arm64     |      Linux       |       1       |
|    amd64     |     Windows      | Not supported |
|     arm      |      Linux       | Not supported |
|     i386     |      Linux       | Not supported |

To officially support a new platform, one must satisfy the following requirements:

| Node continuous integration        | Tier 1  | Tier 2  | Tier 3  |
| ---------------------------------- | :-----: | :-----: | :-----: |
| Build passes                       | &check; | &check; | &check; |
| Unit and integration tests pass    | &check; | &check; |         |
| End-to-end and stress tests pass   | &check; |         |         |

## Security Bugs

**We and our community welcome responsible disclosures.**

<<<<<<< HEAD
If you've discovered a security vulnerability, please report it via our [bug bounty program](https://hackenproof.com/avalanche/). Valid reports will be eligible for a reward (terms and conditions apply).
>>>>>>> 51f21a85b (Update buf to v1.9.0 (#2239))
=======
If you've discovered a security vulnerability, please report it via our [bug bounty program](mailto:security@lux.network). Valid reports will be eligible for a reward (terms and conditions apply).
>>>>>>> 1d9864b10 (Update README)
