// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// adopt.hpp — the register of networks Lux did not create.
//
// Rendered from Go vms/platformvm/adopt (adopt.go, registry.go). See LP-1021.
//
// A network normally reaches the P-chain by being made: a chain is created, a
// validator set is assigned, it produces blocks. That path assumes the network
// does not exist yet. Ethereum, Bitcoin and Solana all exist, and none of it is
// Lux's to create or validate.
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

#pragma once

#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"

#include <cstdint>
#include <map>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

namespace lux::platformvm::adopt {

// Why a message attributed to an adopted network is believed. It is the whole
// security boundary: everything downstream inherits exactly this and nothing
// more.
enum class Anchor : std::uint8_t {
    // Governance asserted the network is what it says it is. Proves the estate
    // agreed and nothing else. Enough to list a network, open markets against
    // it and issue assets — never enough to hold value, because signing a
    // transfer on a chain nobody has verified is custody without evidence.
    Declared = 1,
    // A threshold of the validator set independently observed the network and
    // signed what they saw. Proves they read the same thing; leaves open
    // whether they read it correctly, which is why the reorg depth belongs in
    // the record. This is the level at which value may cross.
    Attested = 2,
    // The network's own consensus is verified — a light client, a committee
    // signature, a validity proof. Proves the message is in a block the
    // network's own validators finalised.
    Proven = 3,
};

std::string_view anchor_name(Anchor a);

// Whether value may be held under this anchor. The line sits between declared
// and attested, and it is the reason adoption can be permissionless: paying a
// fee buys a record, and a record is not a trust decision. Attestation cannot
// be bought, because what it requires is the committee actually observing the
// network.
inline bool custodial(Anchor a) { return a >= Anchor::Attested; }

// How value is held on the adopted network.
enum class Holding : std::uint8_t {
    // A contract or program holds the position and a release is a call to it.
    // Every EVM, Solana, TON.
    Gateway = 1,
    // There is nowhere to put a contract, so the custody address IS the
    // gateway: a deposit is the lock and a signed spend is the release.
    // Bitcoin. Everything a contract would have enforced moves into the
    // attestation, which makes the anchor matter more here, not less.
    Address = 2,
};

std::string_view holding_name(Holding h);

// One adopted network.
struct Record {
    // The network's own chain id — 1 for Ethereum, 8453 for Base.
    std::uint64_t chain_id = 0;
    // WHICH chain bears that id: a genesis hash, or an equivalent. A chain id
    // alone is not an identity. Ids collide across testnets, and a fork
    // inherits the id of the chain it left, so without this a split is
    // invisible to the register.
    Id identity{};
    // The adopted network this one takes its security from, or empty when the
    // network is sovereign. Base names Ethereum; Ethereum names nothing.
    Id parent{};
    Anchor anchor = Anchor::Declared;
    Holding holding = Holding::Gateway;
    // The key that signs for this network. Empty while the anchor is declared,
    // because a declared anchor may not hold value.
    std::string custody;
    std::vector<std::string> endpoints;
    // How many of the adopted network's blocks must pass before its state is
    // attested. A committee that reads a reorged chain attests a reorged chain,
    // so this is the register's own statement of how much reorg it tolerates.
    std::uint64_t depth = 0;

    friend bool operator==(const Record&, const Record&) = default;

    // A record is known by its identity commitment rather than its chain id, so
    // a fork does not silently overwrite the chain it left.
    const Id& key() const { return identity; }
    bool sovereign() const { return parent == kEmptyId; }

    // Well formed on its own terms. It says nothing about the register it is
    // going into — those rules need to see other records.
    Status valid() const;
};

// The adopted networks.
class Registry {
  public:
    std::optional<Record> get(const Id& id) const;
    std::size_t size() const { return by_id_.size(); }

    // Record a network.
    //
    // The rule with teeth: a network that takes its security from another may
    // not be adopted before that other one. It is not bookkeeping. A Base state
    // root is meaningful because it is posted to Ethereum and challengeable
    // there — so adopting Base alone records a belief whose entire basis is a
    // chain the register has never heard of, and the anchor cites a proof
    // nobody can check. Ethereum first, then Base against it.
    Status adopt(const Record& r);

    // Change a record in place.
    //
    // An anchor may be strengthened freely: it costs nobody anything and every
    // consumer is relying on less than it now gets. Weakening one is refused
    // here and needs its own act, because a bridge, an indexer or an interface
    // deciding what to show a user all read the anchor to know what a message
    // is worth — and weakening it changes what every one of them is trusting
    // without any of them being asked.
    //
    // An identity and a security source do not change. A network that forks is
    // a different network and gets its own record; a network that changes where
    // its security comes from has become something else.
    Status revise(const Record& r);

    // Lower an anchor. Separate from revise so it cannot happen by accident,
    // and so the act is legible as what it is.
    Status weaken(const Id& id, Anchor to);

    // End an adoption. Refused while another adopted network takes its security
    // from this one: the survivor's anchor is a claim about the released chain,
    // and releasing the chain it cites makes that claim unreadable. That is the
    // adoption ordering running backwards, and it is the same rule.
    //
    // It is not a delete of history. What was attested before a release stays
    // attested, because it was: what a release ends is new crossings, not the
    // evidence for old ones.
    Status release(const Id& id);

    // Whether value may cross to this network — the single question a bridge
    // asks before releasing. An unadopted network answers no, which is the
    // difference this register exists to make: before it, a bridge trusted a
    // file on disk and nothing on chain said the estate had ever sanctioned it.
    bool may_hold(const Id& id) const;

  private:
    std::map<Id, Record> by_id_;
};

}  // namespace lux::platformvm::adopt
