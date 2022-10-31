# luxd

## Installation

Lux Network is powered by an incredibly lightweight protocol, so the minimum computer requirements are quite modest.
Note that as network usage increases, hardware requirements may change.

The minimum recommended hardware specification for nodes connected to Mainnet is:

- CPU: Equivalent of 8 AWS vCPU
- RAM: 16 GiB
- Storage: 1 TiB
- OS: Ubuntu 20.04/22.04 or macOS >= 12
- Network: Reliable IPv4 or IPv6 network connection, with an open public port.

If you plan to build luxd from source, you will also need the following software:

- [Go](https://golang.org/doc/install) version >= 1.18.1
- [gcc](https://gcc.gnu.org/)
- g++

### Native Install

Clone the `luxd` repository:

```sh
git clone https://github.com/luxdefi/luxd.git
cd luxd
```

This will clone and checkout to `master` branch.

#### Building the Lux Executable

Build Lux by running the build script:

```sh
./scripts/build.sh
```

The output of the script will be the Lux binary named `luxd`. It is located in the build directory:

```sh
./build/luxd
```

### Binary Repository

Install luxd using an `apt` repository.

#### Adding the APT Repository

If you have already added the APT repository, you do not need to add it again.

To add the repository on Ubuntu 20.04 (Focal), run:

```sh
sudo su -
wget -O - https://downloads.lux.network/lux.gpg.key | apt-key add -
echo "deb https://downloads.lux.network/apt focal main" > /etc/apt/sources.list.d/lux.list
exit
```

To add the repository on Ubuntu 22.04 (Jammy), run:

```sh
sudo su -
wget -O - https://downloads.lux.network/lux.gpg.key | apt-key add -
echo "deb https://downloads.lux.network/apt jammy main" > /etc/apt/sources.list.d/lux.list
exit
```

#### Installing the Latest Version

After adding the APT repository, install lux by running:

```sh
sudo apt update
sudo apt install lux
```

### Binary Install

Download the [latest build](https://github.com/luxdefi/luxd/releases/latest) for your operating system and architecture.

The Lux binary to be executed is named `lux`.

### Docker Install

Make sure docker is installed on the machine - so commands like `docker run` etc. are available.

Building the docker image of latest lux branch can be done by running:

```sh
./scripts/build_image.sh
```

To check the built image, run:

```sh
docker image ls
```

The image should be tagged as `luxdefi/luxd:xxxxxxxx`, where `xxxxxxxx` is the shortened commit of the Lux source it was built from. To run the Lux node, run:

```sh
docker run -ti -p 9650:9650 -p 9651:9651 luxdefi/luxd:xxxxxxxx /lux/build/lux
```

## Running Lux

### Connecting to Mainnet

To connect to the Lux Mainnet, run:

```sh
./build/lux
```

You should see some pretty ASCII art and log messages.

You can use `Ctrl+C` to kill the node.

### Connecting to Testnet

To connect to the Lux Testnet, run:

```sh
./build/lux --network-id=testnet
```

### Creating a Local Testnet

See [this tutorial.](https://docs.lux.network)

## Bootstrapping

A node needs to catch up to the latest network state before it can participate in consensus and serve API calls. This process, called bootstrapping, currently takes several days for a new node connected to Mainnet.

A node will not [report healthy](https://docs.lux.network) until it is done bootstrapping.

Improvements that reduce the amount of time it takes to bootstrap are under development.

The bottleneck during bootstrapping is typically database IO. Using a more powerful CPU or increasing the database IOPS on the computer running a node will decrease the amount of time bootstrapping takes.

## Generating Code

Lux uses multiple tools to generate efficient and boilerplate code.

### Running protobuf codegen

To regenerate the protobuf go code, run `scripts/protobuf_codegen.sh` from the root of the repo.

This should only be necessary when upgrading protobuf versions or modifying .proto definition files.

To use this script, you must have [buf](https://docs.buf.build/installation) (v1.7.0), protoc-gen-go (v1.28.0) and protoc-gen-go-grpc (v1.2.0) installed.

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

## Supported Platforms

luxd can run on different platforms, with different support tiers:

- **Tier 1**: Fully supported by the maintainers, guaranteed to pass all tests including e2e and stress tests.
- **Tier 2**: Passes all unit and integration tests but not necessarily e2e tests.
- **Tier 3**: Builds but lightly tested (or not), considered _experimental_.
- **Not supported**: May not build and not tested, considered _unsafe_. To be supported in the future.

The following table lists currently supported platforms and their corresponding
luxd support tiers:

| Architecture | Operating system | Support tier  |
| :----------: | :--------------: | :-----------: |
|    amd64     |      Linux       |       1       |
|    arm64     |      Linux       |       2       |
|    amd64     |      Darwin      |       2       |
|    amd64     |     Windows      |       3       |
|     arm      |      Linux       | Not supported |
|     i386     |      Linux       | Not supported |
|    arm64     |      Darwin      | Not supported |

To officially support a new platform, one must satisfy the following requirements:

| luxd continuous integration | Tier 1  | Tier 2  | Tier 3  |
| ---------------------------------- | :-----: | :-----: | :-----: |
| Build passes                       | &check; | &check; | &check; |
| Unit and integration tests pass    | &check; | &check; |         |
| End-to-end and stress tests pass   | &check; |         |         |

## Security Bugs

**We and our community welcome responsible disclosures.**

If you've discovered a security vulnerability, please report it via our [bug bounty program](mailto:bugs@lux.partners). Valid reports will be eligible for a reward (terms and conditions apply).
