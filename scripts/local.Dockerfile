# syntax=docker/dockerfile:experimental

# This Dockerfile is meant to be used with the build_local_dep_image.sh script
# in order to build an image using the local version of coreth

# Changes to the minimum golang version must also be replicated in
# scripts/build_avalanche.sh
# scripts/local.Dockerfile (here)
# Dockerfile
# README.md
# go.mod
FROM golang:1.18.5-buster

RUN mkdir -p /go/src/github.com/ava-labs

WORKDIR $GOPATH/src/github.com/ava-labs
COPY avalanchego avalanchego

WORKDIR $GOPATH/src/github.com/ava-labs/avalanchego
RUN ./scripts/build_avalanche.sh
<<<<<<< HEAD
<<<<<<< HEAD
=======
RUN ./scripts/build_coreth.sh -c ../coreth -e $PWD/build/plugins/evm
>>>>>>> 0d8e8458d (Add race detection to the e2e tests (#2299))
=======
>>>>>>> 374536bc0 (Replace `--build-dir` with `--plugin-dir` (#1741))

RUN ln -sv $GOPATH/src/github.com/ava-labs/avalanche-byzantine/ /avalanchego
