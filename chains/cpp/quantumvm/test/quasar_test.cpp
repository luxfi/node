// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// quasar_test.cpp — ported from Go chains/quantumvm/quasar_test.go.

#include "fixtures.hpp"

#include "lux/quantumvm/quasar.hpp"

#include <set>
#include <thread>
#include <vector>

using namespace qvmtest;
using namespace lux::quantumvm::quasar;

namespace {

struct Committee {
    std::shared_ptr<Quasar> q;
    Id block_id{};
    Bytes hash;
};

// A bridge whose core knows every member, plus the block they are all signing.
// Only a registered validator's signature can verify, so the roster is what
// decides whose statement counts.
Committee committee(const std::string& self, const std::vector<std::string>& peers) {
    Committee c;
    auto made = Quasar::make(Config{self, 1 + static_cast<int>(peers.size())});
    c.q = *made;
    for (const auto& peer : peers) (void)c.q->add_validator(peer, 100);

    c.block_id = random_id();
    c.hash.assign(64, 0);
    std::copy(c.block_id.begin(), c.block_id.end(), c.hash.begin());
    // The bridge starts tracking a block when THIS node signs it, which is what
    // production does; a peer signature then has somewhere to land.
    (void)c.q->sign_block(c.block_id, view(c.hash), 1);
    return c;
}

// A real signature from a committee member over a message.
QuasarSig sign_as(const Quasar& q, const std::string& validator_id, ByteView message) {
    auto sig = q.core()->sign(validator_id, message);
    return sig ? *sig : QuasarSig{};
}

}  // namespace

// TestThresholdFollowsTheCommittee.
//
// The threshold is derived, never configured, so the two cannot disagree — and
// the derivation has to land in the range that is actually a quorum. ⌊2n/3⌋+1
// equals n for every n below four, which is unanimity: zero Byzantine
// tolerance, one absent validator is a halt.
TEST(ThresholdFollowsTheCommittee) {
    for (int n : {1, 2, 3})
        REQUIRE_ERR(Quasar::make(Config{"self", n}), Err::CommitteeTooSmall);

    struct Case {
        int committee, threshold;
    };
    for (const Case& tc : {Case{4, 3}, Case{5, 4}, Case{7, 5}, Case{10, 7}, Case{100, 67}}) {
        auto q = Quasar::make(Config{"self", tc.committee});
        REQUIRE_OK(q);
        REQUIRE_EQ(tc.threshold, (*q)->threshold());
        REQUIRE_EQ(tc.committee, (*q)->committee());
        REQUIRE_MSG((*q)->threshold() < (*q)->committee(), "a quorum of everyone is not a quorum");
        REQUIRE((*q)->threshold() >= 2);
    }

    // An unset committee settles on the smallest that survives a fault.
    auto q = Quasar::make(Config{"self", 0});
    REQUIRE_OK(q);
    REQUIRE_EQ(config::kCommitteeMin, (*q)->committee());
    REQUIRE_EQ(config::quorum(config::kCommitteeMin), (*q)->threshold());
    REQUIRE_OK((*q)->add_validator("v1", 1));
}

// A signer with no name cannot be one of a number of distinct signers.
TEST(ABridgeWithNoIdentityIsRefused) {
    REQUIRE_ERR(Quasar::make(Config{"", 4}), Err::NoValidatorID);
}

// TestTheBridgeSignsForItself.
//
// The node was never registered with its own consensus core, so signing
// answered "validator not found" on every call — and everything downstream
// followed: the failure path deleted the pending entry, so a peer's signature
// arriving afterwards found no block to attach to, so the quorum was never
// reached and finality was unreachable.
TEST(TheBridgeSignsForItself) {
    auto made = Quasar::make(Config{"self", 4});
    REQUIRE_OK(made);
    auto q = *made;

    const Id block_id = random_id();
    const Bytes hash(block_id.begin(), block_id.end());
    auto sig = q->sign_block(block_id, view(hash), 1);
    REQUIRE_MSG(sig.has_value(), "the node cannot sign for its own identity");
    REQUIRE_EQ(std::string("self"), sig->validator_id);

    REQUIRE_MSG(q->tracking(block_id),
                "a signed block is not being tracked, so no peer signature can join it");
    REQUIRE_EQ(std::size_t{1}, q->signatures_of(block_id).size());

    // Signing it again is the same statement, not a second signer.
    auto again = q->sign_block(block_id, view(hash), 1);
    REQUIRE_OK(again);
    REQUIRE_MSG(*again == *sig, "signing twice produced two different statements");
    REQUIRE_EQ(std::size_t{1}, q->signatures_of(block_id).size());

    // And a peer's signature now finds the block and joins it.
    REQUIRE_OK(q->add_validator("peer", 1));
    const QuasarSig peer_sig = sign_as(*q, "peer", view(hash));
    REQUIRE_OK(q->add_signature(block_id, &peer_sig));
    REQUIRE_EQ(std::size_t{2}, q->signatures_of(block_id).size());
}

