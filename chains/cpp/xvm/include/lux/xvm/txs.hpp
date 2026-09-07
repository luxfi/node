// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// txs.hpp — the five X-Chain transactions, and the UTXO vocabulary they move.
//
// The X-Chain is a UTXO DAG: a transaction CONSUMES previous outputs and
// PRODUCES new ones, and it is valid when its inputs are authorized to consume
// what they name and consume at least as much as they produce.
//
//   BaseTx         move value
//   CreateAssetTx  define a new asset and its per-fx genesis state
//   OperationTx    run fx operations (mint / NFT transfer / property burn)
//   ImportTx       consume UTXOs that arrived from another chain
//   ExportTx       emit UTXOs to another chain
//
// There is no codec and no version byte: an unsigned tx is a ZAP object whose
// byte 0 is the kind, and a signed tx is a SignedTx envelope carrying those
// bytes plus one credential per authorization. TxID = sha256(signed bytes), so
// the bytes are the transaction — never a re-encoding of a struct.

#pragma once

#include "lux/xvm/fx.hpp"
#include "lux/xvm/id.hpp"
#include "lux/xvm/wire.hpp"

#include <map>
#include <memory>
#include <optional>
#include <set>
#include <string>
#include <vector>

namespace lux::xvm::txs {

template <class T>
using Result = wire::Result<T>;

// ---- error sentinels (one per Go error) ----
inline constexpr const char* kErrNilTx = "nil tx is not valid";
inline constexpr const char* kErrWrongNetworkID = "tx has wrong network ID";
inline constexpr const char* kErrWrongChainID = "tx has wrong chain ID";
inline constexpr const char* kErrMemoTooLarge = "memo exceeds maximum length";
inline constexpr const char* kErrNilTransferableOutput = "nil transferable output is not valid";
inline constexpr const char* kErrNilTransferableFxOutput =
    "nil transferable feature extension output is not valid";
inline constexpr const char* kErrOutputsNotSorted = "outputs not sorted";
inline constexpr const char* kErrNilTransferableInput = "nil transferable input is not valid";
inline constexpr const char* kErrNilTransferableFxInput =
    "nil transferable feature extension input is not valid";
inline constexpr const char* kErrInputsNotSortedUnique = "inputs not sorted and unique";
inline constexpr const char* kErrInsufficientFunds = "insufficient funds";
inline constexpr const char* kErrEmptyAssetID = "empty asset ID is not valid";
inline constexpr const char* kErrNilInitialState = "nil initial state is not valid";
inline constexpr const char* kErrNilFxOutput = "nil feature extension output is not valid";
inline constexpr const char* kErrInitialOutputsNotSorted = "outputs not sorted";
inline constexpr const char* kErrUnknownFx = "unknown feature extension";
inline constexpr const char* kErrNilOperation = "nil operation is not valid";
inline constexpr const char* kErrNilFxOperation = "nil fx operation is not valid";
inline constexpr const char* kErrNotSortedAndUniqueUTXOIDs = "utxo IDs not sorted and unique";

inline constexpr std::size_t kMaxMemoSize = 256;

// ---- xkind: the 1-byte discriminator at object offset 0 of every unsigned tx.
// It is the whole dispatch — no codec, no version, no slot map. The values are
// the old linearcodec registration order shifted by one, so 0 stays a sentinel
// that never appears on the wire.
enum class XKind : std::uint8_t {
    Reserved = 0,
    Base = 1,
    CreateAsset = 2,
    Operation = 3,
    Import = 4,
    Export = 5,
};

// ================= the UTXO vocabulary =================

struct UTXOID {
    Id tx_id{};
    std::uint32_t output_index = 0;
    // symbol marks an input that is NOT a row in this chain's UTXO table (an
    // imported one). It never reaches the wire.
    bool symbol = false;

    // input_id is the unique id of the UTXO being spent: sha256 of the output
    // index as a big-endian prefix over the tx id.
    Id input_id() const { return prefix_id(tx_id, std::uint64_t(output_index)); }
    int compare(const UTXOID& other) const;
    bool operator==(const UTXOID& o) const {
        return tx_id == o.tx_id && output_index == o.output_index;
    }
};

struct TransferableOutput {
    Id asset_id{};
    std::shared_ptr<fx::FxTransferOut> out;

