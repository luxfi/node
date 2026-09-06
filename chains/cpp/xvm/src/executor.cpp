// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/executor.hpp"

#include <cstring>

namespace lux::xvm::executor {
namespace {

// ---- the asset-name character rules ----
//
// Go iterates RUNES and rejects anything above U+007F, so a byte-wise scan is
// equivalent for the character test: in valid UTF-8 every byte of a rune above
// U+007F has the high bit set, and invalid UTF-8 decodes to U+FFFD, which is
// also above U+007F. The whitespace test is NOT byte-wise, because Go trims the
// full Unicode White_Space set — so that one decodes.

bool ascii_letter(std::uint8_t c) { return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z'); }
bool ascii_number(std::uint8_t c) { return c >= '0' && c <= '9'; }
bool ascii_upper(std::uint8_t c) { return c >= 'A' && c <= 'Z'; }

// decode_rune reads one UTF-8 rune, returning U+FFFD and width 1 on malformed
// input — Go's decoding rule, which matters because RuneError is above ASCII and
// therefore rejected.
std::pair<std::uint32_t, std::size_t> decode_rune(const std::string& s, std::size_t i) {
    const auto b0 = std::uint8_t(s[i]);
    if (b0 < 0x80) return {b0, 1};
    auto cont = [&](std::size_t k) {
        return i + k < s.size() && (std::uint8_t(s[i + k]) & 0xC0) == 0x80;
    };
    if ((b0 & 0xE0) == 0xC0 && cont(1))
        return {std::uint32_t((b0 & 0x1F) << 6 | (std::uint8_t(s[i + 1]) & 0x3F)), 2};
    if ((b0 & 0xF0) == 0xE0 && cont(1) && cont(2))
        return {std::uint32_t((b0 & 0x0F) << 12 | (std::uint8_t(s[i + 1]) & 0x3F) << 6 |
                              (std::uint8_t(s[i + 2]) & 0x3F)),
                3};
    if ((b0 & 0xF8) == 0xF0 && cont(1) && cont(2) && cont(3))
        return {std::uint32_t((b0 & 0x07) << 18 | (std::uint8_t(s[i + 1]) & 0x3F) << 12 |
                              (std::uint8_t(s[i + 2]) & 0x3F) << 6 |
                              (std::uint8_t(s[i + 3]) & 0x3F)),
                4};
    return {0xFFFD, 1};
}

// Unicode White_Space, the set Go's strings.TrimSpace trims.
bool unicode_space(std::uint32_t r) {
    switch (r) {
        case '\t':
        case '\n':
        case '\v':
        case '\f':
        case '\r':
        case ' ':
        case 0x85:
        case 0xA0:
        case 0x1680:
        case 0x2028:
        case 0x2029:
        case 0x202F:
        case 0x205F:
        case 0x3000:
            return true;
        default:
            return r >= 0x2000 && r <= 0x200A;
    }
}

bool has_surrounding_space(const std::string& s) {
    if (s.empty()) return false;
    auto [first, _] = decode_rune(s, 0);
    if (unicode_space(first)) return true;
    // Walk forward to find the last rune's start — the string is short (<=128)
    // so this is cheaper than a reverse decoder nobody else needs.
    std::size_t i = 0, last_start = 0;
    while (i < s.size()) {
        last_start = i;
        i += decode_rune(s, i).second;
    }
    return unicode_space(decode_rune(s, last_start).first);
}

// The one historical exception in the whole chain: a single mainnet OperationTx
// whose operations are accepted without fx verification. It is a fact about the
// chain's history, not a policy, so it is a constant rather than a flag.
const Id& legacy_unverified_operation_tx() {
    static const Id id = [] {
        Id v{};
        static const std::uint8_t raw[32] = {0x2f, 0x21, 0xd5, 0x74, 0x88, 0x89, 0x2c, 0x35,
                                             0xa3, 0x39, 0xd1, 0xbf, 0x09, 0x6f, 0x8f, 0x33,
                                             0xe0, 0xe6, 0x01, 0x51, 0xc3, 0xf4, 0x2a, 0x99,
                                             0x23, 0x73, 0x5b, 0x79, 0xbf, 0x4b, 0x2e, 0x68};
        std::memcpy(v.data(), raw, 32);
        return v;
    }();
    return id;
}

}  // namespace

// ================= pass 1: syntax =================

Result<void> SyntacticVerifier::verify_credential_count(int num_inputs) const {
    for (const auto& cred : tx_.creds) {
        if (cred == nullptr) return std::unexpected(fx::kErrNilCredential);
        if (auto r = cred->verify(); !r) return r;
    }
    int num_creds = int(tx_.creds.size());
    if (num_creds != num_inputs)
        return std::unexpected(std::string(kErrWrongNumberOfCredentials) + ": " +
                               std::to_string(num_creds) + " != " + std::to_string(num_inputs));
    return {};
}

Result<void> SyntacticVerifier::base_tx(txs::BaseTx& tx) {
    if (auto r = tx.base.verify(backend_.network_id, backend_.chain_id); !r) return r;
    if (auto r = txs::verify_tx(backend_.config.tx_fee, backend_.fee_asset_id, {&tx.base.ins},
                                {&tx.base.outs});
        !r)
        return r;
    return verify_credential_count(int(tx.base.ins.size()));
}

Result<void> SyntacticVerifier::create_asset_tx(txs::CreateAssetTx& tx) {
    if (tx.name.size() < kMinNameLen) return std::unexpected(kErrNameTooShort);
    if (tx.name.size() > kMaxNameLen) return std::unexpected(kErrNameTooLong);
    if (tx.symbol.size() < kMinSymbolLen) return std::unexpected(kErrSymbolTooShort);
    if (tx.symbol.size() > kMaxSymbolLen) return std::unexpected(kErrSymbolTooLong);
    if (tx.states.empty()) return std::unexpected(kErrNoFxs);
    if (tx.denomination > kMaxDenomination) return std::unexpected(kErrDenominationTooLarge);
    if (has_surrounding_space(tx.name)) return std::unexpected(kErrUnexpectedWhitespace);

    for (std::size_t i = 0; i < tx.name.size();) {
        auto [r, w] = decode_rune(tx.name, i);
        if (r > 0x7F || (!ascii_letter(std::uint8_t(r)) && !ascii_number(std::uint8_t(r)) &&
                         r != ' '))
            return std::unexpected(kErrIllegalNameCharacter);
        i += w;
    }
    for (std::size_t i = 0; i < tx.symbol.size();) {
        auto [r, w] = decode_rune(tx.symbol, i);
        if (r > 0x7F || !ascii_upper(std::uint8_t(r)))
            return std::unexpected(kErrIllegalSymbolCharacter);
        i += w;
    }

    if (auto r = tx.base.verify(backend_.network_id, backend_.chain_id); !r) return r;
    if (auto r = txs::verify_tx(backend_.config.create_asset_tx_fee, backend_.fee_asset_id,
                                {&tx.base.ins}, {&tx.base.outs});
        !r)
        return r;

    for (const auto& state : tx.states) {
        if (auto r = state.verify(int(backend_.fxs.size())); !r) return r;
        // An fx index in RANGE is not the same as an fx that is THERE. A slot the
        // host left empty keeps its position — the index is the position, so a
        // missing family cannot be compacted away without renumbering every asset
        // that came before — but an asset must not be able to declare it. Go
        // refuses the empty slot earlier, when the VM is initialized; this port
        // takes its fx list through a constructor and has no error to return
        // there, so the refusal lands here, at the one place an empty slot could
        // otherwise be named.
        if (backend_.fxs[state.fx_index].fx == nullptr) return std::unexpected(kErrUnknownFx);
    }
    for (std::size_t i = 0; i + 1 < tx.states.size(); ++i) {
        if (tx.states[i].compare(tx.states[i + 1]) >= 0)
            return std::unexpected(kErrInitialStatesNotSortedUnique);
    }
    return verify_credential_count(int(tx.base.ins.size()));
}

Result<void> SyntacticVerifier::operation_tx(txs::OperationTx& tx) {
    if (tx.ops.empty()) return std::unexpected(kErrNoOperations);
    if (auto r = tx.base.verify(backend_.network_id, backend_.chain_id); !r) return r;
    if (auto r = txs::verify_tx(backend_.config.tx_fee, backend_.fee_asset_id, {&tx.base.ins},
                                {&tx.base.outs});
        !r)
        return r;

    // An operation consumes UTXOs too, so its ids share one double-spend set
    // with the inputs — otherwise a tx could spend the same UTXO twice by naming
    // it once as an input and once in an operation.
    std::set<Id> inputs;
    for (const auto& in : tx.base.ins) inputs.insert(in.utxo_id.input_id());

    for (const auto& op : tx.ops) {
        if (auto r = op.verify(); !r) return r;
        for (const auto& utxo_id : op.utxo_ids) {
            Id input_id = utxo_id.input_id();
            if (inputs.count(input_id) != 0) return std::unexpected(kErrDoubleSpend);
            inputs.insert(input_id);
        }
    }
    if (!txs::is_sorted_and_unique_operations(tx.ops))
        return std::unexpected(kErrOperationsNotSortedUnique);

    return verify_credential_count(int(tx.base.ins.size() + tx.ops.size()));
}

Result<void> SyntacticVerifier::import_tx(txs::ImportTx& tx) {
    if (tx.imported_ins.empty()) return std::unexpected(kErrNoImportInputs);
    if (auto r = tx.base.verify(backend_.network_id, backend_.chain_id); !r) return r;
    if (auto r = txs::verify_tx(backend_.config.tx_fee, backend_.fee_asset_id,
                                {&tx.base.ins, &tx.imported_ins}, {&tx.base.outs});
        !r)
        return r;
    return verify_credential_count(int(tx.base.ins.size() + tx.imported_ins.size()));
}

Result<void> SyntacticVerifier::export_tx(txs::ExportTx& tx) {
    if (tx.exported_outs.empty()) return std::unexpected(kErrNoExportOutputs);
    if (auto r = tx.base.verify(backend_.network_id, backend_.chain_id); !r) return r;
    if (auto r = txs::verify_tx(backend_.config.tx_fee, backend_.fee_asset_id, {&tx.base.ins},
                                {&tx.base.outs, &tx.exported_outs});
        !r)
        return r;
    return verify_credential_count(int(tx.base.ins.size()));
}

// ================= pass 2: the chain's agreement =================

Result<int> SemanticVerifier::get_fx(const fx::FxValue* v) {
    int idx = backend_.fx_index.get_fx(v);
    if (idx < 0) return std::unexpected(kErrUnknownFx);
    return idx;
}

Result<void> SemanticVerifier::verify_fx_usage(int fx_index, const Id& asset_id) {
    // An asset declares WHICH fxs may touch it, in its CreateAssetTx. Reading
    // that tx back is how a spend is checked against the asset's own rules
    // rather than against a global setting.
    auto tx = chain_.get_tx(asset_id);
    if (!tx) return std::unexpected(tx.error());
    const auto* create = dynamic_cast<const txs::CreateAssetTx*>((*tx)->unsigned_tx.get());
    if (create == nullptr) return std::unexpected(kErrNotAnAsset);
    for (const auto& state : create->states) {
        if (state.fx_index == std::uint32_t(fx_index)) return Result<void>{};
    }
    return std::unexpected(kErrIncompatibleFx);
}

Result<void> SemanticVerifier::verify_transfer_of_utxo(txs::UnsignedTx& tx,
                                                       const txs::TransferableInput& in,
                                                       const fx::FxValue* cred,
                                                       const txs::UTXO& utxo) {
    if (utxo.asset_id != in.asset_id) return std::unexpected(kErrAssetIDMismatch);

    auto fx_index = get_fx(cred);
    if (!fx_index) return std::unexpected(fx_index.error());
    if (auto r = verify_fx_usage(*fx_index, in.asset_id); !r) return r;

    const auto& parsed = backend_.fxs[std::size_t(*fx_index)];
    return parsed.fx->verify_transfer(view(tx.bytes()), in.in.get(), cred, utxo.out.get());
}

Result<void> SemanticVerifier::verify_transfer(txs::UnsignedTx& tx,
                                               const txs::TransferableInput& in,
                                               const fx::FxValue* cred) {
    auto utxo = chain_.get_utxo(in.utxo_id.input_id());
    if (!utxo)
        return std::unexpected("failed to get utxo " + hex(in.utxo_id.input_id()) + ": " +
                               utxo.error());
    return verify_transfer_of_utxo(tx, in, cred, *utxo);
}

Result<void> SemanticVerifier::verify_base(txs::UnsignedTx& tx) {
    for (std::size_t i = 0; i < tx.base.ins.size(); ++i) {
        // The credential COUNT was checked in syntactic verification, which runs
        // first; indexing here is therefore total.
        const auto& cred = tx_.creds[i];
        if (auto r = verify_transfer(tx, tx.base.ins[i], cred.get()); !r)
            return std::unexpected("failed to verify transfer: " + r.error());
    }
    for (const auto& out : tx.base.outs) {
        auto fx_index = get_fx(out.out.get());
        if (!fx_index) return std::unexpected("failed to get fx: " + fx_index.error());
        if (auto r = verify_fx_usage(*fx_index, out.asset_id); !r)
            return std::unexpected("failed to verify fx usage: " + r.error());
    }
    return {};
}

Result<void> SemanticVerifier::base_tx(txs::BaseTx& tx) { return verify_base(tx); }

Result<void> SemanticVerifier::create_asset_tx(txs::CreateAssetTx& tx) { return verify_base(tx); }

Result<void> SemanticVerifier::operation_tx(txs::OperationTx& tx) {
    if (auto r = verify_base(tx); !r) return r;

    if (!backend_.bootstrapped || tx_.id() == legacy_unverified_operation_tx()) return Result<void>{};

    const std::size_t offset = tx.base.ins.size();
    for (std::size_t i = 0; i < tx.ops.size(); ++i) {
        const auto& cred = tx_.creds[i + offset];
        if (auto r = verify_operation(tx, tx.ops[i], cred.get()); !r) return r;
    }
    return {};
}

Result<void> SemanticVerifier::verify_operation(txs::OperationTx& tx, const txs::Operation& op,
                                                const fx::FxValue* cred) {
    std::vector<txs::UTXO> owned;
    std::vector<const fx::FxValue*> utxo_outs;
    owned.reserve(op.utxo_ids.size());
    for (const auto& utxo_id : op.utxo_ids) {
        auto utxo = chain_.get_utxo(utxo_id.input_id());
        if (!utxo) return std::unexpected(utxo.error());
        if (utxo->asset_id != op.asset_id) return std::unexpected(kErrAssetIDMismatch);
        owned.push_back(*utxo);
    }
    for (const auto& u : owned) utxo_outs.push_back(u.out.get());

    auto fx_index = get_fx(op.op.get());
    if (!fx_index) return std::unexpected(fx_index.error());
    if (auto r = verify_fx_usage(*fx_index, op.asset_id); !r) return r;

    const auto& parsed = backend_.fxs[std::size_t(*fx_index)];
    return parsed.fx->verify_operation(view(tx.bytes()), op.op.get(), cred, utxo_outs);
}

Result<void> SemanticVerifier::same_net(const Id& peer_chain_id) {
    if (peer_chain_id == backend_.chain_id) return std::unexpected(kErrSameChainID);
    if (backend_.net_lookup == nullptr)
        return std::unexpected("failed to get net of a peer chain: no net lookup");
    auto peer_net = backend_.net_lookup->network_of(peer_chain_id);
    if (!peer_net)
        return std::unexpected("failed to get net of " + hex(peer_chain_id) + ": " +
                               peer_net.error());
    if (backend_.net_id != *peer_net) return std::unexpected(kErrMismatchedNetIDs);
    return {};
}

Result<void> SemanticVerifier::import_tx(txs::ImportTx& tx) {
    if (auto r = verify_base(tx); !r) return r;
    if (!backend_.bootstrapped) return Result<void>{};
    if (auto r = same_net(tx.source_chain); !r) return r;

    std::vector<Bytes> utxo_ids;
    utxo_ids.reserve(tx.imported_ins.size());
    for (const auto& in : tx.imported_ins) {
        Id input_id = in.utxo_id.input_id();
        utxo_ids.emplace_back(input_id.begin(), input_id.end());
    }
    if (backend_.shared_memory == nullptr) return std::unexpected("no shared memory");
    auto all = backend_.shared_memory->get(tx.source_chain, utxo_ids);
    if (!all) return std::unexpected(all.error());
    if (all->size() != tx.imported_ins.size())
        return std::unexpected("shared memory returned the wrong number of utxos");

    const std::size_t offset = tx.base.ins.size();
    for (std::size_t i = 0; i < tx.imported_ins.size(); ++i) {
        // The SAME ZAP envelope flows on shared memory and on disk, so an
        // imported UTXO is parsed by the one UTXO parser rather than a
        // transport-specific one.
        auto utxo = txs::parse_utxo(view((*all)[i]));
        if (!utxo) return std::unexpected(utxo.error());
        const auto& cred = tx_.creds[i + offset];
        if (auto r = verify_transfer_of_utxo(tx, tx.imported_ins[i], cred.get(), *utxo); !r)
            return r;
    }
    return {};
}

Result<void> SemanticVerifier::export_tx(txs::ExportTx& tx) {
    if (auto r = verify_base(tx); !r) return r;
    if (backend_.bootstrapped) {
        if (auto r = same_net(tx.destination_chain); !r) return r;
    }
    for (const auto& out : tx.exported_outs) {
        auto fx_index = get_fx(out.out.get());
        if (!fx_index) return std::unexpected(fx_index.error());
        if (auto r = verify_fx_usage(*fx_index, out.asset_id); !r) return r;
    }
    return {};
}

// ================= pass 3: apply =================

namespace {

// The spending half every kind shares: consume what the inputs name, produce the
// outputs at indices 0..n-1. Each kind then adds only what is ITS own.
void apply_base(state::Chain& chain, const Id& tx_id, txs::UnsignedTx& tx) {
    state::consume(chain, tx.base.ins);
    state::produce(chain, tx_id, tx.base.outs);
}

}  // namespace

Result<void> Executor::base_tx(txs::BaseTx& tx) {
    apply_base(chain_, tx_.id(), tx);
    return {};
}

Result<void> Executor::create_asset_tx(txs::CreateAssetTx& tx) {
    apply_base(chain_, tx_.id(), tx);
    // A new asset's genesis outputs are denominated in the asset itself, whose
    // id IS this tx's id — so the asset and its first UTXOs are created in one
    // indivisible step.
    std::uint32_t index = std::uint32_t(tx.base.outs.size());
    for (const auto& state : tx.states) {
        for (const auto& out : state.outs) {
            chain_.add_utxo(txs::UTXO{txs::UTXOID{tx_.id(), index, false}, tx_.id(), out});
            ++index;
        }
    }
    return {};
}

Result<void> Executor::operation_tx(txs::OperationTx& tx) {
    apply_base(chain_, tx_.id(), tx);
    std::uint32_t index = std::uint32_t(tx.base.outs.size());
    for (const auto& op : tx.ops) {
        for (const auto& utxo_id : op.utxo_ids) chain_.delete_utxo(utxo_id.input_id());
        for (const auto& out : op.op->outs()) {
            chain_.add_utxo(txs::UTXO{txs::UTXOID{tx_.id(), index, false}, op.asset_id, out});
            ++index;
        }
    }
    return {};
}

Result<void> Executor::import_tx(txs::ImportTx& tx) {
    apply_base(chain_, tx_.id(), tx);
    AtomicRequests req;
    for (const auto& in : tx.imported_ins) {
        Id utxo_id = in.utxo_id.input_id();
        inputs.insert(utxo_id);
        req.remove_requests.emplace_back(utxo_id.begin(), utxo_id.end());
    }
    atomic_requests[tx.source_chain] = std::move(req);
    return {};
}

Result<void> Executor::export_tx(txs::ExportTx& tx) {
    apply_base(chain_, tx_.id(), tx);
    std::uint32_t index = std::uint32_t(tx.base.outs.size());
    AtomicRequests req;
    for (const auto& out : tx.exported_outs) {
        txs::UTXO utxo{txs::UTXOID{tx_.id(), index, false}, out.asset_id,
                       std::static_pointer_cast<fx::FxOutput>(out.out)};
        ++index;
        auto bytes = utxo.wire_bytes();
        if (!bytes) return std::unexpected("failed to marshal UTXO: " + bytes.error());
        Id utxo_id = utxo.utxo_id.input_id();
        AtomicElement elem;
        elem.key.assign(utxo_id.begin(), utxo_id.end());
        elem.value = *bytes;
        elem.traits = utxo.out->addresses();
        req.put_requests.push_back(std::move(elem));
    }
    atomic_requests[tx.destination_chain] = std::move(req);
    return {};
}

}  // namespace lux::xvm::executor