// TestOnlyAVerifiedSignatureCounts.
//
// The quorum was a count of STRINGS: a caller supplied a validator id and it was
// counted, unverified, against the threshold. So three fabricated ids finalized
// a block; five spellings of one name counted as five distinct signers of one
// signature; and a signature made for block A was accepted onto block B.
TEST(OnlyAVerifiedSignatureCounts) {
    Committee c = committee("validator-a", {"validator-b", "validator-c", "validator-d"});
    // The self-signature is already recorded; this test is about what ELSE may
    // join it.
    REQUIRE_EQ(std::size_t{1}, c.q->signatures_of(c.block_id).size());

    // Names nobody holds a key for.
    for (const char* name : {"ghost-1", "ghost-2", "ghost-3"}) {
        QuasarSig ghost;
        ghost.bls = bytes_of("not a signature");
        ghost.validator_id = name;
        REQUIRE_ERR(c.q->add_signature(c.block_id, &ghost), Err::UnverifiedSigner);
    }
    REQUIRE_EQ(std::size_t{1}, c.q->signatures_of(c.block_id).size());

    // One real signature, re-labelled every way a string can be respelled.
    const QuasarSig real = c.q->signatures_of(c.block_id)[0];
    for (const char* respelling : {"validator-a ", " validator-a", "Validator-A", "VALIDATOR-A",
                                   "validator-a\x01", "ｖａｌｉｄａｔｏｒ－ａ", "validator-a\n",
                                   "validator‑a"}) {
        QuasarSig relabelled = real;
        relabelled.validator_id = respelling;
        REQUIRE_ERR(c.q->add_signature(c.block_id, &relabelled), Err::UnverifiedSigner);
    }

    // The same name twice is one signer, however the bytes differ.
    REQUIRE_ERR(c.q->add_signature(c.block_id, &real), Err::DuplicateSigner);
    const QuasarSig again = sign_as(*c.q, "validator-a", view(c.hash));
    REQUIRE_ERR(c.q->add_signature(c.block_id, &again), Err::DuplicateSigner);
    REQUIRE_EQ(std::size_t{1}, c.q->signatures_of(c.block_id).size());

    // A signature for another block does not count for this one.
    const Id other = random_id();
    const Bytes other_hash(other.begin(), other.end());
    const QuasarSig elsewhere = sign_as(*c.q, "validator-b", view(other_hash));
    REQUIRE_ERR(c.q->add_signature(c.block_id, &elsewhere), Err::UnverifiedSigner);

    // Nor does no signature at all.
    REQUIRE_ERR(c.q->add_signature(c.block_id, nullptr), Err::UnverifiedSigner);
    REQUIRE_EQ(std::size_t{1}, c.q->signatures_of(c.block_id).size());
}

