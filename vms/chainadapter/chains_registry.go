// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.

package chainadapter

import (
	"errors"
	"sync"
)

// Chain is one row of the supported-chain registry. Pure data plus an optional
// per-row adapter factory. Mirrors the precompile/bridge shape so a chain
// becomes one row in the seed rather than a brand-keyed const token in source.
//
// Constructor is the data-driven dispatch seam that replaces the brand-keyed
// switch in NewExtendedAdapter: each row carries its own factory closure, so
// adding a chain with adapter support is one row edit, not a switch edit.
// Rows with nil Constructor have no adapter implementation in this package
// today; lookup returns nil cleanly.
type Chain struct {
	ID          ChainID            // canonical numeric id (taxonomic, not EIP-155)
	Name        string             // canonical lowercase short name (e.g. "bitcoin", "ethereum")
	EVM         bool               // native EVM (eth_chainId) vs virtual / bookkeeping
	Constructor func() ChainAdapter // per-row adapter factory; nil if no adapter ships in this package
}

// ChainTaxonomy resolves chains by id or canonical name and enumerates the
// supported set. Distinct from the adapter Registry (which holds live adapter
// instances) and from ChainRegistry (which holds *ChainConfig values); this
// is the static taxonomy of supported chain rows backing the package's
// internal id<->name mapping.
type ChainTaxonomy interface {
	Get(id ChainID) (Chain, bool)
	GetByName(name string) (Chain, bool)
	All() []Chain
}

// ErrChainNameUnknown is returned by MustChainID when the name has no row.
var ErrChainNameUnknown = errors.New("chain name not registered")

// staticChainTaxonomy is an immutable in-memory ChainTaxonomy.
type staticChainTaxonomy struct {
	chains []Chain
	byID   map[ChainID]Chain
	byName map[string]Chain
}

// NewStaticChainTaxonomy returns a ChainTaxonomy over an immutable list.
func NewStaticChainTaxonomy(chains []Chain) ChainTaxonomy {
	byID := make(map[ChainID]Chain, len(chains))
	byName := make(map[string]Chain, len(chains))
	out := make([]Chain, len(chains))
	for i, c := range chains {
		out[i] = c
		byID[c.ID] = c
		byName[c.Name] = c
	}
	return &staticChainTaxonomy{chains: out, byID: byID, byName: byName}
}

func (r *staticChainTaxonomy) Get(id ChainID) (Chain, bool) {
	c, ok := r.byID[id]
	return c, ok
}

func (r *staticChainTaxonomy) GetByName(name string) (Chain, bool) {
	c, ok := r.byName[name]
	return c, ok
}

func (r *staticChainTaxonomy) All() []Chain {
	out := make([]Chain, len(r.chains))
	copy(out, r.chains)
	return out
}

// defaultTaxonomyMu guards lazy construction of the default taxonomy.
var (
	defaultTaxonomyMu sync.RWMutex
	defaultTaxonomy   ChainTaxonomy
)

// DefaultChainTaxonomy returns the process-wide chain taxonomy, building it
// once from DefaultChainSeed on first call.
func DefaultChainTaxonomy() ChainTaxonomy {
	defaultTaxonomyMu.RLock()
	if r := defaultTaxonomy; r != nil {
		defaultTaxonomyMu.RUnlock()
		return r
	}
	defaultTaxonomyMu.RUnlock()
	defaultTaxonomyMu.Lock()
	defer defaultTaxonomyMu.Unlock()
	if defaultTaxonomy == nil {
		defaultTaxonomy = NewStaticChainTaxonomy(DefaultChainSeed())
	}
	return defaultTaxonomy
}

// SetDefaultChainTaxonomy replaces the process-wide taxonomy. Intended for
// tests and operators that want to inject a custom set; passing nil resets
// to the lazy default.
func SetDefaultChainTaxonomy(r ChainTaxonomy) {
	defaultTaxonomyMu.Lock()
	defaultTaxonomy = r
	defaultTaxonomyMu.Unlock()
}

// mustID resolves a canonical chain name to its ID via the default taxonomy,
// panicking if the name is not registered. Used at package-init time to bind
// stable local IDs to canonical names — this is the seam that replaces the
// brand-keyed const enum: adding a chain is one row in DefaultChainSeed and a
// matching mustID() call where it is consumed, with zero const tokens.
func mustID(name string) ChainID {
	c, ok := DefaultChainTaxonomy().GetByName(name)
	if !ok {
		panic(ErrChainNameUnknown.Error() + ": " + name)
	}
	return c.ID
}
