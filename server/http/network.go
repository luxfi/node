// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.
// SPDX-License-Identifier: BSD-3-Clause-Eco

package server

import (
	"strconv"
	"strings"

	genesisconfigs "github.com/luxfi/genesis/configs"
)

// Network is the network a node runs, and so the chain aliases that are its
// own to serve.
//
// A node knows one network. That network owns a set of names, and a name it
// does not own is not this node's to answer — however the caller spells it.
// Five nodes have been running on Hanzo's network while serving an EVM named
// C-Chain, which is the Lux primary network's EVM and no one else's: a caller
// reading that answer cannot tell which network replied.
type Network struct {
	// family is the brand, empty when nobody has written this network down.
	family genesisconfigs.NetworkFamily
	// evm is the EVM chain id this network runs, 0 when unknown.
	evm uint64
	// alias is the name this network's own EVM answers to.
	alias string
}

// NetworkOf is the network a --network-id names.
//
// Two axes meet here. On the Lux primary network the id is 1/2/3/1337 and the
// EVM it runs carries a different number. On a sovereign L1 the two are the
// same number by construction — one id per brand per environment — which is
// what makes a node's own id enough to say which chains are its own.
//
// An id in neither set is a network nobody has written down: a private net, a
// test net. It claims nothing and refuses nothing, so it keeps working.
func NetworkOf(id uint32) Network {
	if evm, ok := genesisconfigs.EVMChainID(genesisconfigs.FamilyLux, id); ok {
		return Network{family: genesisconfigs.FamilyLux, evm: evm, alias: aliasOf(genesisconfigs.FamilyLux)}
	}
	if family, ok := genesisconfigs.NetworkFamilyOf(uint64(id)); ok {
		return Network{family: family, evm: uint64(id), alias: aliasOf(family)}
	}
	// An unwritten network still runs an EVM, and this node has always called
	// that EVM the C-Chain. Keeping the name is the behaviour it already has.
	return Network{alias: aliasOf(genesisconfigs.FamilyLux)}
}

// Alias is the one name this network's own EVM answers to.
func (n Network) Alias() string {
	return n.alias
}

// Aliases are every name this network's own EVM answers to: the brand, and the
// EVM chain id a wallet already knows it by.
func (n Network) Aliases() []string {
	if n.evm == 0 {
		return []string{n.alias}
	}
	return []string{n.alias, strconv.FormatUint(n.evm, 10)}
}

// Owns reports whether a chain alias is this network's to serve.
//
// The table claims a closed set of names — the brands, and the EVM chain id
// each brand runs in each environment. A name outside it is nobody's to
// police: a chain id, a letter, a VM's own word all belong to whoever
// registers them, as before. A name inside it belongs to exactly one network,
// and every other node answers 404.
func (n Network) Owns(alias string) bool {
	// A network nobody wrote down cannot be told what is its own, and a node
	// that refuses its own chains is worse than one that serves a stale name.
	if n.family == "" {
		return true
	}
	family, evm, claimed := claim(alias)
	if !claimed {
		return true
	}
	if family != n.family {
		return false
	}
	// A brand names its family in any environment; a number names exactly one,
	// so a mainnet node does not answer for testnet's chain id.
	return evm == 0 || evm == n.evm
}

// claim is the family a chain alias names, and the one EVM chain id it names
// when it names a number rather than a brand.
//
// The `-Chain` a chain carries in its own name is folded off, because
// `c-chain` names the C-Chain exactly as `c` does and a rule that stopped at
// one of them would be a rule with a door in it.
func claim(alias string) (genesisconfigs.NetworkFamily, uint64, bool) {
	name := strings.TrimSuffix(strings.ToLower(alias), "-chain")
	if family, ok := familyOf(name); ok {
		return family, 0, true
	}
	if evm, err := strconv.ParseUint(name, 10, 64); err == nil {
		if family, ok := genesisconfigs.NetworkFamilyOf(evm); ok {
			return family, evm, true
		}
	}
	return "", 0, false
}

// aliasOf is the name a family's own EVM answers to. Every family answers to
// its own name; the Lux primary network's EVM has been the C-Chain since
// before the families were written down, and answers to `c`.
func aliasOf(family genesisconfigs.NetworkFamily) string {
	if family == genesisconfigs.FamilyLux {
		return "c"
	}
	return string(family)
}

// familyOf reverses aliasOf.
func familyOf(alias string) (genesisconfigs.NetworkFamily, bool) {
	if alias == aliasOf(genesisconfigs.FamilyLux) {
		return genesisconfigs.FamilyLux, true
	}
	return genesisconfigs.ParseNetworkFamily(alias)
}
