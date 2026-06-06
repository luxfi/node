// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package pcodecs is the canonical construction site for every
// codec.Manager + codec instance under node/vms. Wave 2G-Internal of
// the codec rip (#101) replaces the previous direct imports of
// github.com/luxfi/codec / linearcodec / zapcodec with a single
// import of github.com/luxfi/proto/zap_codec — the proto-layer wire
// codec entry point established in Wave 2G-Wallet.
//
// Layout:
//
//   - Type aliases re-export the proto/zap_codec surfaces every VM
//     consumer needs (Manager, Registry, LinearCodec, ZAPCodec, Errs,
//     Packer, VersionSize, sentinel errors).
//   - Constructor helpers return fresh Manager / Codec instances at
//     canonical size budgets (Default, MaxInt32, MaxInt, Sized). They
//     are wire-agnostic — each VM's own codec.go layer registers its
//     type set on top.
//
// pcodecs has NO knowledge of any specific VM's type set. VM-specific
// registration stays in the VM's own codec.go (e.g.
// vms/platformvm/txs/codec.go). This split keeps pcodecs a leaf package
// — every node/vms subpackage is free to import it without closing an
// import cycle.
//
// Wire format: ZAP-native (little-endian) — the underlying impl is
// luxfi/codec/zapcodec routed through proto/zap_codec. The "Linear"
// name on LinearCodec / NewLinearCodec refers to LINEAR TYPE-ID
// ASSIGNMENT (sequential ids assigned in registration order), NOT to
// the historical linearcodec big-endian wire format. Activation
// per LP-023 (proto/zap_native/codec_select.go: ZAPActivationUnix=0).
package pcodecs

import (
	"math"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/zapcodec"
	"github.com/luxfi/proto/zap_codec"
)

// Manager is the multi-version wire codec surface every node/vms
// consumer holds. Aliased to zap_codec.MultiManager so the rest of
// node/vms references pcodecs.Manager rather than reaching into
// proto/zap_codec or luxfi/codec directly.
type Manager = zap_codec.MultiManager

// Registry is the type-registration surface. Aliased to
// zap_codec.Registry — structurally identical to the legacy
// codec.Registry interface (RegisterType only).
type Registry = zap_codec.Registry

// LinearCodec is the union of Registry + Codec + SkipRegistrations.
// Used by VMs that need SkipRegistrations on their per-version codec
// to preserve historical type-id slot layouts (Apricot / Banff /
// Durango / Quasar registration sequences).
//
// Despite the name, this codec is backed by luxfi/codec/zapcodec — the
// "Linear" refers to LINEAR TYPE-ID ASSIGNMENT, not to the legacy
// linearcodec big-endian wire layout.
type LinearCodec = zap_codec.LinearCodec

// ZAPCodec is an explicit alias for the same zapcodec-backed Codec
// surface — used by post-cutover wire layouts (currently platformvm
// txs V2) that want to emphasise the ZAP-native wire layout. Same
// backing type as LinearCodec; the alias exists for readability.
type ZAPCodec = zap_codec.ZAPCodec

// Errs is the multi-error accumulator used by per-VM codec.go files
// that fan in many RegisterCodec / RegisterType calls under a single
// error tap.
type Errs = zap_codec.Errs

// Packer is the wire-byte packer used by VM tests that drive
// MarshalInto / UnmarshalFrom byte streams directly.
type Packer = zap_codec.Packer

// VersionSize is the on-wire length of the codec-version prefix
// (2 bytes). VM fee-complexity calculations subtract this from
// observed byte sizes to isolate the payload component.
const VersionSize = zap_codec.VersionSize

