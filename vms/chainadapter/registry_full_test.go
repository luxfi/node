// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.

package chainadapter

import (
	"errors"
	"testing"
)

// TestNewExtendedAdapter_DispatchesViaSeedConstructor verifies that the
// data-driven dispatch returns concrete adapters for every seed row that
// carries a Constructor. The taxonomy is the single source of truth — no
// switch is involved in the lookup.
func TestNewExtendedAdapter_DispatchesViaSeedConstructor(t *testing.T) {
	tax := DefaultChainTaxonomy()
	withCtor := 0
	for _, chain := range tax.All() {
		if chain.Constructor == nil {
			continue
		}
		withCtor++
		adapter, err := NewExtendedAdapter(chain.ID)
		if err != nil {
			t.Fatalf("chain %d (%s): expected adapter, got error %v", chain.ID, chain.Name, err)
		}
		if adapter == nil {
			t.Fatalf("chain %d (%s): expected non-nil adapter", chain.ID, chain.Name)
		}
		if adapter.ChainID() != chain.ID {
			t.Fatalf("chain %d (%s): adapter reported ChainID %d", chain.ID, chain.Name, adapter.ChainID())
		}
	}
	if withCtor == 0 {
		t.Fatal("expected at least one seed row to carry a Constructor")
	}
}

// TestNewExtendedAdapter_NilConstructorReturnsUnsupported verifies that seed
// rows without a Constructor cleanly return ErrChainNotSupported rather than
// panicking or returning a partially-constructed adapter.
func TestNewExtendedAdapter_NilConstructorReturnsUnsupported(t *testing.T) {
	tax := DefaultChainTaxonomy()
	tested := 0
	for _, chain := range tax.All() {
		if chain.Constructor != nil {
			continue
		}
		tested++
		adapter, err := NewExtendedAdapter(chain.ID)
		if adapter != nil {
			t.Fatalf("chain %d (%s): expected nil adapter for nil Constructor, got %T", chain.ID, chain.Name, adapter)
		}
		if !errors.Is(err, ErrChainNotSupported) {
			t.Fatalf("chain %d (%s): expected ErrChainNotSupported, got %v", chain.ID, chain.Name, err)
		}
	}
	if tested == 0 {
		t.Fatal("expected at least one seed row to have nil Constructor (residual)")
	}
}

// TestNewExtendedAdapter_UnknownIDReturnsUnsupported guards the case where the
// caller hands an id that is not in the seed at all.
func TestNewExtendedAdapter_UnknownIDReturnsUnsupported(t *testing.T) {
	adapter, err := NewExtendedAdapter(ChainID(0xDEADBEEF))
	if adapter != nil {
		t.Fatalf("expected nil adapter for unknown id, got %T", adapter)
	}
	if !errors.Is(err, ErrChainNotSupported) {
		t.Fatalf("expected ErrChainNotSupported, got %v", err)
	}
}