// TestFinalityNeedsAnAggregateThatVerifies.
//
// Reaching the count is necessary and not sufficient. The signatures are
// aggregated and the aggregate is checked against the committee's keys, so a
// block finalizes on cryptography rather than on arithmetic.
TEST(FinalityNeedsAnAggregateThatVerifies) {
    Committee c = committee("validator-a", {"validator-b", "validator-c", "validator-d"});
    REQUIRE_EQ(3, c.q->threshold());

    // Below the threshold nothing finalizes and nothing errors: it is simply not
    // time yet. (validator-a's own signature is already in.)
    const QuasarSig b = sign_as(*c.q, "validator-b", view(c.hash));
    REQUIRE_OK(c.q->add_signature(c.block_id, &b));
    auto pending = c.q->try_finalize(c.block_id);
    REQUIRE_OK(pending);
    REQUIRE_MSG(!pending->finalized, "two of three finalized a block");
    REQUIRE(!c.q->is_finalized(c.block_id));

    // The third carries it.
    const QuasarSig d = sign_as(*c.q, "validator-c", view(c.hash));
    REQUIRE_OK(c.q->add_signature(c.block_id, &d));
    auto done = c.q->try_finalize(c.block_id);
    REQUIRE_OK(done);
    REQUIRE_MSG(done->finalized, "a verified quorum did not finalize the block");
    REQUIRE(c.q->is_finalized(c.block_id));
    REQUIRE(c.q->verify_aggregate(view(c.hash), &done->aggregate));
    REQUIRE_MSG(!c.q->verify_aggregate(view(bytes_of("another block")), &done->aggregate),
                "the aggregate verified against a message it does not sign");

    // Cleanup below the finalized frontier releases it from both maps.
    c.q->cleanup(2);
    REQUIRE(!c.q->tracking(c.block_id));
    REQUIRE(!c.q->is_finalized(c.block_id));
}

// The set is verified on the way in, so bytes that never came from a signer
// never reach the threshold.
TEST(SignaturesThatAreNotSignaturesFinalizeNothing) {
    Committee c = committee("validator-a", {"validator-b", "validator-c", "validator-d"});

    for (const char* name : {"validator-b", "validator-c", "validator-d"}) {
        QuasarSig junk;
        junk.bls = bytes_of("not a signature");
        junk.validator_id = name;
        REQUIRE_ERR(c.q->add_signature(c.block_id, &junk), Err::UnverifiedSigner);
    }
    REQUIRE_EQ(std::size_t{1}, c.q->signatures_of(c.block_id).size());

    auto done = c.q->try_finalize(c.block_id);
    REQUIRE_OK(done);
    REQUIRE_MSG(!done->finalized, "three forged signatures reached the threshold and finalized");
    REQUIRE(!c.q->is_finalized(c.block_id));
}

// An id nobody proposed is not a place to accumulate signatures a peer chose the
// id of.
TEST(SignaturesForAnUnknownBlockAreRefused) {
    Committee c = committee("validator-a", {"validator-b", "validator-c", "validator-d"});
    const Id stranger = random_id();

    QuasarSig sig;
    sig.validator_id = "validator-b";
    REQUIRE_ERR(c.q->add_signature(stranger, &sig), Err::UnknownBlock);
    REQUIRE_ERR(c.q->try_finalize(stranger), Err::UnknownBlock);
}

// TestCleanupReleasesWhatWillNeverFinalize.
//
// minHeight is the caller's finalized frontier, so nothing below it can gather
// another signature. Releasing only the FINALIZED entries kept exactly the ones
// that accumulate — every proposal that lost, timed out or failed to sign.
TEST(CleanupReleasesWhatWillNeverFinalize) {
    auto made = Quasar::make(Config{"self", 4});
    REQUIRE_OK(made);
    auto q = *made;

    for (std::uint64_t h = 1; h <= 100; ++h) {
        const Id id = random_id();
        const Bytes hash(id.begin(), id.end());
        REQUIRE_OK(q->sign_block(id, view(hash), h));
    }
    REQUIRE_EQ(std::size_t{100}, q->tracked());

    q->cleanup(90);
    REQUIRE_MSG(q->tracked() == 11,
                "blocks below the finalized frontier were kept because they never finalized — "
                "which is why they pile up");
}

