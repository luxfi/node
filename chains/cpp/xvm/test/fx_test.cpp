// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fx_test.cpp — the three fx families' spend and operation gates, ported from
// secp256k1fx/fx_test.go, nftfx/fx_test.go and propertyfx/fx_test.go.
//
// The first case is a cross-language KAT and the reason the rest can be
// trusted: Go's fixture carries a fixed unsigned-tx body {0,1,2,3,4,5}, a fixed
// 65-byte signature, and the address that signature belongs to. Recovering that
// address here, from those exact bytes, is what proves this port's hash, its
// recovery and its address derivation are the SAME three functions Go's are —
// every later "the signature is accepted" case rests on it.

#include "check.hpp"
#include "keys.hpp"

#include "lux/xvm/fx.hpp"

using namespace lux::xvm;
using namespace lux::xvm::test;

namespace {

// ---- the Go fixture, verbatim (secp256k1fx/fx_test.go) ----

Bytes go_tx_bytes() { return Bytes{0, 1, 2, 3, 4, 5}; }

fx::Signature go_sig() {
    return fx::Signature{0x0e, 0x33, 0x4e, 0xbc, 0x67, 0xa7, 0x3f, 0xe8, 0x24, 0x33, 0xac, 0xa3,
                         0x47, 0x88, 0xa6, 0x3d, 0x58, 0xe5, 0x8e, 0xf0, 0x3a, 0xd5, 0x84, 0xf1,
                         0xbc, 0xa3, 0xb2, 0xd2, 0x5d, 0x51, 0xd6, 0x9b, 0x0f, 0x28, 0x5d, 0xcd,
                         0x3f, 0x71, 0x17, 0x0a, 0xf9, 0xbf, 0x2d, 0xb1, 0x10, 0x26, 0x5c, 0xe9,
                         0xdc, 0xc3, 0x9d, 0x7a, 0x01, 0x50, 0x9d, 0xe8, 0x35, 0xbd, 0xcb, 0x29,
                         0x3a, 0xd1, 0x49, 0x32, 0x00};
}

ShortId go_addr() {
    return ShortId{0x01, 0x5c, 0xce, 0x6c, 0x55, 0xd6, 0xb5, 0x09, 0x84, 0x5c,
                   0x8c, 0x4e, 0x30, 0xbe, 0xd9, 0x8d, 0x39, 0x1a, 0xe7, 0xf0};
}

// ---- the second signer, derived rather than transcribed ----
//
// Go's addr2/sig2Bytes come from a cb58-encoded key its test decodes at init.
// Deriving a second signer from this port's own test keys asks the same
// question — two distinct signers, two distinct addresses — without importing
// a second encoding just to read one constant.

ShortId addr2() { return test_address(1); }
fx::Signature sig2() { return sign_unsigned_tx(test_key(1), view(go_tx_bytes())); }

fx::OutputOwners owners(std::uint64_t locktime, std::uint32_t threshold,
                        std::vector<ShortId> addrs) {
    return fx::OutputOwners{locktime, threshold, std::move(addrs)};
}

std::shared_ptr<fx::secp256k1fx::Credential> cred(std::vector<fx::Signature> sigs) {
    auto c = std::make_shared<fx::secp256k1fx::Credential>();
    c->signatures = std::move(sigs);
    return c;
}

std::shared_ptr<fx::secp256k1fx::TransferOutput> tout(std::uint64_t amt,
                                                      fx::OutputOwners o) {
    auto out = std::make_shared<fx::secp256k1fx::TransferOutput>();
    out->amt = amt;
    out->out_owners = std::move(o);
    return out;
}

std::shared_ptr<fx::secp256k1fx::TransferInput> tin(std::uint64_t amt,
                                                    std::vector<std::uint32_t> idx) {
    auto in = std::make_shared<fx::secp256k1fx::TransferInput>();
    in->amt = amt;
    in->input.sig_indices = std::move(idx);
    return in;
}

// A bootstrapped fx whose clock reads `now`. Signature verification is only on
// once bootstrapped — that is the whole point of the flag — so every case that
// asserts something about a signature runs on one of these.
struct Rig {
    fx::Clock clock;
    fx::Secp256k1Fx secp;
    fx::NFTFx nft;
    fx::PropertyFx property;

