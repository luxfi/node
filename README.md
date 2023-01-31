<div align="center">
  <img src="resources/luxlogo.png?raw=true">
</div>

---

Node implementation for the [Lux](https://lux.network) network -
a decentralized network of blockchains designed for real world assets (RWAs).

## Installation

Lux is an incredibly lightweight protocol, so the minimum computer requirements are quite modest.
Note that as network usage increases, hardware requirements may change.

The minimum recommended hardware specification for nodes connected to Mainnet is:

- CPU: Equivalent of 8 AWS vCPU
- RAM: 16 GiB
- Storage: 1 TiB
- OS: Ubuntu 20.04/22.04 or macOS >= 12
- Network: Reliable IPv4 or IPv6 network connection, with an open public port.

If you plan to build Lux Node from source, you will also need the following software:

- [Go](https://golang.org/doc/install) version >= 1.18.1
- [gcc](https://gcc.gnu.org/)
- g++

### Building From Source

#### Clone The Repository

Clone the Lux Node repository:

```sh
git clone git@github.com:luxdefi/node.git
cd node
```

This will clone and checkout the `master` branch.

#### Building Lux Node

Build Lux Node by running the build script:

```sh
./scripts/build.sh
```

The `lux` binary is now in the `build` directory. To run:

```sh
./build/lux
```

### Binary Repository

Install Lux Node using an `apt` repository.

#### Adding the APT Repository

If you have already added the APT repository, you do not need to add it again.

To add the repository on Ubuntu, run:

```sh
sudo su -
wget -qO - https://downloads.avax.network/luxd.gpg.key | tee /etc/apt/trusted.gpg.d/luxd.asc
source /etc/os-release && echo "deb https://downloads.avax.network/apt $UBUNTU_CODENAME main" > /etc/apt/sources.list.d/lux.list
exit
```

#### Installing the Latest Version

After adding the APT repository, install lux by running:

```sh
sudo apt update
sudo apt install lux
```

### Binary Install

Download the [latest build](https://github.com/luxdefi/node/releases/latest) for your operating system and architecture.

The Lux binary to be executed is named `lux`.

### Docker Install

Make sure docker is installed on the machine - so commands like `docker run` etc. are available.

Building the docker image of latest luxd branch can be done by running:

```sh
./scripts/build_image.sh
```

To check the built image, run:

```sh
docker image ls
```

The image should be tagged as `luxdefi/node:xxxxxxxx`, where `xxxxxxxx` is the shortened commit of the Lux source it was built from. To run the lux node, run:

```sh
docker run -ti -p 9650:9650 -p 9651:9651 luxdefi/node:xxxxxxxx /node/build/lux
```

## Running Lux

### Connecting to Lux Mainnet

To connect to the Lux Mainnet, run:

```sh
./build/lux
```

You should see some pretty ASCII art and log messages.

You can use `Ctrl+C` to kill the node.

### Connecting to Lux Testnet

To connect to the Lux Testnet, run:

```sh
./build/lux --network-id=testnet
```

### Creating a Local Testnet

See [this tutorial.](https://docs.avax.network/build/tutorials/platform/create-a-local-test-network/)

## Bootstrapping

A node needs to catch up to the latest network state before it can participate in consensus and serve API calls. This process, called bootstrapping, currently takes several days for a new node connected to Mainnet.

A node will not [report healthy](https://docs.avax.network/build/node-apis/health) until it is done bootstrapping.

Improvements that reduce the amount of time it takes to bootstrap are under development.

The bottleneck during bootstrapping is typically database IO. Using a more powerful CPU or increasing the database IOPS on the computer running a node will decrease the amount of time bootstrapping takes.

## Generating Code

Luxgo uses multiple tools to generate efficient and boilerplate code.

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

Because Lux Node's version denotes the network version, it is expected that interfaces exported by Lux Node's packages may change in `Patch` version updates.

### API Compatibility Guarantees

APIs exposed when running Lux Node will maintain backwards compatibility, unless the functionality is explicitly deprecated and announced when removed.

## Supported Platforms

Lux Node can run on different platforms, with different support tiers:

- **Tier 1**: Fully supported by the maintainers, guaranteed to pass all tests including e2e and stress tests.
- **Tier 2**: Passes all unit and integration tests but not necessarily e2e tests.
- **Tier 3**: Builds but lightly tested (or not), considered _experimental_.
- **Not supported**: May not build and not tested, considered _unsafe_. To be supported in the future.

The following table lists currently supported platforms and their corresponding
Lux Node support tiers:

| Architecture | Operating system | Support tier  |
| :----------: | :--------------: | :-----------: |
|    arm64     |      Darwin      |       1       |
|    arm64     |      Linux       |       1       |
|    amd64     |      Darwin      |       2       |
|    amd64     |      Linux       |       2       |
|     arm      |      Linux       | Not supported |
|     i386     |      Linux       | Not supported |
|      any     |     Windows      | Not supported |

To officially support a new platform, one must satisfy the following requirements:

| Lux Node continuous integration | Tier 1  | Tier 2  | Tier 3  |
| ---------------------------------- | :-----: | :-----: | :-----: |
| Build passes                       | &check; | &check; | &check; |
| Unit and integration tests pass    | &check; | &check; |         |
| End-to-end and stress tests pass   | &check; |         |         |

## Security Bugs

**We and our community welcome responsible disclosures.**

If you've discovered a security vulnerability, please report it via our [bug bounty program](mailto:security@lux.network/). Valid reports will be eligible for a reward (terms and conditions apply).
