// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/txs.hpp"

#include <algorithm>
#include <cstring>
#include <limits>

namespace lux::xvm::txs {
namespace {

// ---- the unsigned-tx object layout, shared by every X-chain tx type ----
constexpr int kOffXKind = 0;    // u8 — the whole dispatch
constexpr int kOffBaseTx = 8;   // bytes ptr: the XVMBaseTx envelope
constexpr int kSizeBaseObj = 16;

constexpr int kOffCAName = 16;
constexpr int kOffCASymbol = 24;
constexpr int kOffCADenom = 32;
constexpr int kOffCAStatesLen = 36;
constexpr int kOffCAStatesBlob = 44;
constexpr int kSizeCA = 52;

constexpr int kOffOpsLen = 16;
constexpr int kOffOpsBlob = 24;
constexpr int kSizeOpTx = 32;

constexpr int kOffImportSource = 16;
constexpr int kOffImportIns = 48;
constexpr int kSizeImport = 56;

constexpr int kOffExportDest = 16;
constexpr int kOffExportOuts = 48;
constexpr int kSizeExport = 56;

constexpr int kOffOpAsset = 0;
constexpr int kOffOpUTXOIDs = 32;
constexpr int kOffOpFxOp = 40;
constexpr int kSizeOp = 48;

constexpr int kOffISFxIndex = 0;
constexpr int kOffISOutsLen = 4;
constexpr int kOffISOutsBlob = 12;
constexpr int kSizeIS = 20;

constexpr std::uint32_t kItemLenStride = 4;
constexpr std::uint32_t kUTXOIDStride = 36;

// ---- packed blob list: a u32 length list plus a concatenated blob ----

struct BlobList {
    int len_off = 0;
    int len_count = 0;
    Bytes blob;
};

BlobList write_blob_list(zap::Builder& b, const std::vector<Bytes>& bufs) {
    BlobList out;
    if (bufs.empty()) return out;
    auto lb = b.start_list(int(kItemLenStride));
    for (const auto& buf : bufs) {
        lb.add_u32(std::uint32_t(buf.size()));
        out.blob.insert(out.blob.end(), buf.begin(), buf.end());
    }
    auto [off, count] = lb.finish();
    out.len_off = off;
    out.len_count = count;
    return out;
}

Result<std::vector<ByteView>> read_blob_list(const zap::Object& obj, int len_ptr_off,
                                             int blob_ptr_off) {
    auto lengths = obj.list_stride(len_ptr_off, kItemLenStride);
    int n = lengths.size();
    if (n == 0) return std::vector<ByteView>{};
    ByteView blob = obj.bytes(blob_ptr_off);
    std::vector<ByteView> out;
    out.reserve(std::size_t(n));
    std::size_t cursor = 0;
    for (int i = 0; i < n; ++i) {
        std::size_t size = std::size_t(lengths.u32(i));
        if (cursor + size > blob.size())
            return std::unexpected("xvm txs: element length overruns blob");
        out.push_back(blob.subspan(cursor, size));
        cursor += size;
    }
    return out;
}

// ---- UTXOID fixed-stride list (36 bytes: TxID 32B + OutputIndex u32 LE) ----

std::pair<int, int> write_utxo_ids(zap::Builder& b, const std::vector<UTXOID>& ids) {
    if (ids.empty()) return {0, 0};
    auto lb = b.start_list(int(kUTXOIDStride));
    for (const auto& u : ids) {
        std::array<std::uint8_t, kUTXOIDStride> e{};
        std::memcpy(e.data(), u.tx_id.data(), 32);
        e[32] = std::uint8_t(u.output_index);
        e[33] = std::uint8_t(u.output_index >> 8);
        e[34] = std::uint8_t(u.output_index >> 16);
        e[35] = std::uint8_t(u.output_index >> 24);
        lb.add_bytes(view(e));
    }
    auto [off, _] = lb.finish();
    return {off, int(ids.size())};
}

std::vector<UTXOID> read_utxo_ids(const zap::Object& obj, int ptr_off) {
    auto list = obj.list_stride(ptr_off, kUTXOIDStride);
    std::vector<UTXOID> out(static_cast<std::size_t>(list.size()));
    for (int i = 0; i < list.size(); ++i) {
        auto e = list.object(i, int(kUTXOIDStride));
        UTXOID u;
        auto s = e.bytes_fixed(0, 32);
        if (s.size() == 32) std::memcpy(u.tx_id.data(), s.data(), 32);
        u.output_index = e.u32(32);
        out[std::size_t(i)] = u;
    }
    return out;
}

Id id_at(const zap::Object& obj, int off) {
    Id out{};
    auto s = obj.bytes_fixed(off, 32);
    if (s.size() == 32) std::memcpy(out.data(), s.data(), 32);
    return out;
}

// ---- the BaseTx wire envelope, both directions ----

Result<TransferableOutput> output_from_wire(const wire::TransferableOut& w) {
    auto out = fx::wrap_transfer_out(w.output_bytes());
    if (!out) return std::unexpected(out.error());
    return TransferableOutput{w.asset_id(), *out};
}

Result<TransferableInput> input_from_wire(const wire::TransferableIn& w) {
    auto in = fx::wrap_transfer_in(w.input_bytes());
    if (!in) return std::unexpected(in.error());
    return TransferableInput{UTXOID{w.tx_id(), w.output_index(), false}, w.asset_id(), *in};
}

Result<BaseTxFields> decode_base_tx_wire(const zap::Object& obj) {
    auto w = wire::wrap_xvm_base_tx(obj.bytes(kOffBaseTx));
    if (!w) return std::unexpected(w.error());
    BaseTxFields base;
    base.network_id = w->network_id();
    base.blockchain_id = w->blockchain_id();
    for (std::uint32_t i = 0; i < w->outs_count(); ++i) {
        auto wo = w->out_at(i);
        if (!wo) return std::unexpected(wo.error());
        auto out = output_from_wire(*wo);
        if (!out) return std::unexpected(out.error());
        base.outs.push_back(*out);
    }
    for (std::uint32_t i = 0; i < w->ins_count(); ++i) {
        auto wi = w->in_at(i);
        if (!wi) return std::unexpected(wi.error());
        auto in = input_from_wire(*wi);
        if (!in) return std::unexpected(in.error());
        base.ins.push_back(*in);
    }
    auto m = w->memo();
    base.memo.assign(m.begin(), m.end());
    return base;
}

}  // namespace

// ================= the UTXO vocabulary =================

int UTXOID::compare(const UTXOID& other) const {
    int c = std::memcmp(tx_id.data(), other.tx_id.data(), tx_id.size());
    if (c != 0) return c;
    if (output_index < other.output_index) return -1;
    if (output_index > other.output_index) return 1;
    return 0;
}

Result<void> TransferableOutput::verify() const {
    if (out == nullptr) return std::unexpected(kErrNilTransferableFxOutput);
    if (asset_id == kEmptyId) return std::unexpected(kErrEmptyAssetID);
    return out->verify();
}

Result<void> TransferableInput::verify() const {
    if (in == nullptr) return std::unexpected(kErrNilTransferableFxInput);
    if (asset_id == kEmptyId) return std::unexpected(kErrEmptyAssetID);
    return in->verify();
}

Result<void> UTXO::verify() const {
    if (out == nullptr) return std::unexpected("empty utxo is not valid");
    if (asset_id == kEmptyId) return std::unexpected(kErrEmptyAssetID);
    return out->verify();
}

Result<Bytes> UTXO::wire_bytes() const {
    if (out == nullptr) return std::unexpected("empty utxo is not valid");
    return wire::new_utxo(
        wire::UTXOInput{utxo_id.tx_id, utxo_id.output_index, asset_id, out->bytes()});
}

Result<UTXO> parse_utxo(ByteView b) {
    auto w = wire::wrap_utxo(b);
    if (!w) return std::unexpected(w.error());
    auto out = fx::wrap_output(w->output_bytes());
    if (!out) return std::unexpected(out.error());
    return UTXO{UTXOID{w->tx_id(), w->output_index(), false}, w->asset_id(), *out};
}

namespace {

// The canonical output order: asset id first, then the inner fx envelope's own
// bytes. Both are what reaches the wire — there is no second encoding.
bool output_less(const TransferableOutput& a, const TransferableOutput& b) {
    int c = std::memcmp(a.asset_id.data(), b.asset_id.data(), a.asset_id.size());
    if (c != 0) return c < 0;
    Bytes ab = a.out ? a.out->bytes() : Bytes{};
    Bytes bb = b.out ? b.out->bytes() : Bytes{};
    return ab < bb;
}

}  // namespace

void sort_transferable_outputs(std::vector<TransferableOutput>& outs) {
    std::stable_sort(outs.begin(), outs.end(), output_less);
}

bool is_sorted_transferable_outputs(const std::vector<TransferableOutput>& outs) {
    return std::is_sorted(outs.begin(), outs.end(), output_less);
}

bool is_sorted_and_unique_inputs(const std::vector<TransferableInput>& ins) {
    for (std::size_t i = 0; i + 1 < ins.size(); ++i) {
        if (ins[i].utxo_id.compare(ins[i + 1].utxo_id) >= 0) return false;
    }
    return true;
}

// ---- FlowChecker ----

void FlowChecker::add(std::map<Id, std::uint64_t>& m, const Id& asset_id, std::uint64_t amount) {
    auto& slot = m[asset_id];
    if (amount > std::numeric_limits<std::uint64_t>::max() - slot) {
        errs_.emplace_back("overflow");
        return;
    }
    slot += amount;
}

void FlowChecker::consume(const Id& asset_id, std::uint64_t amount) {
    add(consumed_, asset_id, amount);
}
void FlowChecker::produce(const Id& asset_id, std::uint64_t amount) {
    add(produced_, asset_id, amount);
}

Result<void> FlowChecker::verify() const {
    if (!errs_.empty()) return std::unexpected(errs_.front());
    for (const auto& [asset_id, produced] : produced_) {
        auto it = consumed_.find(asset_id);
        std::uint64_t consumed = it == consumed_.end() ? 0 : it->second;
        if (produced > consumed) return std::unexpected(kErrInsufficientFunds);
    }
    return {};
}

Result<void> verify_tx(std::uint64_t fee_amount, const Id& fee_asset_id,
                       const std::vector<const std::vector<TransferableInput>*>& all_ins,
                       const std::vector<const std::vector<TransferableOutput>*>& all_outs) {
    FlowChecker fc;
    fc.produce(fee_asset_id, fee_amount);  // the tx fee must be burned

    for (const auto* outs : all_outs) {
        for (const auto& out : *outs) {
            if (auto r = out.verify(); !r) return r;
            fc.produce(out.asset_id, out.amount());
        }
        if (!is_sorted_transferable_outputs(*outs)) return std::unexpected(kErrOutputsNotSorted);
    }
    for (const auto* ins : all_ins) {
        for (const auto& in : *ins) {
            if (auto r = in.verify(); !r) return r;
            fc.consume(in.asset_id, in.amount());
        }
        if (!is_sorted_and_unique_inputs(*ins)) return std::unexpected(kErrInputsNotSortedUnique);
    }
    return fc.verify();
}

// ================= InitialState =================

Bytes InitialState::bytes() const {
    std::vector<Bytes> out_bufs;
    out_bufs.reserve(outs.size());
    for (const auto& o : outs) out_bufs.push_back(o ? o->bytes() : Bytes{});

    zap::Builder b(zap::kHeaderSize + kSizeIS + 256);
    auto bl = write_blob_list(b, out_bufs);

    auto ob = b.start_object(kSizeIS);
    ob.set_u32(kOffISFxIndex, fx_index);
    ob.set_list(kOffISOutsLen, bl.len_off, bl.len_count);
    ob.set_bytes(kOffISOutsBlob, view(bl.blob));
    ob.finish_as_root();
    return b.finish();
}

Result<InitialState> parse_initial_state(ByteView b) {
    const auto msg = zap::Message::parse(b);
    if (!msg) return std::unexpected(std::string(zap::describe(msg.error())));
    auto obj = msg->root();
    auto bufs = read_blob_list(obj, kOffISOutsLen, kOffISOutsBlob);
    if (!bufs) return std::unexpected(bufs.error());
    InitialState is;
    is.fx_index = obj.u32(kOffISFxIndex);
    for (const auto& env : *bufs) {
        auto out = fx::wrap_output(env);
        if (!out) return std::unexpected(out.error());
        is.outs.push_back(*out);
    }
    return is;
}

Result<void> InitialState::verify(int num_fxs) const {
    if (fx_index >= std::uint32_t(num_fxs)) return std::unexpected(kErrUnknownFx);
    for (const auto& out : outs) {
        if (out == nullptr) return std::unexpected(kErrNilFxOutput);
        if (auto r = out->verify(); !r) return r;
    }
    // The outputs must be in canonical wire order, so an asset's genesis state
    // has exactly one encoding.
    for (std::size_t i = 0; i + 1 < outs.size(); ++i) {
        if (!(outs[i]->bytes() < outs[i + 1]->bytes()))
            return std::unexpected(kErrInitialOutputsNotSorted);
    }
    return {};
}

int InitialState::compare(const InitialState& other) const {
    if (fx_index < other.fx_index) return -1;
    if (fx_index > other.fx_index) return 1;
    return 0;
}

void InitialState::sort() {
    std::stable_sort(outs.begin(), outs.end(),
                     [](const std::shared_ptr<fx::FxOutput>& a,
                        const std::shared_ptr<fx::FxOutput>& b) {
                         Bytes ab = a ? a->bytes() : Bytes{};
                         Bytes bb = b ? b->bytes() : Bytes{};
                         return ab < bb;
                     });
}

// ================= Operation =================

Bytes Operation::bytes() const {
    Bytes fx_op = op ? op->bytes() : Bytes{};
    zap::Builder b(zap::kHeaderSize + kSizeOp + int(fx_op.size()) + 256);
    auto [utxo_off, utxo_count] = write_utxo_ids(b, utxo_ids);

    auto ob = b.start_object(kSizeOp);
    ob.set_bytes_fixed(kOffOpAsset, view(asset_id));
    ob.set_list(kOffOpUTXOIDs, utxo_off, utxo_count);
    ob.set_bytes(kOffOpFxOp, view(fx_op));
    ob.finish_as_root();
    return b.finish();
}

Result<Operation> parse_operation(ByteView b) {
    const auto msg = zap::Message::parse(b);
    if (!msg) return std::unexpected(std::string(zap::describe(msg.error())));
    auto obj = msg->root();
    auto fx_op = fx::wrap_operation(obj.bytes(kOffOpFxOp));
    if (!fx_op) return std::unexpected(fx_op.error());
    Operation op;
    op.asset_id = id_at(obj, kOffOpAsset);
    op.utxo_ids = read_utxo_ids(obj, kOffOpUTXOIDs);
    op.op = *fx_op;
    return op;
}

Result<void> Operation::verify() const {
    if (op == nullptr) return std::unexpected(kErrNilFxOperation);
    for (std::size_t i = 0; i + 1 < utxo_ids.size(); ++i) {
        if (utxo_ids[i].compare(utxo_ids[i + 1]) >= 0)
            return std::unexpected(kErrNotSortedAndUniqueUTXOIDs);
    }
    if (asset_id == kEmptyId) return std::unexpected(kErrEmptyAssetID);
    return op->verify();
}

void sort_operations(std::vector<Operation>& ops) {
    std::stable_sort(ops.begin(), ops.end(),
                     [](const Operation& a, const Operation& b) { return a.bytes() < b.bytes(); });
}

bool is_sorted_and_unique_operations(const std::vector<Operation>& ops) {
    for (std::size_t i = 0; i + 1 < ops.size(); ++i) {
        if (!(ops[i].bytes() < ops[i + 1].bytes())) return false;
    }
    return true;
}

// ================= BaseTxFields =================

Result<void> BaseTxFields::verify(std::uint32_t expect_network_id, const Id& expect_chain_id) const {
    if (network_id != expect_network_id) return std::unexpected(kErrWrongNetworkID);
    if (blockchain_id != expect_chain_id) return std::unexpected(kErrWrongChainID);
    if (memo.size() > kMaxMemoSize) return std::unexpected(kErrMemoTooLarge);
    return {};
}

Bytes BaseTxFields::wire_envelope() const {
    wire::XVMBaseTxInput in;
    in.network_id = network_id;
    in.blockchain_id = blockchain_id;
    in.outs.reserve(outs.size());
    for (const auto& o : outs)
        in.outs.push_back(wire::XVMTransferOut{o.asset_id, o.out ? o.out->bytes() : Bytes{}});
    in.ins.reserve(ins.size());
    for (const auto& i : ins)
        in.ins.push_back(wire::XVMTransferIn{i.utxo_id.tx_id, i.utxo_id.output_index, i.asset_id,
                                             i.in ? i.in->bytes() : Bytes{}});
    in.memo = memo;
    return wire::new_xvm_base_tx(in);
}

// ================= UnsignedTx =================

const Bytes& UnsignedTx::bytes() const {
    if (bytes_.empty()) bytes_ = serialize();
    return bytes_;
}

std::vector<UTXOID> UnsignedTx::input_utxos() const {
    std::vector<UTXOID> out;
    out.reserve(base.ins.size());
    for (const auto& in : base.ins) out.push_back(in.utxo_id);
    return out;
}

std::set<Id> UnsignedTx::input_ids() const {
    std::set<Id> out;
    for (const auto& u : input_utxos()) out.insert(u.input_id());
    return out;
}

std::vector<UTXOID> OperationTx::input_utxos() const {
    auto out = UnsignedTx::input_utxos();
    for (const auto& op : ops) out.insert(out.end(), op.utxo_ids.begin(), op.utxo_ids.end());
    return out;
}

std::vector<UTXOID> ImportTx::input_utxos() const {
    auto out = UnsignedTx::input_utxos();
    for (const auto& in : imported_ins) {
        UTXOID u = in.utxo_id;
        u.symbol = true;  // an imported input is not a row in this chain's table
        out.push_back(u);
    }
    return out;
}

// ---- serialize: one object per tx kind, the kind byte at offset 0 ----

Bytes BaseTx::serialize() const {
    Bytes env = base.wire_envelope();
    zap::Builder b(zap::kHeaderSize + kSizeBaseObj + int(env.size()) + 64);
    auto ob = b.start_object(kSizeBaseObj);
    ob.set_u8(kOffXKind, std::uint8_t(XKind::Base));
    ob.set_bytes(kOffBaseTx, view(env));
    ob.finish_as_root();
    return b.finish();
}

Bytes CreateAssetTx::serialize() const {
    Bytes env = base.wire_envelope();
    std::vector<Bytes> state_bufs;
    state_bufs.reserve(states.size());
    for (const auto& s : states) state_bufs.push_back(s.bytes());

    zap::Builder b(zap::kHeaderSize + kSizeCA + int(env.size()) + 256);
    auto bl = write_blob_list(b, state_bufs);

    auto ob = b.start_object(kSizeCA);
    ob.set_u8(kOffXKind, std::uint8_t(XKind::CreateAsset));
    ob.set_bytes(kOffBaseTx, view(env));
    ob.set_text(kOffCAName, name);
    ob.set_text(kOffCASymbol, symbol);
    ob.set_u8(kOffCADenom, denomination);
    ob.set_list(kOffCAStatesLen, bl.len_off, bl.len_count);
    ob.set_bytes(kOffCAStatesBlob, view(bl.blob));
    ob.finish_as_root();
    return b.finish();
}

Bytes OperationTx::serialize() const {
    Bytes env = base.wire_envelope();
    std::vector<Bytes> op_bufs;
    op_bufs.reserve(ops.size());
    for (const auto& op : ops) op_bufs.push_back(op.bytes());

    zap::Builder b(zap::kHeaderSize + kSizeOpTx + int(env.size()) + 256);
    auto bl = write_blob_list(b, op_bufs);

    auto ob = b.start_object(kSizeOpTx);
    ob.set_u8(kOffXKind, std::uint8_t(XKind::Operation));
    ob.set_bytes(kOffBaseTx, view(env));
    ob.set_list(kOffOpsLen, bl.len_off, bl.len_count);
    ob.set_bytes(kOffOpsBlob, view(bl.blob));
    ob.finish_as_root();
    return b.finish();
}

Bytes ImportTx::serialize() const {
    Bytes env = base.wire_envelope();
    zap::Builder b(zap::kHeaderSize + kSizeImport + int(env.size()) + 256);
    std::vector<int> offs(imported_ins.size());
    for (std::size_t i = 0; i < imported_ins.size(); ++i) {
        const auto& in = imported_ins[i];
        Bytes inner = in.in ? in.in->bytes() : Bytes{};
        offs[i] = wire::append_transferable_in(b, in.utxo_id.tx_id, in.utxo_id.output_index,
                                               in.asset_id, view(inner));
    }
    auto lb = b.start_list(4);
    for (int off : offs) lb.add_object_ptr(off);
    auto [ins_off, ins_len] = lb.finish();

    auto ob = b.start_object(kSizeImport);
    ob.set_u8(kOffXKind, std::uint8_t(XKind::Import));
    ob.set_bytes(kOffBaseTx, view(env));
    ob.set_bytes_fixed(kOffImportSource, view(source_chain));
    ob.set_list(kOffImportIns, ins_off, ins_len);
    ob.finish_as_root();
    return b.finish();
}

Bytes ExportTx::serialize() const {
    Bytes env = base.wire_envelope();
    zap::Builder b(zap::kHeaderSize + kSizeExport + int(env.size()) + 256);
    std::vector<int> offs(exported_outs.size());
    for (std::size_t i = 0; i < exported_outs.size(); ++i) {
        const auto& o = exported_outs[i];
        Bytes inner = o.out ? o.out->bytes() : Bytes{};
        offs[i] = wire::append_transferable_out(b, o.asset_id, view(inner));
    }
    auto lb = b.start_list(4);
    for (int off : offs) lb.add_object_ptr(off);
    auto [outs_off, outs_len] = lb.finish();

    auto ob = b.start_object(kSizeExport);
    ob.set_u8(kOffXKind, std::uint8_t(XKind::Export));
    ob.set_bytes(kOffBaseTx, view(env));
    ob.set_bytes_fixed(kOffExportDest, view(destination_chain));
    ob.set_list(kOffExportOuts, outs_off, outs_len);
    ob.finish_as_root();
    return b.finish();
}

Result<void> BaseTx::visit(Visitor& v) { return v.base_tx(*this); }
Result<void> CreateAssetTx::visit(Visitor& v) { return v.create_asset_tx(*this); }
Result<void> OperationTx::visit(Visitor& v) { return v.operation_tx(*this); }
Result<void> ImportTx::visit(Visitor& v) { return v.import_tx(*this); }
Result<void> ExportTx::visit(Visitor& v) { return v.export_tx(*this); }

// ================= parse =================

namespace {

Result<std::shared_ptr<UnsignedTx>> parse_base(ByteView unsigned_bytes, const zap::Object& obj) {
    auto base = decode_base_tx_wire(obj);
    if (!base) return std::unexpected(base.error());
    auto tx = std::make_shared<BaseTx>();
    tx->base = std::move(*base);
    tx->set_bytes(Bytes(unsigned_bytes.begin(), unsigned_bytes.end()));
    return tx;
}

Result<std::shared_ptr<UnsignedTx>> parse_create_asset(ByteView unsigned_bytes,
                                                       const zap::Object& obj) {
    auto base = decode_base_tx_wire(obj);
    if (!base) return std::unexpected(base.error());
    auto bufs = read_blob_list(obj, kOffCAStatesLen, kOffCAStatesBlob);
    if (!bufs) return std::unexpected(bufs.error());
    auto tx = std::make_shared<CreateAssetTx>();
    tx->base = std::move(*base);
    tx->name = std::string(obj.text(kOffCAName));
    tx->symbol = std::string(obj.text(kOffCASymbol));
    tx->denomination = obj.u8(kOffCADenom);
    for (const auto& buf : *bufs) {
        auto s = parse_initial_state(buf);
        if (!s) return std::unexpected(s.error());
        tx->states.push_back(std::move(*s));
    }
    tx->set_bytes(Bytes(unsigned_bytes.begin(), unsigned_bytes.end()));
    return tx;
}

Result<std::shared_ptr<UnsignedTx>> parse_operation_tx(ByteView unsigned_bytes,
                                                       const zap::Object& obj) {
    auto base = decode_base_tx_wire(obj);
    if (!base) return std::unexpected(base.error());
    auto bufs = read_blob_list(obj, kOffOpsLen, kOffOpsBlob);
    if (!bufs) return std::unexpected(bufs.error());
    auto tx = std::make_shared<OperationTx>();
    tx->base = std::move(*base);
    for (const auto& buf : *bufs) {
        auto op = parse_operation(buf);
        if (!op) return std::unexpected(op.error());
        tx->ops.push_back(std::move(*op));
    }
    tx->set_bytes(Bytes(unsigned_bytes.begin(), unsigned_bytes.end()));
    return tx;
}

Result<std::shared_ptr<UnsignedTx>> parse_import(ByteView unsigned_bytes, const zap::Object& obj) {
    auto base = decode_base_tx_wire(obj);
    if (!base) return std::unexpected(base.error());
    auto tx = std::make_shared<ImportTx>();
    tx->base = std::move(*base);
    tx->source_chain = id_at(obj, kOffImportSource);
    auto l = obj.list_stride(kOffImportIns, 4);
    for (int i = 0; i < l.size(); ++i) {
        wire::TransferableIn w(l.object_ptr(i));
        auto in = input_from_wire(w);
        if (!in) return std::unexpected(in.error());
        tx->imported_ins.push_back(*in);
    }
    tx->set_bytes(Bytes(unsigned_bytes.begin(), unsigned_bytes.end()));
    return tx;
}

Result<std::shared_ptr<UnsignedTx>> parse_export(ByteView unsigned_bytes, const zap::Object& obj) {
    auto base = decode_base_tx_wire(obj);
    if (!base) return std::unexpected(base.error());
    auto tx = std::make_shared<ExportTx>();
    tx->base = std::move(*base);
    tx->destination_chain = id_at(obj, kOffExportDest);
    auto l = obj.list_stride(kOffExportOuts, 4);
    for (int i = 0; i < l.size(); ++i) {
        wire::TransferableOut w(l.object_ptr(i));
        auto out = output_from_wire(w);
        if (!out) return std::unexpected(out.error());
        tx->exported_outs.push_back(*out);
    }
    tx->set_bytes(Bytes(unsigned_bytes.begin(), unsigned_bytes.end()));
    return tx;
}

}  // namespace

Result<std::shared_ptr<UnsignedTx>> parse_unsigned(ByteView unsigned_bytes) {
    const auto msg = zap::Message::parse(unsigned_bytes);
    if (!msg) return std::unexpected(std::string(zap::describe(msg.error())));
    auto obj = msg->root();
    switch (XKind(obj.u8(kOffXKind))) {
        case XKind::Base:
            return parse_base(unsigned_bytes, obj);
        case XKind::CreateAsset:
            return parse_create_asset(unsigned_bytes, obj);
        case XKind::Operation:
            return parse_operation_tx(unsigned_bytes, obj);
        case XKind::Import:
            return parse_import(unsigned_bytes, obj);
        case XKind::Export:
            return parse_export(unsigned_bytes, obj);
        default:
            return std::unexpected("xvm txs: unknown tx kind");
    }
}

Result<std::shared_ptr<Tx>> parse(ByteView signed_bytes) {
    auto st = wire::wrap_signed_tx(signed_bytes);
    if (!st) return std::unexpected("couldn't parse signed tx: " + st.error());
    auto unsigned_tx = parse_unsigned(st->unsigned_bytes());
    if (!unsigned_tx) return std::unexpected("couldn't parse unsigned tx: " + unsigned_tx.error());

    auto tx = std::make_shared<Tx>();
    tx->unsigned_tx = *unsigned_tx;

    std::uint32_t n = st->credential_count();
    ByteView blob = st->credential_bytes();
    for (std::uint32_t i = 0; i < n; ++i) {
        auto split = wire::next_envelope(blob);
        if (!split) return std::unexpected("couldn't parse credentials: " + split.error());
        auto cred = fx::wrap_credential(split->envelope);
        if (!cred) return std::unexpected("couldn't parse credentials: " + cred.error());
        tx->creds.push_back(*cred);
        blob = split->rest;
    }

    tx->bytes_.assign(signed_bytes.begin(), signed_bytes.end());
    tx->tx_id = sha256(signed_bytes);
    return tx;
}

// ================= Tx =================

void Tx::set_bytes(Bytes unsigned_bytes, Bytes signed_bytes) {
    tx_id = sha256(view(signed_bytes));
    bytes_ = std::move(signed_bytes);
    unsigned_tx->set_bytes(std::move(unsigned_bytes));
}

Result<void> Tx::initialize() {
    if (unsigned_tx == nullptr) return std::unexpected(kErrNilTx);
    Bytes unsigned_bytes = unsigned_tx->bytes();
    std::vector<Bytes> cred_envelopes;
    cred_envelopes.reserve(creds.size());
    for (const auto& c : creds) {
        if (c == nullptr) return std::unexpected("problem creating transaction: nil credential");
        cred_envelopes.push_back(c->bytes());
    }
    Bytes signed_bytes = wire::new_signed_tx(wire::SignedTxInput{unsigned_bytes, cred_envelopes});
    set_bytes(std::move(unsigned_bytes), std::move(signed_bytes));
    return {};
}

std::vector<UTXO> Tx::utxos() const {
    std::vector<UTXO> out;
    if (unsigned_tx == nullptr) return out;
    const Id txid = tx_id;

    // Every kind produces its base outputs first, at index i — that is what
    // fixes an output's index, so the order here IS the chain's answer.
    const auto& base = unsigned_tx->base;
    out.reserve(base.outs.size());
    for (std::size_t i = 0; i < base.outs.size(); ++i) {
        out.push_back(UTXO{UTXOID{txid, std::uint32_t(i), false}, base.outs[i].asset_id,
                           std::static_pointer_cast<fx::FxOutput>(base.outs[i].out)});
    }

    if (const auto* ca = dynamic_cast<const CreateAssetTx*>(unsigned_tx.get())) {
        // A new asset's id IS the tx id, so its genesis outputs are denominated
        // in itself.
        for (const auto& state : ca->states) {
            for (const auto& o : state.outs) {
                out.push_back(UTXO{UTXOID{txid, std::uint32_t(out.size()), false}, txid, o});
            }
        }
    } else if (const auto* op_tx = dynamic_cast<const OperationTx*>(unsigned_tx.get())) {
        for (const auto& op : op_tx->ops) {
            for (const auto& o : op.op->outs()) {
                out.push_back(UTXO{UTXOID{txid, std::uint32_t(out.size()), false}, op.asset_id, o});
            }
        }
    }
    return out;
}

}  // namespace lux::xvm::txs
