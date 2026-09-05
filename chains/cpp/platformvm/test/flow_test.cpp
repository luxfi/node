// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// flow_test.cpp — who may spend, and that nothing was created.
//
// Ported from Go utxo/verifier_test.go (VerifySpendUTXOs) and
// secp256k1fx/fx_test.go (VerifyTransfer / VerifyCredentials). The signature
// case is not a self-consistency check: the key, the address and the signature
// all came out of the Go reference, so a recovery that disagrees fails here.

#include "golden.hpp"
#include "harness.hpp"
#include "lux/platformvm/flow.hpp"
#include "lux/platformvm/fx.hpp"

#include <string>

using namespace lux::platformvm;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    for (std::size_t i = 0; i < kIdLen; ++i) v[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
ShortId short_of(std::uint8_t b) {
    ShortId v{};
    for (std::size_t i = 0; i < kShortIdLen; ++i) v[i] = static_cast<std::uint8_t>(b + i);
    return v;
}

std::vector<std::uint8_t> unhex(const char* s) {
    auto nib = [](char c) -> int {
        if (c >= '0' && c <= '9') return c - '0';
        if (c >= 'a' && c <= 'f') return c - 'a' + 10;
        if (c >= 'A' && c <= 'F') return c - 'A' + 10;
        return -1;
    };
    std::vector<std::uint8_t> out;
    for (std::size_t i = 0; s[i] != 0 && s[i + 1] != 0; i += 2)
        out.push_back(static_cast<std::uint8_t>(nib(s[i]) * 16 + nib(s[i + 1])));
    return out;
}

txs::Credential cred_of(const std::vector<std::uint8_t>& sig) {
    txs::Credential c;
    std::array<std::uint8_t, txs::kSigLen> a{};
    for (std::size_t i = 0; i < a.size() && i < sig.size(); ++i) a[i] = sig[i];
    c.sigs.push_back(a);
    return c;
}

const std::uint64_t kAssetA = 0x10;

UTXO utxo_of(std::uint8_t tx, std::uint32_t idx, std::uint64_t amt, std::uint64_t stake_lock,
             const OutputOwners& owners) {
    UTXO u;
    u.utxo = UtxoId{id_of(tx), idx};
    u.asset = id_of(static_cast<std::uint8_t>(kAssetA));
    u.stake_lock = stake_lock;
    u.out = TransferOutput{amt, owners};
    return u;
}

TransferableInput in_of(std::uint8_t tx, std::uint32_t idx, std::uint64_t amt, std::uint64_t stake_lock,
                        std::vector<std::uint32_t> sig_indices) {
    TransferableInput in;
    in.utxo = UtxoId{id_of(tx), idx};
    in.asset = id_of(static_cast<std::uint8_t>(kAssetA));
    in.stake_lock = stake_lock;
    in.in = TransferInput{amt, std::move(sig_indices)};
    return in;
}

TransferableOutput out_of(std::uint64_t amt, std::uint64_t stake_lock, const OutputOwners& owners) {
    return TransferableOutput{id_of(static_cast<std::uint8_t>(kAssetA)), stake_lock,
                              TransferOutput{amt, owners}};
}

}  // namespace

// The key, the address and the signature all came from the Go reference. This
// case is the one that proves the recovery and the address derivation are the
// same function the network runs.
TEST(RecoverAddressMatchesTheReference) {
    const auto compressed = unhex(pvmgold::signer_pubkey_compressed);
    const auto addr = fx::address_of_compressed_key(compressed);
    REQUIRE_EQ(std::string(pvmgold::signer_addr), hex(addr));

    const auto unsigned_bytes = unhex(pvmgold::spend_tx_unsigned);
    const auto sig = unhex(pvmgold::spend_tx_sig0);
    auto recovered = fx::recover_address(sha256(unsigned_bytes), sig);
    REQUIRE_OK(recovered);
    REQUIRE_EQ(std::string(pvmgold::signer_addr), hex(recovered.value()));
}

// A signature over a DIFFERENT message recovers a different address, so it is
// refused — not accepted with a shrug.
TEST(WrongMessageIsRefused) {
    const auto sig = unhex(pvmgold::spend_tx_sig0);
    const auto other = unhex(pvmgold::base_tx);
    auto recovered = fx::recover_address(sha256(other), sig);
    // Recovery still succeeds (any well-formed signature recovers SOME key), and
    // that is exactly why the ADDRESS comparison is the check.
    REQUIRE_OK(recovered);
    REQUIRE(hex(recovered.value()) != std::string(pvmgold::signer_addr));
}