    Result<void> verify() const;
    std::uint64_t amount() const { return out ? out->amount() : 0; }
};

struct TransferableInput {
    UTXOID utxo_id;
    Id asset_id{};
    std::shared_ptr<fx::FxTransferIn> in;

    Result<void> verify() const;
    std::uint64_t amount() const { return in ? in->amount() : 0; }
};

struct UTXO {
    UTXOID utxo_id;
    Id asset_id{};
    std::shared_ptr<fx::FxOutput> out;

    Result<void> verify() const;
    // wire_bytes is the ZAP-native envelope this UTXO travels and rests as —
    // the SAME bytes on shared memory and on disk, which is why an exported UTXO
    // needs no re-encoding when it is imported.
    Result<Bytes> wire_bytes() const;
};
Result<UTXO> parse_utxo(ByteView b);

// sort_transferable_outputs orders by (AssetID, inner fx wire bytes) — the bytes
// that reach the wire are the sort key, so there is no second encoding to keep
// in step.
void sort_transferable_outputs(std::vector<TransferableOutput>& outs);
bool is_sorted_transferable_outputs(const std::vector<TransferableOutput>& outs);
bool is_sorted_and_unique_inputs(const std::vector<TransferableInput>& ins);

// FlowChecker sums what a tx consumes and produces per asset. A tx is funded
// when it produces no more of any asset than it consumed.
class FlowChecker {
public:
    void consume(const Id& asset_id, std::uint64_t amount);
    void produce(const Id& asset_id, std::uint64_t amount);
    Result<void> verify() const;

private:
    void add(std::map<Id, std::uint64_t>& m, const Id& asset_id, std::uint64_t amount);

    std::map<Id, std::uint64_t> consumed_, produced_;
    std::vector<std::string> errs_;
};

// verify_tx flow-checks a transaction including its fee, and enforces that
// outputs are sorted and inputs sorted-and-unique.
Result<void> verify_tx(std::uint64_t fee_amount, const Id& fee_asset_id,
                       const std::vector<const std::vector<TransferableInput>*>& all_ins,
                       const std::vector<const std::vector<TransferableOutput>*>& all_outs);

// ================= the tx pieces =================

// InitialState is the per-fx genesis state a CreateAssetTx installs: an fx index
// plus that fx's initial output set. The FxID is recovered from each output's
// own envelope discriminator, so it is not stored on the wire.
// Go carries an FxID beside the FxIndex here and fills it in a separate pass
// (tx_init.go), for its JSON API to print. This port does not: an fx value
// already SAYS which family it belongs to, so a second copy would be a field
// that is empty until someone remembers to populate it — the exact shape of
// bug the closed-sum dispatch was chosen to make unrepresentable.
struct InitialState {
    std::uint32_t fx_index = 0;
    std::vector<std::shared_ptr<fx::FxOutput>> outs;

    Bytes bytes() const;
    Result<void> verify(int num_fxs) const;
    int compare(const InitialState& other) const;
    void sort();
};
Result<InitialState> parse_initial_state(ByteView b);

// Operation runs one fx operation over a set of consumed UTXOs of one asset.
struct Operation {
    Id asset_id{};
    std::vector<UTXOID> utxo_ids;
    std::shared_ptr<fx::FxOperation> op;

    Bytes bytes() const;
    Result<void> verify() const;
};
Result<Operation> parse_operation(ByteView b);

void sort_operations(std::vector<Operation>& ops);
bool is_sorted_and_unique_operations(const std::vector<Operation>& ops);

// ================= the five transactions =================

// The multi-asset spending envelope every X-Chain tx carries. X-Chain is a
// universal multi-fx settlement layer, so an output names the asset it moves.
struct BaseTxFields {
    std::uint32_t network_id = 0;
    Id blockchain_id{};
    std::vector<TransferableOutput> outs;
    std::vector<TransferableInput> ins;
    Bytes memo;

    // verify checks the tx METADATA against the chain it claims to be on.
    Result<void> verify(std::uint32_t expect_network_id, const Id& expect_chain_id) const;
    Bytes wire_envelope() const;
};

struct Visitor;

struct UnsignedTx {
    virtual ~UnsignedTx() = default;

    BaseTxFields base;

