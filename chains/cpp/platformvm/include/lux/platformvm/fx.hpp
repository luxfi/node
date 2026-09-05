// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fx.hpp — who is allowed to spend an output, and the proof of it.
//
// Rendered from github.com/luxfi/utxo/secp256k1fx (fx.go VerifySpend /
// VerifyCredentials, credential.go). This is the authorisation half of a
// transaction: the flow check in utxo.hpp proves that no value was created, and
// this proves that whoever moved it was allowed to.
//
// The check is a REAL public-key recovery. A signature is 65 bytes — r ‖ s ‖ v —
// over sha256 of the unsigned transaction; the recovered key is compressed,
// hashed to an address (ripemd160 ∘ sha256, the Lux address derivation) and that
// address must be the one the output names at the index the input claimed. There
// is no mode in which this answers yes without doing the arithmetic, other than
// the one the reference has and names: a node that is still bootstrapping is
// replaying history the network already agreed on, and re-checks every signature
// once it is caught up.

#pragma once

#include "lux/platformvm/components.hpp"
#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"
#include "lux/platformvm/txs.hpp"

#include <cstdint>
#include <span>
#include <vector>

namespace lux::platformvm::fx {

// The Lux address of a secp256k1 public key: ripemd160(sha256(compressed key)).
// Rendered from hash.PubkeyBytesToAddress over PublicKey.Bytes(), which is the
// COMPRESSED form — 0x02/0x03 by the parity of y, then x.
ShortId address_of_compressed_key(std::span<const std::uint8_t> compressed);

// Recover the signing key from a 65-byte r‖s‖v signature over `hash`, and return
// the address it belongs to. An unrecoverable signature is a refusal.
Result<ShortId> recover_address(const Id& hash, std::span<const std::uint8_t> sig65);

// The signature-checking half of the fx. `bootstrapped` false skips the
// recovery, exactly as the reference does while a node replays history.
class Fx {
  public:
    explicit Fx(bool bootstrapped = true) : bootstrapped_(bootstrapped) {}

    void set_bootstrapped(bool b) { bootstrapped_ = b; }
    bool bootstrapped() const { return bootstrapped_; }

    // Go: Fx.VerifyCredentials. `now` is the clock the locktime is judged
    // against, and `tx_bytes` is the UNSIGNED buffer the signatures cover.
    Status verify_credentials(std::span<const std::uint8_t> tx_bytes,
                              const std::vector<std::uint32_t>& sig_indices, const txs::Credential& cred,
                              const OutputOwners& owners, std::uint64_t now) const;

    // Go: Fx.VerifySpend — the amounts must match, then the credentials must
    // authorise the owners of the output being consumed.
    Status verify_transfer(std::span<const std::uint8_t> tx_bytes, const TransferInput& in,
                           const txs::Credential& cred, const TransferOutput& utxo,
                           std::uint64_t now) const;

    // Go: Fx.VerifyPermission — an authorisation over an owner that is not an
    // output (a network owner authorising a change to its own network).
    Status verify_permission(std::span<const std::uint8_t> tx_bytes, const txs::Auth& auth,
                             const txs::Credential& cred, const OutputOwners& owners,
                             std::uint64_t now) const;

  private:
    bool bootstrapped_ = true;
};

}  // namespace lux::platformvm::fx
