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

RUN mkdir -p /go/src/github.com/luxdefi

WORKDIR $GOPATH/src/github.com/luxdefi
COPY luxd luxd
COPY coreth coreth

WORKDIR $GOPATH/src/github.com/luxdefi/luxd
RUN ./scripts/build_avalanche.sh
RUN ./scripts/build_coreth.sh ../coreth $PWD/build/plugins/evm

RUN ln -sv $GOPATH/src/github.com/luxdefi/avalanche-byzantine/ /luxd
