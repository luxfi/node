// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// adopt_test.cpp — the register of networks Lux did not create.
//
// Ported from Go vms/platformvm/adopt/adopt_test.go, including its fixtures:
// the real networks, as they actually relate.

#include "harness.hpp"
#include "lux/platformvm/adopt.hpp"

using namespace lux::platformvm;
using namespace lux::platformvm::adopt;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    v.b[0] = b;
    return v;
}

Record ethereum() {
    return Record{1, id_of(1), kEmptyId, Anchor::Attested, Holding::Gateway, "mchain/eth", {"https://eth"}, 64};
}
Record base() {
    return Record{8453,   id_of(2),          id_of(1), Anchor::Attested, Holding::Gateway,
                  "mchain/base", {"https://base"}, 200};
}
Record bitcoin() {
    return Record{0x1000, id_of(3), kEmptyId, Anchor::Attested, Holding::Address, "mchain/btc",
                  {"https://btc"}, 6};
}

}  // namespace

// A network that takes its security from another may not be adopted before that
// other one. Adopting Base alone records a belief whose entire basis is a chain
// the register has never heard of.
TEST(ANetworkCannotPrecedeTheOneItLeansOn) {
    Registry g;
    REQUIRE_ERR(g.adopt(base()), Err::NoParent);
    REQUIRE_EQ_NUM(0, g.size());

    REQUIRE_OK(g.adopt(ethereum()));
    REQUIRE_OK(g.adopt(base()));
    REQUIRE_EQ_NUM(2, g.size());

    // And a network already there is not adopted twice.
    REQUIRE_ERR(g.adopt(ethereum()), Err::AlreadyAdopted);
}

// The same rule backwards: releasing the network underneath would make the one
// above it unreadable, because its anchor is a claim about the one underneath.
TEST(ANetworkCannotBeReleasedWhileAnotherLeansOnIt) {
    Registry g;
    REQUIRE_OK(g.adopt(ethereum()));
    REQUIRE_OK(g.adopt(base()));

    REQUIRE_ERR(g.release(ethereum().key()), Err::ParentHeld);

    // Release the dependent first and the order opens up.
    REQUIRE_OK(g.release(base().key()));
    REQUIRE_OK(g.release(ethereum().key()));
    REQUIRE_EQ_NUM(0, g.size());
    REQUIRE_ERR(g.release(ethereum().key()), Err::NotAdopted);
}

// The line the permissionless path rests on. Paying buys a record; a record is
// not a trust decision, and a declared anchor may not hold value.
TEST(ADeclaredAnchorCannotHoldValue) {
    const Record paid{999, id_of(9), kEmptyId, Anchor::Declared, Holding::Gateway, "",
                      {"https://newchain"}, 0};

    Registry g;
    REQUIRE_OK(g.adopt(paid));
    REQUIRE(!g.may_hold(paid.key()));

    // And it cannot be smuggled in by attaching a key to it.
    Record bought = paid;
    bought.custody = "mchain/newchain";
    Registry other;
    REQUIRE_ERR(other.adopt(bought), Err::CustodyUnheld);
}

// An anchor that may hold value and names no key promises something it cannot
// do.
TEST(AnAnchorThatHoldsValueNeedsAKey) {
    Record r = ethereum();
    r.custody.clear();
    REQUIRE_ERR(r.valid(), Err::NoCustody);
}

// A record has to say which chain it is about, where to reach it, and on what
// basis it is believed.
TEST(ARecordSaysWhatItIsAbout) {
    {
        Record r = ethereum();
        r.chain_id = 0;
        REQUIRE_ERR(r.valid(), Err::NoChainId);
    }
    {
        Record r = ethereum();
        r.identity = kEmptyId;
        REQUIRE_ERR(r.valid(), Err::NoIdentity);
    }
    {
        Record r = ethereum();
        r.endpoints.clear();
        REQUIRE_ERR(r.valid(), Err::NoEndpoints);
    }
    {
        Record r = ethereum();
        r.anchor = static_cast<Anchor>(9);
        REQUIRE_ERR(r.valid(), Err::BadAnchor);
    }
    {
        Record r = ethereum();
        r.holding = static_cast<Holding>(9);
        REQUIRE_ERR(r.valid(), Err::BadHolding);
    }
    {
        Record r = ethereum();
        r.parent = r.identity;
        REQUIRE_ERR(r.valid(), Err::SelfParent);
    }
}

