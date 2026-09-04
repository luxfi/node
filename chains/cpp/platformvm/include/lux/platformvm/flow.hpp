// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// flow.hpp — no value is created, and locked value stays locked.
//
// Rendered from Go vms/platformvm/utxo/verifier.go (VerifySpend /
// VerifySpendUTXOs). Two invariants, checked together because they interact:
//
//   1. For every asset, produced ≤ consumed. The fee is "produced" that has no
//      output, which is how a fee is charged without a special case.
//   2. For every (asset, locktime, owner), locked produced ≤ locked consumed.
//      Unlocked funds may back a shortfall — you may lock more of your own money
//      — but locked funds may never come out early, be re-owned, or have their
//      unlock moved.
//
// The second is why the walk is keyed by OWNER as well as locktime: without it,
// a transaction could consume Alice's locked output and produce a locked output
// of the same amount owned by Bob, conserving value while stealing it. The owner
// key is sha256 of the ONE canonical owner encoding — the same bytes a
// transaction carries — so two spellings of one owner cannot become two owners.

#pragma once

#include "lux/platformvm/components.hpp"
#include "lux/platformvm/error.hpp"
#include "lux/platformvm/fx.hpp"
#include "lux/platformvm/ids.hpp"

#include <cstdint>
#include <map>
#include <span>
#include <vector>

namespace lux::platformvm::flow {

// What the transaction produces that no output accounts for, per asset — the
// fee. Anything here must come out of the inputs.
using Produced = std::map<Id, std::uint64_t>;

// Go: utxo.VerifySpendUTXOs. `tx_bytes` is the UNSIGNED buffer the credentials
// sign over; `now` is the clock a locktime is judged against.
Status verify_spend_utxos(const fx::Fx& f, std::span<const std::uint8_t> tx_bytes,
                          const std::vector<UTXO>& utxos, const std::vector<TransferableInput>& ins,
                          const std::vector<TransferableOutput>& outs,
                          const std::vector<txs::Credential>& creds, Produced unlocked_produced,
                          std::uint64_t now);

}  // namespace lux::platformvm::flow