// Sentinel errors re-exported from proto/zap_codec so VM packages can
// assert on them without importing proto/zap_codec themselves.
var (
	ErrCantPackVersion = zap_codec.ErrCantPackVersion
	ErrCantUnpackVersion = zap_codec.ErrCantUnpackVersion
	ErrUnknownVersion = zap_codec.ErrUnknownVersion
	// ErrMaxSliceLenExceeded points at the codec-layer sentinel that the
	// underlying reflect/zapcodec impls actually wrap. The proto layer
	// defines its own value with a different string ("zap_codec: max
	// slice length exceeded") which is unused by the runtime decode
	// path — using that would not chain through errors.Is.
	ErrMaxSliceLenExceeded = codec.ErrMaxSliceLenExceeded
	ErrMarshalNil = zap_codec.ErrMarshalNil
	ErrUnmarshalNil = zap_codec.ErrUnmarshalNil
	ErrDoesNotImplementInterface = zap_codec.ErrDoesNotImplementInterface
	ErrExtraSpace = zap_codec.ErrExtraSpace

	// ErrInsufficientLength is the sentinel returned when a wire payload
	// is shorter than the inner Codec needs to decode a field. The proto
	// layer's multiManager.Unmarshal returns the inner zapcodec error
	// unwrapped (see proto/zap_codec/multi.go:Unmarshal), so callers
	// asserting on this value must point at the zapcodec-side sentinel —
	// the legacy wrappers-side sentinel is a different value with the
	// same human-readable text and does NOT chain through errors.Is.
	ErrInsufficientLength = zapcodec.ErrInsufficientLength
)

// LongLen is the on-wire length of a uint64 (8 bytes). Used by index
// code to size cursor buffers.
const LongLen = zap_codec.LongLen

// IntLen is the on-wire length of a uint32 (4 bytes). Used by p2p
// response-size accounting code.
const IntLen = zap_codec.IntLen

// ShortLen is the on-wire length of a uint16 (2 bytes).
const ShortLen = zap_codec.ShortLen

// ByteLen is the on-wire length of a uint8 (1 byte).
const ByteLen = zap_codec.ByteLen

// BoolLen is the on-wire length of a bool (1 byte).
const BoolLen = zap_codec.BoolLen

// NewLinearCodec returns a fresh ZAP-native Codec instance with the
// default ("serialize") struct tag. The "Linear" name refers to
// LINEAR TYPE-ID ASSIGNMENT (sequential ids assigned in registration
// order), NOT to the legacy linearcodec big-endian wire format.
func NewLinearCodec() LinearCodec {
	return zap_codec.NewLinearCodec()
}

// NewLinearCodecWithTags returns a fresh ZAP-native Codec instance
// that honours the supplied struct-tag names. Used by the metadata
// codec wiring where v0:"true" / v1:"true" tags select per-version
// field sets.
func NewLinearCodecWithTags(tags ...string) LinearCodec {
	return zap_codec.NewLinearCodecWithTags(tags...)
}

// NewZAPCodec returns a fresh ZAP-native Codec instance. Alias for
// NewLinearCodec — both return the same zapcodec-backed Codec; the
// name exists for call sites that want to emphasise the ZAP-native
// wire layout (e.g. platformvm txs V2).
func NewZAPCodec() ZAPCodec {
	return zap_codec.NewZAPCodec()
}

// NewDefaultManager returns a fresh multi-version Manager with the
// default 1 MiB wire-payload size.
func NewDefaultManager() Manager {
	return zap_codec.NewDefaultManager()
}

// NewManager returns a fresh multi-version Manager with the supplied
// max wire-payload size.
func NewManager(maxSize uint64) Manager {
	return zap_codec.NewManager(maxSize)
}

// NewMaxInt32Manager returns a fresh multi-version Manager sized for
// genesis-style blobs (math.MaxInt32 budget). VM genesis codec wiring
// reaches for this rather than re-deriving the budget at every call
// site.
func NewMaxInt32Manager() Manager {
	return zap_codec.NewMaxInt32Manager()
}

// NewMaxIntManager returns a fresh multi-version Manager sized for
// warp / proposervm-block style payloads (math.MaxInt budget —
// effectively unbounded). The p2p layer caps real-world wire sizes
// well below this.
func NewMaxIntManager() Manager {
	return zap_codec.NewManager(uint64(math.MaxInt))
}
