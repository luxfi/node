// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package pcodecs is the canonical construction site for every
// codec.Manager + linearcodec / zapcodec instance under node/vms. Wave 2D
// of the codec rip (#101) consolidates direct `github.com/luxfi/codec`
// imports here so the rest of node/vms can reach for typed Manager /
// Registry values without importing luxfi/codec themselves.
//
// Layout:
//
//   - Type aliases re-export the codec / linearcodec / zapcodec / wrappers
//     surfaces every VM consumer needs (Manager, Registry, LinearCodec,
//     ZAPCodec, Errs, Packer, VersionSize, sentinel errors).
//   - Constructor helpers return fresh Manager / linearcodec / zapcodec
//     instances at canonical size budgets (Default, MaxInt32, MaxInt,
//     Sized). They are wire-agnostic — each VM's own codec.go layer
//     registers its type set on top.
//   - Genesis / runtime helpers compose Manager + LinearCodec pairs for
//     the recurring "register types + register codec at version" pattern
//     used by every VM's init().
//
// pcodecs has NO knowledge of any specific VM's type set. VM-specific
// registration stays in the VM's own codec.go (e.g.
// vms/platformvm/txs/codec.go). This split keeps pcodecs a leaf package
// — every node/vms subpackage is free to import it without closing an
// import cycle.
package pcodecs

import (
	"math"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"
	"github.com/luxfi/codec/wrappers"
	"github.com/luxfi/codec/zapcodec"
)

// Manager is the codec.Manager surface every node/vms consumer holds.
// Aliased so the rest of node/vms references pcodecs.Manager rather than
// importing luxfi/codec directly.
type Manager = codec.Manager

// Registry is the codec.Registry surface used for type registration.
type Registry = codec.Registry

// LinearCodec is the linearcodec.Codec surface (Registry +
// SkipRegistrations). Several VMs need the SkipRegistrations method on
// their per-version codec, so the alias exposes the concrete linearcodec
// type rather than just codec.Registry.
type LinearCodec = linearcodec.Codec

// ZAPCodec is the zapcodec.Codec surface used by post-cutover wire
// layouts (platformvm V2). Same shape as LinearCodec; different wire
// encoding.
type ZAPCodec = zapcodec.Codec

// Errs is the wrappers.Errs multi-error accumulator. Used by per-VM
// codec.go files that fan in many RegisterCodec / RegisterType calls
// under a single error tap.
type Errs = wrappers.Errs

// Packer re-exports wrappers.Packer for VM tests that drive
// MarshalInto / UnmarshalFrom byte streams directly.
type Packer = wrappers.Packer

// VersionSize is the on-wire length of the codec-version prefix
// (codec.VersionSize == 2). VM fee-complexity calculations subtract
// this from observed byte sizes to isolate the payload component.
const VersionSize = codec.VersionSize

// Sentinel errors re-exported from luxfi/codec / luxfi/codec/wrappers so
// VM packages can assert on them without importing luxfi/codec.
var (
	ErrCantPackVersion           = codec.ErrCantPackVersion
	ErrCantUnpackVersion         = codec.ErrCantUnpackVersion
	ErrUnknownVersion            = codec.ErrUnknownVersion
	ErrMaxSliceLenExceeded       = codec.ErrMaxSliceLenExceeded
	ErrMarshalNil                = codec.ErrMarshalNil
	ErrUnmarshalNil              = codec.ErrUnmarshalNil
	ErrDoesNotImplementInterface = codec.ErrDoesNotImplementInterface
	ErrExtraSpace                = codec.ErrExtraSpace

	ErrInsufficientLength = wrappers.ErrInsufficientLength
)

// LongLen re-exports wrappers.LongLen (the on-wire length of a uint64
// — 8 bytes). Used by index code to size cursor buffers.
const LongLen = wrappers.LongLen

// NewLinearCodec returns a fresh linearcodec-backed Codec instance with
// the default tag set. Mirrors linearcodec.NewDefault().
func NewLinearCodec() LinearCodec {
	return linearcodec.NewDefault()
}

// NewLinearCodecWithTags returns a fresh linearcodec.Codec with the
// supplied struct-tag names. Mirrors linearcodec.New([]string{tag...}).
// Used by the metadata codec wiring where v0:"true" / v1:"true" tags
// select per-version field sets.
func NewLinearCodecWithTags(tags ...string) LinearCodec {
	return linearcodec.New(tags)
}

// NewZAPCodec returns a fresh zapcodec-backed Codec instance with the
// default tag set. Mirrors zapcodec.NewDefault(). Used by post-cutover
// wire formats (currently platformvm txs V2).
func NewZAPCodec() ZAPCodec {
	return zapcodec.NewDefault()
}

// NewDefaultManager returns a fresh codec.Manager with the default wire
// payload size. Mirrors codec.NewDefaultManager().
func NewDefaultManager() Manager {
	return codec.NewDefaultManager()
}

// NewManager returns a fresh codec.Manager with the supplied max wire
// payload size. Mirrors codec.NewManager(maxSize).
func NewManager(maxSize uint64) Manager {
	return codec.NewManager(maxSize)
}

// NewMaxInt32Manager returns a fresh codec.Manager sized for
// genesis-style blobs (math.MaxInt32 budget). VM genesis codec wiring
// reaches for this rather than re-deriving the budget at every call
// site.
func NewMaxInt32Manager() Manager {
	return codec.NewManager(math.MaxInt32)
}

// NewMaxIntManager returns a fresh codec.Manager sized for warp /
// proposervm-block style payloads (math.MaxInt budget — effectively
// unbounded). The p2p layer caps real-world wire sizes well below this.
func NewMaxIntManager() Manager {
	return codec.NewManager(math.MaxInt)
}