// Go: Fx.VerifyCredentials — the four shape refusals, then the real check.
TEST(VerifyCredentials) {
    const auto unsigned_bytes = unhex(pvmgold::spend_tx_unsigned);
    const auto sig = unhex(pvmgold::spend_tx_sig0);
    ShortId signer{};
    {
        const auto b = unhex(pvmgold::signer_addr);
        for (std::size_t i = 0; i < kShortIdLen; ++i) signer[i] = b[i];
    }
    const OutputOwners owners{0, 1, {signer}};
    const fx::Fx f(true);

    REQUIRE_OK(f.verify_credentials(unsigned_bytes, {0}, cred_of(sig), owners, 1000));

    // Timelocked: the output cannot be spent yet.
    const OutputOwners locked{2000, 1, {signer}};
    REQUIRE_ERR(f.verify_credentials(unsigned_bytes, {0}, cred_of(sig), locked, 1000), Err::Timelocked);

    // Too many / too few signers against the threshold.
    const OutputOwners two{0, 2, {signer, short_of(0x40)}};
    REQUIRE_ERR(f.verify_credentials(unsigned_bytes, {0}, cred_of(sig), two, 1000), Err::TooFewSigners);
    const OutputOwners zero{0, 0, {}};
    REQUIRE_ERR(f.verify_credentials(unsigned_bytes, {0}, cred_of(sig), zero, 1000), Err::TooManySigners);

    // The credential must carry exactly as many signatures as the input claims.
    txs::Credential empty;
    REQUIRE_ERR(f.verify_credentials(unsigned_bytes, {0}, empty, owners, 1000),
                Err::InputCredentialSignersMismatch);

    // An index the output does not have.
    REQUIRE_ERR(f.verify_credentials(unsigned_bytes, {1}, cred_of(sig), owners, 1000),
                Err::InputOutputIndexOutOfBounds);

    // A signature from someone else.
    const OutputOwners stranger{0, 1, {short_of(0x40)}};
    REQUIRE_ERR(f.verify_credentials(unsigned_bytes, {0}, cred_of(sig), stranger, 1000), Err::WrongSig);

    // A node still replaying history does not re-derive keys — and says so.
    const fx::Fx booting(false);
    REQUIRE_OK(booting.verify_credentials(unsigned_bytes, {0}, cred_of(sig), stranger, 1000));
}

// Go: Fx.VerifySpend — the amounts must match before anything else.
TEST(VerifyTransferAmountsMustMatch) {
    const auto unsigned_bytes = unhex(pvmgold::spend_tx_unsigned);
    const auto sig = unhex(pvmgold::spend_tx_sig0);
    ShortId signer{};
    {
        const auto b = unhex(pvmgold::signer_addr);
        for (std::size_t i = 0; i < kShortIdLen; ++i) signer[i] = b[i];
    }
    const OutputOwners owners{0, 1, {signer}};
    const fx::Fx f(true);

    TransferOutput utxo{1000, owners};
    TransferInput in{1000, {0}};
    REQUIRE_OK(f.verify_transfer(unsigned_bytes, in, cred_of(sig), utxo, 1000));

    TransferInput less{999, {0}};
    REQUIRE_ERR(f.verify_transfer(unsigned_bytes, less, cred_of(sig), utxo, 1000), Err::MismatchedAmounts);
}

// The whole spend: a real UTXO, a real signature, and the fee coming out of the
// difference. Go: utxo.VerifySpendUTXOs.
TEST(VerifySpendUTXOs) {
    const auto unsigned_bytes = unhex(pvmgold::spend_tx_unsigned);
    const auto sig = unhex(pvmgold::spend_tx_sig0);
    ShortId signer{};
    {
        const auto b = unhex(pvmgold::signer_addr);
        for (std::size_t i = 0; i < kShortIdLen; ++i) signer[i] = b[i];
    }
    const OutputOwners mine{0, 1, {signer}};
    const OutputOwners theirs{0, 1, {short_of(0x30)}};
    const fx::Fx f(true);

    const std::vector<UTXO> utxos = {utxo_of(0xA0, 0, 1000, 0, mine)};
    const std::vector<TransferableInput> ins = {in_of(0xA0, 0, 1000, 0, {0})};
    const std::vector<TransferableOutput> outs = {out_of(900, 0, theirs)};
    const std::vector<txs::Credential> creds = {cred_of(sig)};

    // 1000 consumed, 900 produced, 100 fee: exactly balanced.
    flow::Produced fee;
    fee[id_of(0x10)] = 100;
    REQUIRE_OK(flow::verify_spend_utxos(f, unsigned_bytes, utxos, ins, outs, creds, fee, 1000));

    // One more unit of fee than the inputs cover.
    flow::Produced too_much;
    too_much[id_of(0x10)] = 101;
    REQUIRE_ERR(flow::verify_spend_utxos(f, unsigned_bytes, utxos, ins, outs, creds, too_much, 1000),
                Err::InsufficientUnlockedFunds);

    // Counts must line up.
    REQUIRE_ERR(flow::verify_spend_utxos(f, unsigned_bytes, utxos, ins, outs, {}, fee, 1000),
                Err::WrongNumberCredentials);
    REQUIRE_ERR(flow::verify_spend_utxos(f, unsigned_bytes, {}, ins, outs, creds, fee, 1000),
                Err::WrongNumberUTXOs);

    // The input must claim the asset the UTXO actually is.
    auto wrong_asset = ins;
    wrong_asset[0].asset = id_of(0x11);
    REQUIRE_ERR(flow::verify_spend_utxos(f, unsigned_bytes, utxos, wrong_asset, outs, creds, fee, 1000),
                Err::AssetIDMismatch);
}