    explicit Rig(std::uint64_t now = 1547915117) {
        clock.unix_time = now;
        for (fx::Fx* f : {static_cast<fx::Fx*>(&secp), static_cast<fx::Fx*>(&nft),
                          static_cast<fx::Fx*>(&property)}) {
            f->clock = &clock;
            f->bootstrapping();
            f->bootstrapped();
        }
    }
};

void expect(const wire::Result<void>& r, const std::string& want, const std::string& what) {
    bool ok = want.empty() ? r.has_value() : (!r && r.error().find(want) != std::string::npos);
    check(ok, what);
    if (!ok) {
        std::printf("        got  \"%s\"\n        want \"%s\"\n",
                    r ? "" : r.error().c_str(), want.c_str());
    }
}

// ================= the cross-language KAT =================

void go_signature_kat() {
    std::printf("\n  -- the Go signature fixture --\n");

    // Go's Fx.VerifyCredentials hashes the unsigned bytes with sha256 and
    // recovers the signer from THAT hash. If any of the three steps differed,
    // this would not land on Go's address.
    Rig rig;
    auto out = tout(1, owners(0, 1, {go_addr()}));
    auto in = tin(1, {0});
    auto c = cred({go_sig()});
    expect(rig.secp.verify_transfer(view(go_tx_bytes()), in.get(), c.get(), out.get()), "",
           "Go's signature recovers to Go's address over Go's tx bytes");

    // …and it is that address specifically, not merely some address.
    auto wrong = tout(1, owners(0, 1, {addr2()}));
    expect(rig.secp.verify_transfer(view(go_tx_bytes()), in.get(), c.get(), wrong.get()),
           fx::kErrWrongSig, "…and no other address");

    // The keys this port signs with derive addresses the same way.
    check(test_address(0) != test_address(1), "two test keys give two addresses");
    auto own = tout(1, owners(0, 1, {test_address(1)}));
    auto own_cred = cred({sig2()});
    expect(rig.secp.verify_transfer(view(go_tx_bytes()), in.get(), own_cred.get(), own.get()), "",
           "a signature this port made verifies against the address it derives");
}

// ================= the recovery id, against both other languages =================
//
// The last byte of a signature is a recovery id, and what a chain does with the
// four values it can take is consensus: a transaction one language accepts and
// another refuses is a fork. The Go fixture above is the one signature all three
// languages already share, so the table below is that signature read back at
// every recovery id by luxfi/crypto (Go) and k256 (Rust), run on those bytes
// rather than reasoned about:
//
//     v   Go                     Rust                   what it means
//     0   015cce…e7f0            015cce…e7f0            the signer
//     1   aaabc7…8271            aaabc7…8271            the other candidate y
//     2   recovery failed        recovery failed        x wrapped the order
//     3   recovery failed        recovery failed        x wrapped, odd y
//     4   invalid recovery id    invalid recovery id    not a recovery id
//
// 2 and 3 name the recovery whose R has x = r + n. Producing one needs
// r < p - n, about 2^128 of work, so no signature that exists takes those
// values and both references fail them. A port that masked the byte down to its
// low bit would instead read 2 as 0 and hand back the signer — accepting, off a
// single flipped byte, a transaction the rest of the network refuses.
//
// The address at v=1 is asserted too, and not only the refusals: it is what
// proves the id is being USED rather than ignored, so that "2 is refused"
// cannot be passing for the wrong reason.

ShortId recovery_addr_v1() {
    return ShortId{0xaa, 0xab, 0xc7, 0x96, 0x32, 0xce, 0x64, 0x53, 0x7f, 0x91,
                   0x65, 0xed, 0x52, 0x33, 0x10, 0x70, 0xd8, 0x03, 0x82, 0x71};
}

// with_recovery_id is the whole attack: one byte of a valid credential.
fx::Signature with_recovery_id(fx::Signature sig, std::uint8_t v) {
    sig[64] = v;
    return sig;
}

void recovery_id_matches_the_reference() {
    std::printf("\n  -- the recovery id, against Go and Rust --\n");

    Rig rig;
    const auto bytes = go_tx_bytes();
    auto in = tin(1, {0});
    auto to_signer = tout(1, owners(0, 1, {go_addr()}));
    auto to_other = tout(1, owners(0, 1, {recovery_addr_v1()}));

    // v = 0: the signer, as both references read it.
    auto c0 = cred({with_recovery_id(go_sig(), 0)});
    expect(rig.secp.verify_transfer(view(bytes), in.get(), c0.get(), to_signer.get()), "",
           "v=0 recovers the signer");

    // v = 1: the OTHER candidate — a different address, so the id is read.
    auto c1 = cred({with_recovery_id(go_sig(), 1)});
    expect(rig.secp.verify_transfer(view(bytes), in.get(), c1.get(), to_other.get()), "",
           "v=1 recovers the other candidate");
    expect(rig.secp.verify_transfer(view(bytes), in.get(), c1.get(), to_signer.get()),
           fx::kErrWrongSig, "…and v=1 is therefore not the signer");

    // v = 2 and 3: the wrapped-x recovery. Both references fail it, so this one
    // must too — and it must fail as a RECOVERY, not merely land on some other
    // address, or a signature would be one collision away from spending.
    for (std::uint8_t v : {std::uint8_t(2), std::uint8_t(3)}) {
        auto c = cred({with_recovery_id(go_sig(), v)});
        const std::string at = " (v=" + std::to_string(v) + ")";
        expect(rig.secp.verify_transfer(view(bytes), in.get(), c.get(), to_signer.get()),
               "recovery failed", "a wrapped-x recovery id does not spend the signer's output" + at);
        expect(rig.secp.verify_transfer(view(bytes), in.get(), c.get(), to_other.get()),
               "recovery failed", "…nor the other candidate's" + at);
    }

    // 4 and up are not recovery ids at all, and both references say so by name.
    for (std::uint8_t v : {std::uint8_t(4), std::uint8_t(27), std::uint8_t(255)}) {
        auto c = cred({with_recovery_id(go_sig(), v)});
        expect(rig.secp.verify_transfer(view(bytes), in.get(), c.get(), to_signer.get()),
               "invalid signature recovery id",
               "v=" + std::to_string(v) + " is not a recovery id");
    }
}

// ================= secp256k1fx VerifyTransfer =================

void secp_verify_transfer() {
    std::printf("\n  -- secp256k1fx VerifyTransfer --\n");
    Rig rig;
    const auto bytes = go_tx_bytes();
    auto good_out = tout(1, owners(0, 1, {go_addr()}));
    auto good_in = tin(1, {0});
    auto good_cred = cred({go_sig()});

    expect(rig.secp.verify_transfer(view(bytes), good_in.get(), good_cred.get(), good_out.get()),
           "", "valid");

    expect(rig.secp.verify_transfer(view(bytes), nullptr, good_cred.get(), good_out.get()),
           fx::kErrWrongInputType, "nil input");
    expect(rig.secp.verify_transfer(view(bytes), good_in.get(), nullptr, good_out.get()),
           fx::kErrWrongCredentialType, "nil credential");
    expect(rig.secp.verify_transfer(view(bytes), good_in.get(), good_cred.get(), nullptr),
           fx::kErrWrongUTXOType, "nil output");
    {
        // A value output of another family is not this family's output.
        auto nft_out = std::make_shared<fx::nftfx::TransferOutput>();
        expect(rig.secp.verify_transfer(view(bytes), good_in.get(), good_cred.get(), nft_out.get()),
               fx::kErrWrongUTXOType, "an nftfx output is the wrong utxo type");
    }
    {
        auto bad = tout(1, owners(0, 2, {go_addr()}));  // threshold above addr count
        expect(rig.secp.verify_transfer(view(bytes), good_in.get(), good_cred.get(), bad.get()),
               fx::kErrOutputUnspendable, "invalid output");
    }
    {
        auto in2 = tin(2, {0});
        expect(rig.secp.verify_transfer(view(bytes), in2.get(), good_cred.get(), good_out.get()),
               fx::kErrMismatchedAmounts, "wrong amounts");
    }
    {
        auto locked = tout(1, owners(rig.clock.unix() + 1, 1, {go_addr()}));
        expect(rig.secp.verify_transfer(view(bytes), good_in.get(), good_cred.get(), locked.get()),
               fx::kErrTimelocked, "timelocked");
    }
    {
        auto many = tin(1, {0, 1});
        auto c2 = cred({go_sig(), sig2()});
        expect(rig.secp.verify_transfer(view(bytes), many.get(), c2.get(), good_out.get()),
               fx::kErrTooManySigners, "too many signers");
    }
    {
        auto few = tin(1, {});
        auto c0 = cred({});
        expect(rig.secp.verify_transfer(view(bytes), few.get(), c0.get(), good_out.get()),
               fx::kErrTooFewSigners, "too few signers");
    }
    {
        auto c2 = cred({go_sig(), sig2()});
        expect(rig.secp.verify_transfer(view(bytes), good_in.get(), c2.get(), good_out.get()),
               fx::kErrInputCredentialSignersMismatch, "mismatched signers");
    }
    {
        // An all-zero signature cannot be recovered from at all.
        auto zero = cred({fx::Signature{}});
        expect(rig.secp.verify_transfer(view(bytes), good_in.get(), zero.get(), good_out.get()),
               "recovery failed", "invalid signature");

        // …but during bootstrapping it is not even looked at, because history
        // is being replayed and was verified when it was first accepted.
        fx::Clock clk{rig.clock.unix_time};
        fx::Secp256k1Fx booting;
        booting.clock = &clk;
        booting.bootstrapping();
        expect(booting.verify_transfer(view(bytes), good_in.get(), zero.get(), good_out.get()), "",
               "…and is not checked while bootstrapping");
    }
    {
        auto other = tout(1, owners(0, 1, {addr2()}));
        expect(rig.secp.verify_transfer(view(bytes), good_in.get(), good_cred.get(), other.get()),
               fx::kErrWrongSig, "wrong signer");
    }
    {
        auto oob = tin(1, {1});
        expect(rig.secp.verify_transfer(view(bytes), oob.get(), good_cred.get(), good_out.get()),
               fx::kErrInputOutputIndexOutOfBounds, "sig index out of bounds");
    }
}

// ================= secp256k1fx VerifyOperation =================

std::shared_ptr<fx::secp256k1fx::MintOperation> mint_op(fx::OutputOwners mint_owner,
                                                        std::vector<std::uint32_t> idx) {
    auto op = std::make_shared<fx::secp256k1fx::MintOperation>();
    op->mint_input.sig_indices = std::move(idx);
    op->mint_output.out_owners = std::move(mint_owner);
    op->transfer_output.amt = 1;
    op->transfer_output.out_owners = owners(0, 1, {go_addr()});
    return op;
}

void secp_verify_operation() {
    std::printf("\n  -- secp256k1fx VerifyOperation --\n");
    Rig rig;
    const auto bytes = go_tx_bytes();

    auto utxo = std::make_shared<fx::secp256k1fx::MintOutput>();
    utxo->out_owners = owners(0, 1, {go_addr()});
    auto op = mint_op(owners(0, 1, {go_addr()}), {0});
    auto c = cred({go_sig()});

    expect(rig.secp.verify_operation(view(bytes), op.get(), c.get(), {utxo.get()}), "", "valid");
    expect(rig.secp.verify_operation(view(bytes), nullptr, c.get(), {utxo.get()}),
           fx::kErrWrongOpType, "unknown operation");
    expect(rig.secp.verify_operation(view(bytes), op.get(), nullptr, {utxo.get()}),
           fx::kErrWrongCredentialType, "unknown credential");
    expect(rig.secp.verify_operation(view(bytes), op.get(), c.get(), {utxo.get(), utxo.get()}),
           fx::kErrWrongNumberOfUTXOs, "wrong number of utxos");
    {
        auto not_mint = tout(1, owners(0, 1, {go_addr()}));
        expect(rig.secp.verify_operation(view(bytes), op.get(), c.get(), {not_mint.get()}),
               fx::kErrWrongUTXOType, "unknown utxo type");
    }
    {
        auto bad = mint_op(owners(0, 2, {go_addr()}), {0});  // unspendable mint output
        expect(rig.secp.verify_operation(view(bytes), bad.get(), c.get(), {utxo.get()}),
               fx::kErrOutputUnspendable, "invalid operation verify");
    }
    {
        // Go's TestFxVerifyOperationMismatchedMintOutputs: the operation must
        // re-create the SAME mint authority it consumed, or minting would let a
        // holder rewrite who may mint next.
        auto bad = mint_op(fx::OutputOwners{}, {0});
        expect(rig.secp.verify_operation(view(bytes), bad.get(), c.get(), {utxo.get()}),
               fx::kErrWrongMintCreated, "mismatched mint outputs");
    }
}

// ================= secp256k1fx VerifyPermission =================

void secp_verify_permission() {
    std::printf("\n  -- secp256k1fx VerifyPermission --\n");
    Rig rig;
    const auto bytes = go_tx_bytes();
    const std::uint64_t now = rig.clock.unix();

    struct Case {
        const char* name;
        std::vector<std::uint32_t> indices;
        std::vector<fx::Signature> sigs;
        fx::OutputOwners owner;
        std::string want;
    };

    const std::vector<Case> cases = {
        {"threshold 0, no sigs, has addrs", {}, {}, owners(0, 0, {go_addr()}),
         fx::kErrOutputUnoptimized},
        {"threshold 0, no sigs, no addrs", {}, {}, owners(0, 0, {}), ""},
        {"threshold 1, 1 sig", {0}, {go_sig()}, owners(0, 1, {go_addr()}), ""},
        {"threshold 0, 1 sig (too many sigs)", {0}, {go_sig()}, owners(0, 0, {go_addr()}),
         fx::kErrOutputUnoptimized},
        {"threshold 1, 0 sigs (too few sigs)", {}, {}, owners(0, 1, {go_addr()}),
         fx::kErrTooFewSigners},
        {"threshold 1, 1 incorrect sig", {0}, {go_sig()}, owners(0, 1, {addr2()}),
         fx::kErrWrongSig},
        {"repeated sig", {0, 0}, {go_sig(), go_sig()}, owners(0, 2, {go_addr(), addr2()}),
         fx::kErrInputIndicesNotSortedUnique},
        {"threshold 2, repeated address and repeated sig", {0, 1}, {go_sig(), go_sig()},
         owners(0, 2, {go_addr(), go_addr()}), fx::kErrAddrsNotSortedUnique},
        {"threshold 2, 2 sigs", {0, 1}, {go_sig(), sig2()}, owners(0, 2, {go_addr(), addr2()}),
         ""},
        {"threshold 2, 2 sigs reversed (should be sorted)", {1, 0}, {sig2(), go_sig()},
         owners(0, 2, {go_addr(), addr2()}), fx::kErrInputIndicesNotSortedUnique},
        {"threshold 1, 1 sig, index out of bounds", {1}, {go_sig()}, owners(0, 1, {go_addr()}),
         fx::kErrInputOutputIndexOutOfBounds},
        {"too many signers", {0, 1}, {go_sig(), sig2()}, owners(0, 1, {go_addr(), addr2()}),
         fx::kErrTooManySigners},
        {"number of signatures doesn't match", {0}, {go_sig(), sig2()},
         owners(0, 1, {go_addr(), addr2()}), fx::kErrInputCredentialSignersMismatch},
        {"output is locked", {0}, {go_sig(), sig2()},
         owners(now + 1, 1, {go_addr(), addr2()}), fx::kErrTimelocked},
    };

    for (const auto& t : cases) {
        fx::Input in;
        in.sig_indices = t.indices;
        auto c = cred(t.sigs);
        // The owner list Go writes is the list as given; sorting it here would
        // hide exactly the case that asserts an unsorted list is refused.
        fx::OutputOwners owner = t.owner;
        expect(rig.secp.verify_permission(view(bytes), in, c.get(), owner), t.want, t.name);
    }
}

// ================= nftfx =================

void nft_cases() {
    std::printf("\n  -- nftfx --\n");
    Rig rig;
    const auto bytes = go_tx_bytes();

    auto c = std::make_shared<fx::nftfx::Credential>();
    c->signatures = {go_sig()};

    auto mint_utxo = std::make_shared<fx::nftfx::MintOutput>();
    mint_utxo->group_id = 1;
    mint_utxo->out_owners = owners(0, 1, {go_addr()});

    auto mop = std::make_shared<fx::nftfx::MintOperation>();
    mop->mint_input.sig_indices = {0};
    mop->group_id = 1;
    mop->payload = Bytes{'h', 'e', 'l', 'l', 'o'};
    mop->outputs = {std::make_shared<fx::OutputOwners>(owners(0, 1, {go_addr()}))};

    expect(rig.nft.verify_operation(view(bytes), mop.get(), c.get(), {mint_utxo.get()}), "",
           "mint operation");
    expect(rig.nft.verify_operation(view(bytes), mop.get(), nullptr, {mint_utxo.get()}),
           fx::kErrWrongCredentialType, "mint operation, wrong credential");
    expect(rig.nft.verify_operation(view(bytes), mop.get(), c.get(),
                                    {mint_utxo.get(), mint_utxo.get()}),
           fx::kErrWrongNumberOfUTXOs, "mint operation, wrong number of utxos");
    {
        auto not_nft = tout(1, owners(0, 1, {go_addr()}));
        expect(rig.nft.verify_operation(view(bytes), mop.get(), c.get(), {not_nft.get()}),
               fx::kErrWrongUTXOType, "mint operation, invalid utxo");
    }
    {
        auto bad = std::make_shared<fx::nftfx::MintOperation>(*mop);
        bad->outputs = {std::make_shared<fx::OutputOwners>(owners(0, 2, {go_addr()}))};
        expect(rig.nft.verify_operation(view(bytes), bad.get(), c.get(), {mint_utxo.get()}),
               fx::kErrOutputUnspendable, "mint operation, failing verification");
    }
    {
        auto bad = std::make_shared<fx::nftfx::MintOperation>(*mop);
        bad->group_id = 2;
        expect(rig.nft.verify_operation(view(bytes), bad.get(), c.get(), {mint_utxo.get()}),
               fx::kErrWrongUniqueID, "mint operation, invalid group id");
    }

    auto xfer_utxo = std::make_shared<fx::nftfx::TransferOutput>();
    xfer_utxo->group_id = 1;
    xfer_utxo->payload = Bytes{'h', 'e', 'l', 'l', 'o'};
    xfer_utxo->out_owners = owners(0, 1, {go_addr()});

    auto top = std::make_shared<fx::nftfx::TransferOperation>();
    top->input.sig_indices = {0};
    top->output.group_id = 1;
    top->output.payload = Bytes{'h', 'e', 'l', 'l', 'o'};
    top->output.out_owners = owners(0, 1, {addr2()});

    expect(rig.nft.verify_operation(view(bytes), top.get(), c.get(), {xfer_utxo.get()}), "",
           "transfer operation");
    {
        auto not_nft = tout(1, owners(0, 1, {go_addr()}));
        expect(rig.nft.verify_operation(view(bytes), top.get(), c.get(), {not_nft.get()}),
               fx::kErrWrongUTXOType, "transfer operation, wrong utxo");
    }
    {
        auto bad = std::make_shared<fx::nftfx::TransferOperation>(*top);
        bad->output.out_owners = owners(0, 2, {addr2()});
        expect(rig.nft.verify_operation(view(bytes), bad.get(), c.get(), {xfer_utxo.get()}),
               fx::kErrOutputUnspendable, "transfer operation, failed verify");
    }
    {
        auto bad = std::make_shared<fx::nftfx::TransferOperation>(*top);
        bad->output.group_id = 2;
        expect(rig.nft.verify_operation(view(bytes), bad.get(), c.get(), {xfer_utxo.get()}),
               fx::kErrWrongUniqueID, "transfer operation, wrong group id");
    }
    {
        auto bad = std::make_shared<fx::nftfx::TransferOperation>(*top);
        bad->output.payload = Bytes{'w', 'o', 'r', 'l', 'd'};
        expect(rig.nft.verify_operation(view(bytes), bad.get(), c.get(), {xfer_utxo.get()}),
               "wrong bytes provided", "transfer operation, wrong bytes");
    }
    {
        // Go's TestFxVerifyTransferOperationTooSoon: the CONSUMED output's
        // locktime is what gates the spend, not the produced one's.
        auto locked = std::make_shared<fx::nftfx::TransferOutput>(*xfer_utxo);
        locked->out_owners = owners(rig.clock.unix() + 1, 1, {go_addr()});
        expect(rig.nft.verify_operation(view(bytes), top.get(), c.get(), {locked.get()}),
               fx::kErrTimelocked, "transfer operation, too soon");
    }
    {
        auto burn = std::make_shared<fx::propertyfx::BurnOperation>();
        burn->input.sig_indices = {0};
        expect(rig.nft.verify_operation(view(bytes), burn.get(), c.get(), {xfer_utxo.get()}),
               fx::kErrWrongOpType, "unknown operation");
    }
    {
        auto in = tin(1, {0});
        auto out = tout(1, owners(0, 1, {go_addr()}));
        expect(rig.nft.verify_transfer(view(bytes), in.get(), c.get(), out.get()),
               "cant transfer with this fx", "an nft cannot be transferred as value");
    }
    {
        // The payload cap is a real gate, not a comment.
        auto big = std::make_shared<fx::nftfx::TransferOutput>(*xfer_utxo);
        big->payload.assign(fx::kMaxPayloadSize + 1, 0);
        expect(big->verify(), fx::kErrPayloadTooLarge, "an oversize payload is refused");
    }
}

// ================= propertyfx =================

void property_cases() {
    std::printf("\n  -- propertyfx --\n");
    Rig rig;
    const auto bytes = go_tx_bytes();

    auto c = std::make_shared<fx::propertyfx::Credential>();
    c->signatures = {go_sig()};

    auto mint_utxo = std::make_shared<fx::propertyfx::MintOutput>();
    mint_utxo->out_owners = owners(0, 1, {go_addr()});

    auto mop = std::make_shared<fx::propertyfx::MintOperation>();
    mop->mint_input.sig_indices = {0};
    mop->mint_output.out_owners = owners(0, 1, {go_addr()});
    mop->owned_output.out_owners = owners(0, 1, {addr2()});

    expect(rig.property.verify_operation(view(bytes), mop.get(), c.get(), {mint_utxo.get()}), "",
           "mint operation");
    expect(rig.property.verify_operation(view(bytes), mop.get(), nullptr, {mint_utxo.get()}),
           fx::kErrWrongCredentialType, "mint operation, wrong credential");
    expect(rig.property.verify_operation(view(bytes), mop.get(), c.get(),
                                         {mint_utxo.get(), mint_utxo.get()}),
           fx::kErrWrongNumberOfUTXOs, "mint operation, wrong number of utxos");
    {
        auto not_property = tout(1, owners(0, 1, {go_addr()}));
        expect(rig.property.verify_operation(view(bytes), mop.get(), c.get(), {not_property.get()}),
               fx::kErrWrongUTXOType, "mint operation, invalid utxo");
    }
    {
        auto bad = std::make_shared<fx::propertyfx::MintOperation>(*mop);
        bad->owned_output.out_owners = owners(0, 2, {addr2()});
        expect(rig.property.verify_operation(view(bytes), bad.get(), c.get(), {mint_utxo.get()}),
               fx::kErrOutputUnspendable, "mint operation, failing verification");
    }
    {
        // The mint authority must be re-created unchanged.
        auto bad = std::make_shared<fx::propertyfx::MintOperation>(*mop);
        bad->mint_output.out_owners = owners(0, 1, {addr2()});
        expect(rig.property.verify_operation(view(bytes), bad.get(), c.get(), {mint_utxo.get()}),
               "wrong mint output provided", "mint operation, mismatched mint output");
    }

    auto owned = std::make_shared<fx::propertyfx::OwnedOutput>();
    owned->out_owners = owners(0, 1, {go_addr()});
    auto burn = std::make_shared<fx::propertyfx::BurnOperation>();
    burn->input.sig_indices = {0};

    expect(rig.property.verify_operation(view(bytes), burn.get(), c.get(), {owned.get()}), "",
           "burn operation");
    {
        auto not_owned = tout(1, owners(0, 1, {go_addr()}));
        expect(rig.property.verify_operation(view(bytes), burn.get(), c.get(), {not_owned.get()}),
               fx::kErrWrongUTXOType, "burn operation, wrong utxo");
    }
    {
        auto bad = std::make_shared<fx::propertyfx::OwnedOutput>();
        bad->out_owners = owners(0, 2, {go_addr()});
        expect(rig.property.verify_operation(view(bytes), burn.get(), c.get(), {bad.get()}),
               fx::kErrOutputUnspendable, "burn operation, failed verify");
    }
    {
        auto nft_op = std::make_shared<fx::nftfx::TransferOperation>();
        expect(rig.property.verify_operation(view(bytes), nft_op.get(), c.get(), {owned.get()}),
               fx::kErrWrongOpType, "unknown operation");
    }
    {
        auto in = tin(1, {0});
        auto out = tout(1, owners(0, 1, {go_addr()}));
        expect(rig.property.verify_transfer(view(bytes), in.get(), c.get(), out.get()),
               "cant transfer with this fx", "a property cannot be transferred as value");
    }
    {
        // A burn produces nothing — that is what makes it a burn.
        check(burn->outs().empty(), "a burn operation produces no outputs");
        check(mop->outs().size() == 2, "a mint operation produces its two outputs");
    }
}

// ================= the value shapes' own gates =================

void value_gates() {
    std::printf("\n  -- the value shapes --\n");

    {
        fx::Input in;
        in.sig_indices = {0, 0};
        expect(in.verify(), fx::kErrInputIndicesNotSortedUnique, "repeated sig index");
        in.sig_indices = {1, 0};
        expect(in.verify(), fx::kErrInputIndicesNotSortedUnique, "unsorted sig indices");
        in.sig_indices = {0, 1};
        expect(in.verify(), "", "sorted unique sig indices");
        auto cost = in.cost();
        check(cost && *cost == 2 * fx::kCostPerSignature, "cost is 1000 per signature");
    }
    {
        fx::OutputOwners o = owners(0, 2, {go_addr()});
        expect(o.verify(), fx::kErrOutputUnspendable, "threshold above address count");
        o = owners(0, 0, {go_addr()});
        expect(o.verify(), fx::kErrOutputUnoptimized, "threshold 0 with addresses");
        o = owners(0, 1, {addr2(), go_addr()});
        // go_addr sorts before addr2? Establish the fact rather than assume it.
        auto sorted = o;
        sorted.sort();
        check(sorted.addrs[0] < sorted.addrs[1], "sort orders addresses ascending");
        if (o.addrs[0] > o.addrs[1]) {
            expect(o.verify(), fx::kErrAddrsNotSortedUnique, "unsorted addresses");
        } else {
            expect(o.verify(), "", "already-sorted addresses verify");
        }
        fx::OutputOwners dup = owners(0, 1, {go_addr(), go_addr()});
        expect(dup.verify(), fx::kErrAddrsNotSortedUnique, "duplicate addresses");
    }
    {
        auto out = tout(0, owners(0, 1, {go_addr()}));
        expect(out->verify(), fx::kErrNoValueOutput, "a zero-amount output has no value");
        auto in = tin(0, {0});
        expect(in->verify(), fx::kErrNoValueInput, "a zero-amount input has no value");
    }
}

}  // namespace

int main() {
    std::printf("xvm — the fx families, ported from the Go fx tests\n");
    go_signature_kat();
    recovery_id_matches_the_reference();
    secp_verify_transfer();
    secp_verify_operation();
    secp_verify_permission();
    nft_cases();
    property_cases();
    value_gates();
    return report("fx");
}
