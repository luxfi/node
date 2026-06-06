// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"cmp"
	"errors"
	"sort"

	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/pcodecs"
	"github.com/luxfi/runtime"
	"github.com/luxfi/utils"
)

// PlatformVM uses a single feature extension (secp256k1fx) so the XVM concept
// of an Fx index doesn't apply. We keep the layout (FxIndex + Outs) so that
// the wire bytes match the XVM CreateAssetTx; FxIndex is required to be zero
// (secp256k1fx) for every initial state on this VM.
const platformFxIndex = 0

var (
	ErrNilInitialState     = errors.New("nil initial state is not valid")
	ErrNilInitialStateOut  = errors.New("nil initial state output is not valid")
	ErrInitialStateNonZero = errors.New("initial state fx index must be zero on P-Chain (single secp256k1fx)")
	ErrInitialStateOutsBad = errors.New("initial state outputs are not sorted")

	_ utils.Sortable[*InitialState] = (*InitialState)(nil)
)

// InitialState is the initial state of a single Fx for a new asset.
// On PlatformVM there is exactly one Fx (secp256k1fx, index 0) so [FxIndex]
// is required to be zero.
type InitialState struct {
	FxIndex uint32         `serialize:"true"  json:"fxIndex"`
	Outs    []verify.State `serialize:"true"  json:"outputs"`
}

// InitRuntime is a no-op; verify.State outputs do not require runtime context.
func (is *InitialState) InitRuntime(_ *runtime.Runtime) {}

// Verify returns nil iff this InitialState is well formed.
func (is *InitialState) Verify(c pcodecs.Manager) error {
	switch {
	case is == nil:
		return ErrNilInitialState
	case is.FxIndex != platformFxIndex:
		return ErrInitialStateNonZero
	}

	for _, out := range is.Outs {
		if out == nil {
			return ErrNilInitialStateOut
		}
		if err := out.Verify(); err != nil {
			return err
		}
	}
	if !isSortedState(is.Outs, c) {
		return ErrInitialStateOutsBad
	}
	return nil
}

// Compare provides a deterministic ordering by FxIndex.
func (is *InitialState) Compare(other *InitialState) int {
	return cmp.Compare(is.FxIndex, other.FxIndex)
}

// Sort orders the outputs of this InitialState by their byte representation.
func (is *InitialState) Sort(c pcodecs.Manager) {
	sortState(is.Outs, c)
}

type innerSortState struct {
	vers  []verify.State
	codec pcodecs.Manager
}

func (s *innerSortState) Less(i, j int) bool {
	iV := s.vers[i]
	jV := s.vers[j]
	iBytes, err := s.codec.Marshal(CodecVersion, &iV)
	if err != nil {
		return false
	}
	jBytes, err := s.codec.Marshal(CodecVersion, &jV)
	if err != nil {
		return false
	}
	return bytes.Compare(iBytes, jBytes) == -1
}

func (s *innerSortState) Len() int      { return len(s.vers) }
func (s *innerSortState) Swap(i, j int) { s.vers[j], s.vers[i] = s.vers[i], s.vers[j] }

func sortState(vers []verify.State, c pcodecs.Manager) {
	sort.Sort(&innerSortState{vers: vers, codec: c})
}

func isSortedState(vers []verify.State, c pcodecs.Manager) bool {
	return sort.IsSorted(&innerSortState{vers: vers, codec: c})
}