// TestSignBlockLeavesNothingBehindWhenItFails.
//
// Go creates the tracking entry BEFORE signing and deletes it again when the
// signature does not arrive; here a block becomes tracked exactly when it holds
// a verified signature, so the state this asserts against — tracked and empty —
// cannot be reached at all. That is the same property, made structural rather
// than repaired, and this is what pins it.
TEST(ABlockIsTrackedOnlyOnceItHoldsASignature) {
    auto made = Quasar::make(Config{"self", 4});
    REQUIRE_OK(made);
    auto q = *made;

    // A bridge that has signed nothing tracks nothing.
    REQUIRE_EQ(std::size_t{0}, q->tracked());

    // A signature for a block nobody proposed is refused, and creates no place
    // for a peer to accumulate signatures against an id it chose.
    const Id stranger = random_id();
    QuasarSig sig;
    sig.validator_id = "self";
    REQUIRE_ERR(q->add_signature(stranger, &sig), Err::UnknownBlock);
    REQUIRE_EQ(std::size_t{0}, q->tracked());

    // And a core that does not know the signer refuses rather than producing a
    // signature nobody can check — the failure the bridge's own registration
    // exists to prevent.
    auto bare = Core::make(3);
    REQUIRE_OK(bare);
    REQUIRE_ERR((*bare)->sign("self", view(bytes_of("x"))), Err::SignRefused);

    // Signing puts the block under tracking, with exactly one signature in it.
    const Id block_id = random_id();
    REQUIRE_OK(q->sign_block(block_id, view(bytes_of("hash")), 1));
    REQUIRE(q->tracking(block_id));
    REQUIRE_EQ(std::size_t{1}, q->signatures_of(block_id).size());
}

// Signing appends to the same set incoming peer signatures append to, so the two
// run against each other here.
TEST(SignBlockDoesNotRaceIncomingSignatures) {
    auto made = Quasar::make(Config{"self", 20});
    REQUIRE_OK(made);
    auto q = *made;

    const Id block_id = random_id();
    const Bytes hash(64, 0);

    std::vector<std::thread> threads;
    threads.emplace_back([&] { (void)q->sign_block(block_id, view(hash), 1); });
    for (int i = 0; i < 16; ++i) {
        threads.emplace_back([&, i] {
            QuasarSig sig;
            sig.bls = Bytes{static_cast<std::uint8_t>(i)};
            sig.validator_id = std::string(1, static_cast<char>('a' + i));
            (void)q->add_signature(block_id, &sig);
            (void)q->is_finalized(block_id);
            q->cleanup(0);
            (void)q->active_validators();
        });
    }
    for (auto& t : threads) t.join();
}

// The signer takes whatever the finality bridge supplies, which is not always
// block-hash shaped.
TEST(SignBlockAcceptsAnyMessageLength) {
    auto made = Quasar::make(Config{"self", 4});
    REQUIRE_OK(made);
    auto q = *made;

    for (std::size_t n : {std::size_t{0}, std::size_t{1}, std::size_t{31}, std::size_t{32},
                          std::size_t{64}}) {
        const Id block_id = random_id();
        REQUIRE_MSG(q->sign_block(block_id, view(Bytes(n, 0)), 1).has_value(),
                    "a " + std::to_string(n) + "-byte message was refused");
    }
}

// TestTheCommitteeIsASetOfADeclaredSize.
//
// Registering an id twice hands the core a FRESH key for it, which silently
// invalidates every signature that validator already contributed; registering
// more members than the committee declares makes the threshold a quorum of a
// committee that no longer exists.
TEST(TheCommitteeIsASetOfADeclaredSize) {
    auto made = Quasar::make(Config{"self", 4});
    REQUIRE_OK(made);
    auto q = *made;
    REQUIRE_MSG(q->active_validators() == 1, "the node is a member of its own committee");

    REQUIRE_OK(q->add_validator("validator-1", 100));
    REQUIRE_EQ(2, q->active_validators());

    REQUIRE_ERR(q->add_validator("validator-1", 100), Err::AlreadyRegistered);
    REQUIRE_ERR(q->add_validator("self", 1), Err::AlreadyRegistered);
    REQUIRE_MSG(q->active_validators() == 2,
                "one validator joining twice moved the count the threshold is measured against");

    REQUIRE_OK(q->add_validator("validator-2", 100));
    REQUIRE_OK(q->add_validator("validator-3", 100));
    REQUIRE_ERR(q->add_validator("validator-4", 100), Err::CommitteeFull);
    REQUIRE_ERR(q->add_validator("", 100), Err::NoValidatorID);
    REQUIRE_EQ(4, q->active_validators());
    REQUIRE(q->core() != nullptr);
}

