// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build noblst || purego || !cgo
// +build noblst purego !cgo

package bls

type Signer interface {
	PublicKey() *PublicKey
	Sign(msg []byte) (*Signature, error)
	SignProofOfPossession(msg []byte) (*Signature, error)
}
