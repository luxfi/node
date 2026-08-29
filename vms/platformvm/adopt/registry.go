// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package adopt

import (
	"errors"
	"fmt"

	"github.com/luxfi/ids"
)

// registry.go — the rules that need to see more than one record.
//
// Record.Valid answers whether a record is coherent by itself. Everything
// here is about how records relate: what may be adopted when, what an anchor
// may become, and what may be released. Those questions cannot be answered
// from one record, which is why they live with the register rather than on
// the type.

var (
	ErrAlreadyAdopted = errors.New("adopt: already adopted")
	ErrNotAdopted     = errors.New("adopt: not adopted")
	ErrNoParent       = errors.New("adopt: the network it takes security from is not adopted")
	ErrParentHeld     = errors.New("adopt: a network still takes security from this one")
	ErrWeakerAnchor   = errors.New("adopt: weakening an anchor is a separate act")
)

// Registry holds the adopted networks.
type Registry struct {
	byID map[ids.ID]Record
}

func NewRegistry() *Registry { return &Registry{byID: map[ids.ID]Record{}} }

// Get returns a record and whether it is there.
func (g *Registry) Get(id ids.ID) (Record, bool) {
	r, ok := g.byID[id]
	return r, ok
}

// Len is how many networks are adopted.
func (g *Registry) Len() int { return len(g.byID) }

// Adopt records a network.
//
// The rule with teeth: a network that takes its security from another may not
// be adopted before that other one. It is not bookkeeping. A Base state root
// is meaningful because it is posted to Ethereum and challengeable there — so
// adopting Base alone records a belief whose entire basis is a chain the
// register has never heard of, and the anchor cites a proof nobody can check.
// Ethereum first, then Base against it.
func (g *Registry) Adopt(r Record) error {
	if err := r.Valid(); err != nil {
		return err
	}
	if _, exists := g.byID[r.Key()]; exists {
		return fmt.Errorf("%w: %s", ErrAlreadyAdopted, r.Key())
	}
	if !r.Sovereign() {
		if _, ok := g.byID[r.Parent]; !ok {
			return fmt.Errorf("%w: %s", ErrNoParent, r.Parent)
		}
	}
	g.byID[r.Key()] = r
	return nil
}

// Revise changes a record in place.
//
// An anchor may be strengthened freely: it costs nobody anything and every
// consumer is relying on less than it now gets. Weakening one is refused here
// and needs its own act, because a bridge, an indexer or an interface deciding
// what to show a user all read the anchor to know what a message is worth —
// and weakening it changes what every one of them is trusting without any of
// them being asked.
//
// A record's identity and its parent do not change. A network that forks is a
// different network and gets its own record; a network that changes where its
// security comes from has become something else.
func (g *Registry) Revise(r Record) error {
	if err := r.Valid(); err != nil {
		return err
	}
	old, ok := g.byID[r.Key()]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotAdopted, r.Key())
	}
	if r.Parent != old.Parent {
		return fmt.Errorf("adopt: security source is not revisable: %s", r.Key())
	}
	if r.Anchor < old.Anchor {
		return fmt.Errorf("%w: %s -> %s", ErrWeakerAnchor, old.Anchor, r.Anchor)
	}
	g.byID[r.Key()] = r
	return nil
}

// Weaken lowers an anchor. Separate from Revise so that it cannot happen by
// accident, and so the act is legible in a log as what it is.
func (g *Registry) Weaken(id ids.ID, to Anchor) error {
	r, ok := g.byID[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotAdopted, id)
	}
	if to < Declared || to > Proven {
		return ErrBadAnchor
	}
	if to >= r.Anchor {
		return fmt.Errorf("adopt: %s is not weaker than %s", to, r.Anchor)
	}
	r.Anchor = to
	// Dropping below the custodial line drops the key with it. Leaving a
	// custody key on a record that may no longer hold value would be a record
	// that contradicts itself, and the whole point of the anchor is that
	// downstream reads one field to know what it may do.
	if !to.Custodial() {
		r.Custody = ""
	}
	g.byID[id] = r
	return nil
}

// Release ends an adoption.
//
// Refused while another adopted network takes its security from this one: the
// survivor's anchor is a claim about the released chain, and releasing the
// chain it cites makes that claim unreadable. This is the adoption ordering
// running backwards, and it is the same rule.
//
// Release is not a delete of history. Positions attested before release remain
// attested, because they were: what release ends is new crossings, not the
// evidence for old ones.
func (g *Registry) Release(id ids.ID) error {
	if _, ok := g.byID[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotAdopted, id)
	}
	for _, r := range g.byID {
		if r.Parent == id {
			return fmt.Errorf("%w: %s", ErrParentHeld, r.Key())
		}
	}
	delete(g.byID, id)
	return nil
}

// MayHold reports whether value may cross to this network — the single
// question a bridge asks before releasing.
//
// Unadopted answers no. That is the difference this register exists to make:
// before it, a bridge trusted a config file and nothing on chain said the
// estate had ever sanctioned it.
func (g *Registry) MayHold(id ids.ID) bool {
	r, ok := g.byID[id]
	return ok && r.Anchor.Custodial()
}