// The bridge's own verification helpers agree with what add_signature admits, so
// nothing can be verified one way in one place and another way in another.
TEST(ARegisteredKeyIsWhatVerifies) {
    Committee c = committee("validator-a", {"validator-b", "validator-c", "validator-d"});

    const QuasarSig sig = sign_as(*c.q, "validator-b", view(c.hash));
    REQUIRE(c.q->verify_signature(view(c.hash), &sig));
    REQUIRE(!c.q->verify_signature(view(bytes_of("another message")), &sig));
    REQUIRE(!c.q->verify_signature(view(c.hash), nullptr));
    REQUIRE(!c.q->verify_aggregate(view(c.hash), nullptr));

    QuasarSig relabelled = sig;
    relabelled.validator_id = "validator-c";
    REQUIRE_MSG(!c.q->verify_signature(view(c.hash), &relabelled),
                "a signature relabelled to another committee member verified");
}

// An aggregate that names one validator twice must not clear a threshold it
// cannot actually reach: the count is over DISTINCT registered signers.
TEST(AnAggregateCannotCountOneSignerTwice) {
    Committee c = committee("validator-a", {"validator-b", "validator-c", "validator-d"});

    const QuasarSig b = sign_as(*c.q, "validator-b", view(c.hash));
    REQUIRE_OK(c.q->add_signature(c.block_id, &b));
    const QuasarSig d = sign_as(*c.q, "validator-c", view(c.hash));
    REQUIRE_OK(c.q->add_signature(c.block_id, &d));

    auto done = c.q->try_finalize(c.block_id);
    REQUIRE_OK(done);
    REQUIRE(done->finalized);

    AggregatedSignature padded = done->aggregate;
    padded.validator_ids.push_back(padded.validator_ids.front());
    padded.signer_count = static_cast<int>(padded.validator_ids.size());
    REQUIRE_MSG(!c.q->verify_aggregate(view(c.hash), &padded),
                "one signer counted twice cleared the threshold");
}

// The quorum is the number of DISTINCT registered validators whose keys went
// into the sum — never the count the sender wrote down beside it.
//
// One validator's own signature, under its own name, with a count of a hundred
// typed next to it: every field is genuine except the number, and the number was
// the only thing being checked. It cleared the threshold and finalized a block
// alone. Nothing is forged here — that is the point. The signature verifies,
// because it really is validator-a signing this block; what must fail is that
// ONE of them is not THREE of them.
TEST(TheQuorumIsCountedOverTheKeysThatWereChecked) {
    Committee c = committee("validator-a", {"validator-b", "validator-c", "validator-d"});
    REQUIRE_EQ(3, c.q->threshold());

    const QuasarSig a = sign_as(*c.q, "validator-a", view(c.hash));
    REQUIRE_MSG(c.q->verify_signature(view(c.hash), &a),
                "the single signature this is built from is not real, so the test proves nothing");

    AggregatedSignature alone;
    alone.bls_aggregated = a.bls;
    alone.validator_ids = {"validator-a"};
    alone.signer_count = 100;
    REQUIRE_MSG(!c.q->verify_aggregate(view(c.hash), &alone),
                "one validator claiming to be a hundred cleared a threshold of three");

    // The same aggregate told the truth about itself is still one signer.
    alone.signer_count = 1;
    REQUIRE(!c.q->verify_aggregate(view(c.hash), &alone));

    // And a quorum that IS a quorum still verifies, so this refuses the right
    // thing rather than everything.
    const QuasarSig b = sign_as(*c.q, "validator-b", view(c.hash));
    const QuasarSig d = sign_as(*c.q, "validator-c", view(c.hash));
    auto real = c.q->core()->aggregate(view(c.hash), {a, b, d});
    REQUIRE_OK(real);
    REQUIRE_MSG(c.q->verify_aggregate(view(c.hash), &*real),
                "three distinct signers did not verify");
}