    virtual XKind kind() const = 0;
    virtual Bytes serialize() const = 0;
    virtual Result<void> visit(Visitor& v) = 0;
    virtual int num_credentials() const { return int(base.ins.size()); }
    virtual std::vector<UTXOID> input_utxos() const;
    std::set<Id> input_ids() const;

    // bytes returns the cached unsigned wire bytes, serializing on first use.
    // This is the SIGNING TARGET — every fx signature is over sha256 of it.
    const Bytes& bytes() const;
    void set_bytes(Bytes b) { bytes_ = std::move(b); }

protected:
    mutable Bytes bytes_;
};

struct BaseTx final : UnsignedTx {
    XKind kind() const override { return XKind::Base; }
    Bytes serialize() const override;
    Result<void> visit(Visitor& v) override;
};

struct CreateAssetTx final : UnsignedTx {
    std::string name;
    std::string symbol;
    std::uint8_t denomination = 0;
    std::vector<InitialState> states;

    XKind kind() const override { return XKind::CreateAsset; }
    Bytes serialize() const override;
    Result<void> visit(Visitor& v) override;
};

struct OperationTx final : UnsignedTx {
    std::vector<Operation> ops;

    XKind kind() const override { return XKind::Operation; }
    Bytes serialize() const override;
    Result<void> visit(Visitor& v) override;
    int num_credentials() const override { return int(base.ins.size() + ops.size()); }
    std::vector<UTXOID> input_utxos() const override;
};

struct ImportTx final : UnsignedTx {
    Id source_chain{};
    std::vector<TransferableInput> imported_ins;

    XKind kind() const override { return XKind::Import; }
    Bytes serialize() const override;
    Result<void> visit(Visitor& v) override;
    int num_credentials() const override { return int(base.ins.size() + imported_ins.size()); }
    std::vector<UTXOID> input_utxos() const override;
};

struct ExportTx final : UnsignedTx {
    Id destination_chain{};
    std::vector<TransferableOutput> exported_outs;

    XKind kind() const override { return XKind::Export; }
    Bytes serialize() const override;
    Result<void> visit(Visitor& v) override;
};

// Visitor lets the executor layer act on the concrete tx type without the tx
// layer knowing what execution means.
struct Visitor {
    virtual ~Visitor() = default;
    virtual Result<void> base_tx(BaseTx&) = 0;
    virtual Result<void> create_asset_tx(CreateAssetTx&) = 0;
    virtual Result<void> operation_tx(OperationTx&) = 0;
    virtual Result<void> import_tx(ImportTx&) = 0;
    virtual Result<void> export_tx(ExportTx&) = 0;
};

// ================= the signed transaction =================

struct Tx {
    std::shared_ptr<UnsignedTx> unsigned_tx;
    std::vector<std::shared_ptr<fx::FxCredential>> creds;

    Id tx_id{};

    // initialize binds the cached bytes and TxID from the unsigned tx and its
    // credentials. Used for txs built in-process; a parsed tx already has both.
    Result<void> initialize();
    void set_bytes(Bytes unsigned_bytes, Bytes signed_bytes);

    const Id& id() const { return tx_id; }
    const Bytes& bytes() const { return bytes_; }
    std::size_t size() const { return bytes_.size(); }

    // utxos returns the UTXOs this tx PRODUCES, in the order the executor writes
    // them (which is what fixes their output indices).
    std::vector<UTXO> utxos() const;
    std::set<Id> input_ids() const { return unsigned_tx->input_ids(); }

    Bytes bytes_;
};

// parse decodes a signed X-Chain tx: the SignedTx envelope, its unsigned body
// dispatched on the xkind byte, and one credential per authorization dispatched
// on its own envelope discriminator. TxID = sha256(signed bytes).
Result<std::shared_ptr<Tx>> parse(ByteView signed_bytes);
Result<std::shared_ptr<UnsignedTx>> parse_unsigned(ByteView unsigned_bytes);

// The three questions the node's pool asks about a transaction, and all it may
// ask. They are declared beside the transaction rather than beside the pool
// because the pool must not know what an X-Chain transaction is.
inline Id pool_id(const std::shared_ptr<Tx>& tx) { return tx->id(); }
inline std::size_t pool_size(const std::shared_ptr<Tx>& tx) { return tx->size(); }
inline std::set<Id> pool_inputs(const std::shared_ptr<Tx>& tx) { return tx->input_ids(); }

}  // namespace lux::xvm::txs
