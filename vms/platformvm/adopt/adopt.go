// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package adopt is the register of networks Lux did not create.
//
// A network normally reaches the P-chain by being made: a chain is created, a
// validator set is assigned, it produces blocks. That path assumes the network
// does not exist yet. Ethereum, Bitcoin, Solana and Base all exist, and none of
// it is Lux's to create or validate.
//
// Adoption records a BELIEF about such a network — that it is the one named,
// reachable where stated, and that messages attributed to it can be trusted on
// a stated basis. It confers nothing on the adopted network, which has agreed
// to nothing, and it grants Lux no authority over it.
//
// The register is here rather than in a contract because every subsystem needs
// to read it before acting: the bridge before releasing, the oracle before
// attesting, the custody chain before signing. A P-chain record is the one
// thing all of them can already see.
//
// See LP-1021.
package adopt

import (
	"errors"
	"fmt"

	"github.com/luxfi/ids"
)

// Anchor is the basis for believing a message attributed to an adopted
// network. It is the whole security boundary: everything downstream inherits
// exactly this and nothing more.
type Anchor uint8

const (
	// Declared: governance asserted the network is what it says it is.
	// Proves the estate agreed and nothing else. Enough to list a network,
	// open markets against it and issue assets — never enough to hold value,
	// because signing a transfer on a chain nobody has verified is custody
	// without evidence.
	Declared Anchor = iota + 1

	// Attested: t+1 of the validator set independently observed the network
	// and threshold-signed what they saw. Proves they read the same thing;
	// leaves open whether they read it correctly, which is why finality depth
	// belongs in the record. This is the level at which value may cross.
	Attested

	// Proven: the network's own consensus is verified — a light client, a sync
	// committee signature, a validity proof. Proves the message is in a block
	// the network's own validators finalised.
	Proven
)

func (a Anchor) String() string {
	switch a {
	case Declared:
		return "declared"
	case Attested:
		return "attested"
	case Proven:
		return "proven"
	default:
		return fmt.Sprintf("anchor(%d)", uint8(a))
	}
}

// Custodial reports whether value may be held under this anchor.
//
// The line sits between Declared and Attested and is the reason adoption can
// be permissionless: paying a fee buys a record, and a record is not a trust
// decision. Attestation cannot be bought, because what it requires is the
// committee actually observing the network.
func (a Anchor) Custodial() bool { return a >= Attested }

// Holding is how value is held on the adopted network.
type Holding uint8

const (
	// Gateway: a contract or program holds the position and a release is a
	// call to it. Every EVM, Solana, TON.
	Gateway Holding = iota + 1

	// Address: there is nowhere to put a contract, so the custody address IS
	// the gateway — a deposit is the lock and a signed spend is the release.
	// Bitcoin. Everything a contract would have enforced moves into the
	// attestation, which makes the anchor matter more here, not less.
	Address
)

func (h Holding) String() string {
	switch h {
	case Gateway:
		return "gateway"
	case Address:
		return "address"
	default:
		return fmt.Sprintf("holding(%d)", uint8(h))
	}
}

// Record is one adopted network.
type Record struct {
	// ChainID is the network's own chain id — 1 for Ethereum, 8453 for Base.
	ChainID uint64

	// Identity commits to WHICH chain bears that id: a genesis hash, or an
	// equivalent. A chain id alone is not an identity. Ids collide across
	// testnets, and a fork inherits the id of the chain it left, so without
	// this a split is invisible to the register.
	Identity ids.ID

	// Parent is the adopted network this one takes its security from, or the
	// empty ID when the network is sovereign. Base names Ethereum; Ethereum
	// names nothing.
	Parent ids.ID

	// Anchor is why Lux believes messages attributed to this network.
	Anchor Anchor

	// Holding is how value is held there.
	Holding Holding

	// Custody is the M-Chain key that signs for this network. Empty while the
	// anchor is Declared, because a declared anchor may not hold value.
	Custody string

	// Endpoints is where the network is reachable.
	Endpoints []string

	// Depth is how many blocks of the adopted network must pass before its
	// state is attested. A committee that reads a reorged chain attests a
	// reorged chain, so this is the register's own statement of how much
	// reorg it is willing to tolerate.
	Depth uint64
}

// Key identifies a record. The identity commitment rather than the chain id,
// so a fork does not silently overwrite the chain it left.
func (r Record) Key() ids.ID { return r.Identity }

// Sovereign reports whether the network's security is its own.
func (r Record) Sovereign() bool { return r.Parent == ids.Empty }

var (
	ErrNoChainID     = errors.New("adopt: chain id required")
	ErrNoIdentity    = errors.New("adopt: identity commitment required")
	ErrBadAnchor     = errors.New("adopt: unknown anchor")
	ErrBadHolding    = errors.New("adopt: unknown holding")
	ErrNoEndpoints   = errors.New("adopt: at least one endpoint required")
	ErrSelfParent    = errors.New("adopt: a network cannot take security from itself")
	ErrCustodyUnheld = errors.New("adopt: a declared anchor may not carry custody")
	ErrNoCustody     = errors.New("adopt: an anchor that holds value needs a custody key")
)

// Valid reports whether a record is well formed on its own terms. It says
// nothing about the register it is going into — see Registry.Adopt for the
// rules that need to see other records.
func (r Record) Valid() error {
	switch {
	case r.ChainID == 0:
		return ErrNoChainID
	case r.Identity == ids.Empty:
		return ErrNoIdentity
	case r.Anchor < Declared || r.Anchor > Proven:
		return ErrBadAnchor
	case r.Holding < Gateway || r.Holding > Address:
		return ErrBadHolding
	case len(r.Endpoints) == 0:
		return ErrNoEndpoints
	case r.Parent == r.Identity:
		return ErrSelfParent
	}

	// The rule the whole permissionless path rests on. A declared anchor is a
	// record that the estate agreed, and agreement is not evidence — so it may
	// not hold a key. Conversely an anchor that may hold value and has no key
	// is a record that promises something it cannot do.
	if r.Anchor.Custodial() && r.Custody == "" {
		return ErrNoCustody
	}
	if !r.Anchor.Custodial() && r.Custody != "" {
		return ErrCustodyUnheld
	}
	return nil
}