// Go: errLockedFundsNotMarkedAsLocked / errLocktimeMismatch. Locked funds may
// not be spent as if they were free, and the lock may not be moved.
TEST(LockedFundsStayLocked) {
    const auto unsigned_bytes = unhex(pvmgold::spend_tx_unsigned);
    const auto sig = unhex(pvmgold::spend_tx_sig0);
    ShortId signer{};
    {
        const auto b = unhex(pvmgold::signer_addr);
        for (std::size_t i = 0; i < kShortIdLen; ++i) signer[i] = b[i];
    }
    const OutputOwners mine{0, 1, {signer}};
    const fx::Fx f(true);

    const std::vector<UTXO> locked = {utxo_of(0xA0, 0, 1000, 5000, mine)};
    const std::vector<txs::Credential> creds = {cred_of(sig)};

    // Consuming a still-locked UTXO with an input that does not carry the lock.
    const std::vector<TransferableInput> bare = {in_of(0xA0, 0, 1000, 0, {0})};
    REQUIRE_ERR(flow::verify_spend_utxos(f, unsigned_bytes, locked, bare, {}, creds, {}, 1000),
                Err::LockedFundsNotMarkedAsLocked);

    // Carrying the lock, but claiming a different unlock time.
    const std::vector<TransferableInput> moved = {in_of(0xA0, 0, 1000, 4000, {0})};
    REQUIRE_ERR(flow::verify_spend_utxos(f, unsigned_bytes, locked, moved, {}, creds, {}, 1000),
                Err::LocktimeMismatch);

    // Carrying it correctly, and re-locking the same amount to the same owner.
    const std::vector<TransferableInput> kept = {in_of(0xA0, 0, 1000, 5000, {0})};
    const std::vector<TransferableOutput> relocked = {out_of(1000, 5000, mine)};
    REQUIRE_OK(flow::verify_spend_utxos(f, unsigned_bytes, locked, kept, relocked, creds, {}, 1000));

    // Re-locking it to SOMEONE ELSE conserves value and steals it, so the ledger
    // is keyed by owner and this is refused.
    const OutputOwners theirs{0, 1, {short_of(0x30)}};
    const std::vector<TransferableOutput> stolen = {out_of(1000, 5000, theirs)};
    REQUIRE_ERR(flow::verify_spend_utxos(f, unsigned_bytes, locked, kept, stolen, creds, {}, 1000),
                Err::InsufficientLockedFunds);

    // Moving the unlock EARLIER is the same theft, in time.
    const std::vector<TransferableOutput> early = {out_of(1000, 4000, mine)};
    REQUIRE_ERR(flow::verify_spend_utxos(f, unsigned_bytes, locked, kept, early, creds, {}, 1000),
                Err::InsufficientLockedFunds);

    // Once the lock has expired the funds are ordinary, and a bare input is fine.
    REQUIRE_OK(flow::verify_spend_utxos(f, unsigned_bytes, locked, bare, {}, creds, {}, 6000));
}

// Locking MORE of your own money is allowed: the shortfall comes out of the
// unlocked consumption, and is then no longer available to the free balance.
TEST(UnlockedFundsMayBackALock) {
    const auto unsigned_bytes = unhex(pvmgold::spend_tx_unsigned);
    const auto sig = unhex(pvmgold::spend_tx_sig0);
    ShortId signer{};
    {
        const auto b = unhex(pvmgold::signer_addr);
        for (std::size_t i = 0; i < kShortIdLen; ++i) signer[i] = b[i];
    }
    const OutputOwners mine{0, 1, {signer}};
    const fx::Fx f(true);

    const std::vector<UTXO> free_utxo = {utxo_of(0xA0, 0, 1000, 0, mine)};
    const std::vector<TransferableInput> ins = {in_of(0xA0, 0, 1000, 0, {0})};
    const std::vector<txs::Credential> creds = {cred_of(sig)};

    // 1000 free in, 600 locked out: allowed, 400 left free.
    const std::vector<TransferableOutput> lock600 = {out_of(600, 5000, mine)};
    flow::Produced fee400;
    fee400[id_of(0x10)] = 400;
    REQUIRE_OK(flow::verify_spend_utxos(f, unsigned_bytes, free_utxo, ins, lock600, creds, fee400, 1000));

    // The 600 that went into the lock is spent: only 400 remains for the fee.
    flow::Produced fee401;
    fee401[id_of(0x10)] = 401;
    REQUIRE_ERR(flow::verify_spend_utxos(f, unsigned_bytes, free_utxo, ins, lock600, creds, fee401, 1000),
                Err::InsufficientUnlockedFunds);

    // And more locked out than there is money in refuses on the locked side.
    const std::vector<TransferableOutput> lock1001 = {out_of(1001, 5000, mine)};
    REQUIRE_ERR(flow::verify_spend_utxos(f, unsigned_bytes, free_utxo, ins, lock1001, creds, {}, 1000),
                Err::InsufficientLockedFunds);
}