// Strengthening is free; weakening is a separate act, because it changes what
// every downstream consumer is trusting without any of them being asked.
TEST(AnAnchorStrengthensFreelyAndWeakensDeliberately) {
    Registry g;
    REQUIRE_OK(g.adopt(ethereum()));

    Record up = ethereum();
    up.anchor = Anchor::Proven;
    REQUIRE_OK(g.revise(up));

    Record down = up;
    down.anchor = Anchor::Attested;
    REQUIRE_ERR(g.revise(down), Err::WeakerAnchor);

    REQUIRE_OK(g.weaken(ethereum().key(), Anchor::Attested));
    REQUIRE(g.get(ethereum().key())->anchor == Anchor::Attested);
    // And weakening to what it already is, or to something stronger, is not a
    // weakening.
    REQUIRE_ERR(g.weaken(ethereum().key(), Anchor::Attested), Err::NotWeaker);
    REQUIRE_ERR(g.weaken(ethereum().key(), Anchor::Proven), Err::NotWeaker);

    // Dropping below the custodial line drops the key with it, or the record
    // would contradict itself.
    REQUIRE_OK(g.weaken(ethereum().key(), Anchor::Declared));
    REQUIRE(g.get(ethereum().key())->custody.empty());
    REQUIRE(!g.may_hold(ethereum().key()));

    // A network nobody adopted cannot be revised or weakened.
    REQUIRE_ERR(g.revise(bitcoin()), Err::NotAdopted);
    REQUIRE_ERR(g.weaken(bitcoin().key(), Anchor::Declared), Err::NotAdopted);
}

// A fork inherits the chain id of the chain it left, so the identity
// commitment is what keeps the two apart.
TEST(AForkDoesNotOverwriteTheChainItLeft) {
    Registry g;
    REQUIRE_OK(g.adopt(ethereum()));

    Record fork = ethereum();
    fork.identity = id_of(0xFF);  // the same chain id, a different chain
    REQUIRE_OK(g.adopt(fork));
    REQUIRE_EQ_NUM(2, g.size());
    REQUIRE_U64(ethereum().chain_id, g.get(fork.key())->chain_id);
}

// Where security comes from is not revisable. A network that changed it would
// have become something else.
TEST(ASecuritySourceIsNotRevisable) {
    Registry g;
    REQUIRE_OK(g.adopt(ethereum()));
    REQUIRE_OK(g.adopt(bitcoin()));
    REQUIRE_OK(g.adopt(base()));

    Record moved = base();
    moved.parent = bitcoin().key();
    REQUIRE_ERR(g.revise(moved), Err::SourceNotRevisable);
}

// An unadopted network is not bridgeable — the difference this register makes.
// Before it, a bridge trusted a file on disk and nothing said the estate had
// sanctioned it.
TEST(AnUnadoptedNetworkMayNotHoldValue) {
    Registry g;
    REQUIRE(!g.may_hold(id_of(42)));
    REQUIRE(!g.get(id_of(42)).has_value());

    // The positive control: an adopted, attested one may.
    REQUIRE_OK(g.adopt(ethereum()));
    REQUIRE(g.may_hold(ethereum().key()));
}

// Bitcoin has nowhere to put a gateway, so the custody address is the gateway.
// Adoption does not care; it is the same record read by a different client.
TEST(AChainWithNoContractsIsStillAdoptable) {
    Registry g;
    REQUIRE_OK(g.adopt(bitcoin()));
    REQUIRE(g.get(bitcoin().key())->holding == Holding::Address);
    REQUIRE(g.may_hold(bitcoin().key()));
    REQUIRE(g.get(bitcoin().key())->sovereign());
    REQUIRE(!base().sovereign());
}

// The names are part of the record: a log that cannot say what an anchor was
// cannot show anyone what changed.
TEST(TheAnchorsAndHoldingsAreNamed) {
    REQUIRE_EQ(std::string("declared"), std::string(anchor_name(Anchor::Declared)));
    REQUIRE_EQ(std::string("attested"), std::string(anchor_name(Anchor::Attested)));
    REQUIRE_EQ(std::string("proven"), std::string(anchor_name(Anchor::Proven)));
    REQUIRE_EQ(std::string("gateway"), std::string(holding_name(Holding::Gateway)));
    REQUIRE_EQ(std::string("address"), std::string(holding_name(Holding::Address)));
    REQUIRE(!custodial(Anchor::Declared));
    REQUIRE(custodial(Anchor::Attested));
    REQUIRE(custodial(Anchor::Proven));
}