// A sender does not choose which verification its own aggregate gets.
//
// This core holds per-validator keys and no threshold group key, so it can check
// a sum of committee keys and nothing else. Go's signer on this path never
// builds a threshold verifier either, so it answers false for an aggregate
// flagged as a threshold signature — and an aggregate that verifies in one
// language and not the other is a split, whichever way it falls.
TEST(AFlagOnTheAggregateCannotChooseItsVerification) {
    Committee c = committee("validator-a", {"validator-b", "validator-c", "validator-d"});

    const QuasarSig a = sign_as(*c.q, "validator-a", view(c.hash));
    const QuasarSig b = sign_as(*c.q, "validator-b", view(c.hash));
    const QuasarSig d = sign_as(*c.q, "validator-c", view(c.hash));

    auto real = c.q->core()->aggregate(view(c.hash), {a, b, d});
    REQUIRE_OK(real);
    REQUIRE(c.q->verify_aggregate(view(c.hash), &*real));

    AggregatedSignature relabelled = *real;
    relabelled.is_threshold = true;
    REQUIRE_MSG(!c.q->verify_aggregate(view(c.hash), &relabelled),
                "an aggregate took a verification path this core cannot perform");
}

// TestOneSignerCannotSpendItsOwnSignatureTwice.
//
// BLS is linear, and that is the whole attack: aggregating a validator's own
// signature t times yields t·σ, and summing its key t times yields t·pk, and
// e(t·σ, g) = e(H(m), t·pk) — so the pairing HOLDS. One validator produces an
// aggregate that is cryptographically valid for a committee of t. Counting the
// repeated name is what refuses it.
//
// The second half is what pins the rule on its own: three DISTINCT signers plus
// one of them again. The distinct count is a full quorum, so the quorum check
// passes it; the sum carries validator-a's key twice and the aggregate carries
// its signature twice, so the pairing holds too. Only the repeat itself is left
// to catch it — remove that one line and this verifies.
TEST(OneSignerCannotSpendItsOwnSignatureTwice) {
    Committee c = committee("validator-a", {"validator-b", "validator-c", "validator-d"});
    REQUIRE_EQ(3, c.q->threshold());

    const QuasarSig a = sign_as(*c.q, "validator-a", view(c.hash));
    const QuasarSig b = sign_as(*c.q, "validator-b", view(c.hash));
    const QuasarSig d = sign_as(*c.q, "validator-c", view(c.hash));

    // t copies of one validator: t·σ against t·pk, which is arithmetically sound
    // and consensually meaningless.
    auto scaled = c.q->core()->aggregate(view(c.hash), {a, a, a});
    REQUIRE_OK(scaled);
    REQUIRE_EQ(3, scaled->signer_count);
    REQUIRE_EQ(std::size_t{3}, scaled->validator_ids.size());
    REQUIRE_MSG(!c.q->verify_aggregate(view(c.hash), &*scaled),
                "one validator signed a block three times and finalized it");

    // A full quorum of distinct signers, with one of them named once more. The
    // count is satisfied and the pairing is satisfied; the repeat is the only
    // thing wrong with it.
    auto repeated = c.q->core()->aggregate(view(c.hash), {a, b, d, a});
    REQUIRE_OK(repeated);
    REQUIRE_EQ(std::size_t{4}, repeated->validator_ids.size());

    std::set<std::string> distinct(repeated->validator_ids.begin(),
                                   repeated->validator_ids.end());
    REQUIRE_MSG(static_cast<int>(distinct.size()) >= c.q->threshold(),
                "this case has to clear the quorum on distinct signers, or it is testing the "
                "quorum rule instead of the repeat rule");

    REQUIRE_MSG(!c.q->verify_aggregate(view(c.hash), &*repeated),
                "a quorum that names one of its members twice was accepted");
}

// Each validator signature is BOTH a BLS signature and an ML-DSA attestation, so
// a signature whose post-quantum half is wrong is not a statement at all.
TEST(BothHalvesOfASignatureAreChecked) {
    Committee c = committee("validator-a", {"validator-b", "validator-c", "validator-d"});
    QuasarSig sig = sign_as(*c.q, "validator-b", view(c.hash));
    REQUIRE_MSG(!sig.mldsa.empty(), "the post-quantum half is missing from a validator signature");
    REQUIRE(c.q->verify_signature(view(c.hash), &sig));

    QuasarSig tampered = sig;
    tampered.mldsa[0] ^= 0xFF;
    REQUIRE_MSG(!c.q->verify_signature(view(c.hash), &tampered),
                "a signature whose ML-DSA half does not check was accepted");

    QuasarSig bls_broken = sig;
    bls_broken.bls[0] ^= 0xFF;
    REQUIRE(!c.q->verify_signature(view(c.hash), &bls_broken));
}
