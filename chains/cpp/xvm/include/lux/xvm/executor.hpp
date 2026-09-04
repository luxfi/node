// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// executor.hpp — the three passes a transaction goes through, in order:
//
//   SyntacticVerifier   is this tx well-formed ON ITS OWN? (shape, fees, sorting,
//                       credential count) — no state is read
//   SemanticVerifier    does the CHAIN agree? (the UTXOs exist, the credentials
//                       authorize them, the fx is one the asset declared)
//   Executor            apply it: consume the inputs, produce the outputs
//
// The split is load-bearing. Syntactic verification is pure, so it can run on a
// gossiped tx before any state is touched; semantic verification reads state but
// writes none, so a failing tx costs nothing; execution writes, and only runs
// once the first two passed.

#pragma once

#include "lux/xvm/fx.hpp"
#include "lux/xvm/state.hpp"
#include "lux/xvm/txs.hpp"

#include <map>
#include <memory>
#include <set>
#include <string>
#include <vector>

namespace lux::xvm::executor {

template <class T>
using Result = wire::Result<T>;

// ---- error sentinels, one per Go error ----
inline constexpr const char* kErrWrongNumberOfCredentials = "wrong number of credentials";
inline constexpr const char* kErrInitialStatesNotSortedUnique =
    "initial states not sorted and unique";
inline constexpr const char* kErrNameTooShort = "name is too short, minimum size is 1";
inline constexpr const char* kErrNameTooLong = "name is too long, maximum size is 128";
inline constexpr const char* kErrSymbolTooShort = "symbol is too short, minimum size is 1";
inline constexpr const char* kErrSymbolTooLong = "symbol is too long, maximum size is 4";
inline constexpr const char* kErrNoFxs = "assets must support at least one Fx";
inline constexpr const char* kErrIllegalNameCharacter =
    "asset's name must be made up of only letters and numbers";
inline constexpr const char* kErrIllegalSymbolCharacter =
    "asset's symbol must be all upper case letters";
inline constexpr const char* kErrUnexpectedWhitespace = "unexpected whitespace provided";
inline constexpr const char* kErrDenominationTooLarge = "denomination is too large";
inline constexpr const char* kErrOperationsNotSortedUnique = "operations not sorted and unique";
inline constexpr const char* kErrNoOperations = "an operationTx must have at least one operation";
inline constexpr const char* kErrDoubleSpend = "inputs attempt to double spend an input";
inline constexpr const char* kErrNoImportInputs = "no import inputs";
inline constexpr const char* kErrNoExportOutputs = "no export outputs";

inline constexpr const char* kErrAssetIDMismatch = "asset IDs in the input don't match the utxo";
inline constexpr const char* kErrNotAnAsset = "not an asset";
inline constexpr const char* kErrIncompatibleFx = "incompatible feature extension";
inline constexpr const char* kErrUnknownFx = "unknown feature extension";
inline constexpr const char* kErrSameChainID = "same chainID";
inline constexpr const char* kErrMismatchedNetIDs = "mismatched netIDs";

inline constexpr std::size_t kMinNameLen = 1;
inline constexpr std::size_t kMaxNameLen = 128;
inline constexpr std::size_t kMinSymbolLen = 1;
inline constexpr std::size_t kMaxSymbolLen = 4;
inline constexpr std::uint8_t kMaxDenomination = 32;

struct Config {
    std::uint64_t tx_fee = 0;
    std::uint64_t create_asset_tx_fee = 0;
};

// ParsedFx pairs an fx with the id the chain knows it by. Its POSITION in the
// backend's list is the fx index a CreateAssetTx declares.
struct ParsedFx {
    Id id{};
    std::shared_ptr<fx::Fx> fx;
};

// FxIndex maps an fx family to its position in the backend's fx list — the
// reflection-free replacement for Go's map[reflect.Type]int. A slot of -1 means
// no fx of that family is registered.
class FxIndex {
public:
    FxIndex() { by_kind_.fill(-1); }

