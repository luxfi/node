// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stakeable

import (
	"errors"
	"fmt"

	luxcomp "github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/runtime"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/wire"
	"github.com/luxfi/vm/components/verify"
)

var (
	errInvalidLocktime      = errors.New("invalid locktime")
	errNestedStakeableLocks = errors.New("shouldn't nest stakeable locks")
)

// init registers the LockedOutput wire handler with the cross-fx
// dispatcher in node/vms/components/lux. Without this registration the
// dispatcher returns "unknown (TypeKind=0x00, ShapeKind=0x0F)" for every
// vesting-allocation UTXO in genesis — observed live as
// `platform.getBalance = 0` for every funded P-chain address on
// mainnet/testnet/devnet despite the genesis allocations being
// well-formed and the supply tally matching the sum of allocations.
//
// The cycle that forced this registration pattern: stakeable.LockOut
// embeds utxo.TransferableOut so node/vms/components/lux cannot import
// stakeable directly (would import the embed source through the embed,
// breaking the dispatcher package's "no fx-specific deps" property).
// The registration lets stakeable own the *LockOut reconstruction
// without flipping the dependency graph.
func init() {
	luxcomp.RegisterLockedOutputHandler(func(b []byte) (verify.State, error) {
		lo, err := wire.WrapLockedOutput(b)
		if err != nil {
			return nil, fmt.Errorf("wire.WrapLockedOutput: %w", err)
		}
		innerState, err := luxcomp.WrapOutputBytes(lo.TransferOutBytes())
		if err != nil {
			return nil, fmt.Errorf("inner output dispatch: %w", err)
		}
		inner, ok := innerState.(lux.TransferableOut)
		if !ok {
			return nil, fmt.Errorf("inner state is %T, not lux.TransferableOut — LockOut envelope corrupted", innerState)
		}
		return &LockOut{
			Locktime:        lo.Locktime(),
			TransferableOut: inner,
		}, nil
	})
}

type LockOut struct {
	Locktime            uint64 `serialize:"true" json:"locktime"`
	lux.TransferableOut `serialize:"true" json:"output"`
}

func (s *LockOut) InitRuntime(rt *runtime.Runtime) {
	// Initialize the context for the underlying output if it supports it
	if contextOutput, ok := s.TransferableOut.(interface{ InitRuntime(*runtime.Runtime) }); ok {
		contextOutput.InitRuntime(rt)
	}
}

func (s *LockOut) Addresses() [][]byte {
	if addressable, ok := s.TransferableOut.(lux.Addressable); ok {
		return addressable.Addresses()
	}
	return nil
}

func (s *LockOut) Verify() error {
	if s.Locktime == 0 {
		return errInvalidLocktime
	}
	if _, nested := s.TransferableOut.(*LockOut); nested {
		return errNestedStakeableLocks
	}
	return s.TransferableOut.Verify()
}

// Bytes returns the ZAP-native wire envelope for this LockOut.
// Envelope = (TypeKindReserved, ShapeKindLockedOutput, ZAP message)
// where the ZAP message carries Locktime and the inner TransferableOut's
// own wire envelope (which carries its own TypeKind+ShapeKind+message).
//
// The inner TransferableOut MUST satisfy the wireSerializable contract
// (Bytes() []byte) — every fx package's wire.go adapter does. Nested
// LockOut is forbidden by Verify() and panics here to surface the bug.
func (s *LockOut) Bytes() []byte {
	type wireSerializable interface {
		Bytes() []byte
	}
	inner, ok := s.TransferableOut.(wireSerializable)
	if !ok {
		panic("stakeable.LockOut: inner TransferableOut does not implement wire-serializable (Bytes() []byte)")
	}
	return wire.NewLockedOutput(wire.LockedOutputInput{
		Locktime:         s.Locktime,
		TransferOutBytes: inner.Bytes(),
	})
}

type LockIn struct {
	Locktime           uint64 `serialize:"true" json:"locktime"`
	lux.TransferableIn `serialize:"true" json:"input"`
}

func (s *LockIn) Verify() error {
	if s.Locktime == 0 {
		return errInvalidLocktime
	}
	if _, nested := s.TransferableIn.(*LockIn); nested {
		return errNestedStakeableLocks
	}
	return s.TransferableIn.Verify()
}

func (s *LockIn) InitRuntime(rt *runtime.Runtime) {
	if contextInput, ok := s.TransferableIn.(interface{ InitRuntime(*runtime.Runtime) }); ok {
		contextInput.InitRuntime(rt)
	}
}
