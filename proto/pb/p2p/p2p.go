//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package p2p is the protobuf-generated wire-types package referenced
// by the grpc-tagged transport in proto/p2p/p2p_grpc.go (or its
// legacy single-file form). Default builds use ZAP and never reach
// this package; the build tag here matches the grpc-tagged consumers
// so default builds skip it cleanly.
//
// To re-generate the actual protobuf types (when the grpc transport
// is needed), run:
//
//   protoc -I=proto/ --go_out=proto/pb/ proto/p2p/p2p.proto
//
// in the node root. Until that's done, this stub keeps "go mod tidy"
// happy while the canonical wire format stays ZAP.
package p2p