    void set(wire::TypeKind kind, int idx) {
        if (std::size_t(kind) < by_kind_.size()) by_kind_[std::size_t(kind)] = idx;
    }
    // get returns the index, or -1 when the family is unregistered.
    int get(wire::TypeKind kind) const {
        if (std::size_t(kind) >= by_kind_.size()) return -1;
        return by_kind_[std::size_t(kind)];
    }
    // get_fx asks the VALUE which family it belongs to, then indexes.
    int get_fx(const fx::FxValue* v) const { return v == nullptr ? -1 : get(v->family()); }

private:
    std::array<int, 16> by_kind_{};
};

// SharedMemory is the cross-chain atomic surface: an import reads the UTXO bytes
// the exporting chain put there, keyed by UTXO id.
struct SharedMemory {
    virtual ~SharedMemory() = default;
    virtual Result<std::vector<Bytes>> get(const Id& peer_chain_id,
                                           const std::vector<Bytes>& keys) const = 0;
};

// NetLookup answers "which network is that chain on?" — what an import/export
// checks before trusting a peer chain id.
struct NetLookup {
    virtual ~NetLookup() = default;
    virtual Result<Id> network_of(const Id& chain_id) const = 0;
};

struct Backend {
    Config config;
    std::vector<ParsedFx> fxs;
    FxIndex fx_index;
    // The fee asset may differ from the chain's own asset when this VM runs as a
    // chain of its own.
    Id fee_asset_id{};
    bool bootstrapped = false;

    std::uint32_t network_id = 0;
    Id chain_id{};
    Id net_id{};

    SharedMemory* shared_memory = nullptr;
    NetLookup* net_lookup = nullptr;
};

// ---- pass 1: syntax ----
class SyntacticVerifier final : public txs::Visitor {
public:
    SyntacticVerifier(const Backend& backend, const txs::Tx& tx) : backend_(backend), tx_(tx) {}

    Result<void> base_tx(txs::BaseTx&) override;
    Result<void> create_asset_tx(txs::CreateAssetTx&) override;
    Result<void> operation_tx(txs::OperationTx&) override;
    Result<void> import_tx(txs::ImportTx&) override;
    Result<void> export_tx(txs::ExportTx&) override;

private:
    Result<void> verify_credential_count(int num_inputs) const;

    const Backend& backend_;
    const txs::Tx& tx_;
};

// ---- pass 2: the chain's own agreement ----
class SemanticVerifier final : public txs::Visitor {
public:
    SemanticVerifier(const Backend& backend, const state::ReadOnlyChain& chain, const txs::Tx& tx)
        : backend_(backend), chain_(chain), tx_(tx) {}

    Result<void> base_tx(txs::BaseTx&) override;
    Result<void> create_asset_tx(txs::CreateAssetTx&) override;
    Result<void> operation_tx(txs::OperationTx&) override;
    Result<void> import_tx(txs::ImportTx&) override;
    Result<void> export_tx(txs::ExportTx&) override;

private:
    Result<void> verify_base(txs::UnsignedTx& tx);
    Result<void> verify_transfer(txs::UnsignedTx& tx, const txs::TransferableInput& in,
                                 const fx::FxValue* cred);
    Result<void> verify_transfer_of_utxo(txs::UnsignedTx& tx, const txs::TransferableInput& in,
                                         const fx::FxValue* cred, const txs::UTXO& utxo);
    Result<void> verify_operation(txs::OperationTx& tx, const txs::Operation& op,
                                  const fx::FxValue* cred);
    Result<void> verify_fx_usage(int fx_index, const Id& asset_id);
    Result<int> get_fx(const fx::FxValue* v);
    Result<void> same_net(const Id& peer_chain_id);

    const Backend& backend_;
    const state::ReadOnlyChain& chain_;
    const txs::Tx& tx_;
};

// AtomicRequests are the cross-chain effects of a tx: what to remove from the
// source chain's shared memory (import) or put into the destination's (export).
struct AtomicElement {
    Bytes key;
    Bytes value;
    std::vector<Bytes> traits;
};
struct AtomicRequests {
    std::vector<Bytes> remove_requests;
    std::vector<AtomicElement> put_requests;
};

// ---- pass 3: apply ----
class Executor final : public txs::Visitor {
public:
    Executor(state::Chain& chain, const txs::Tx& tx) : chain_(chain), tx_(tx) {}

    Result<void> base_tx(txs::BaseTx&) override;
    Result<void> create_asset_tx(txs::CreateAssetTx&) override;
    Result<void> operation_tx(txs::OperationTx&) override;
    Result<void> import_tx(txs::ImportTx&) override;
    Result<void> export_tx(txs::ExportTx&) override;

    // inputs are the imported UTXO ids this tx consumed from shared memory.
    std::set<Id> inputs;
    std::map<Id, AtomicRequests> atomic_requests;

private:
    state::Chain& chain_;
    const txs::Tx& tx_;
};

}  // namespace lux::xvm::executor
