// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/vertex.hpp"

#include "lux/zkvm/block.hpp"
#include "lux/zkvm/vm.hpp"

namespace lux::zkvm {
namespace {

void put_be64(Bytes& out, std::uint64_t v) {
    for (int i = 7; i >= 0; --i) out.push_back(std::uint8_t(v >> (8 * i)));
}

void put_be32(Bytes& out, std::uint32_t v) {
    for (int i = 3; i >= 0; --i) out.push_back(std::uint8_t(v >> (8 * i)));
}

std::uint64_t get_be64(ByteView b, std::size_t pos) {
    std::uint64_t v = 0;
    for (std::size_t i = 0; i < 8; ++i) v = (v << 8) | b[pos + i];
    return v;
}

std::uint32_t get_be32(ByteView b, std::size_t pos) {
    std::uint32_t v = 0;
    for (std::size_t i = 0; i < 4; ++i) v = (v << 8) | b[pos + i];
    return v;
}

}  // namespace

Id Vertex::compute_id() const {
    Hasher h;
    if (vm_) h.write(view(vm_->bind()));
    h.num(vertex_height);
    h.num32(epoch);
    for (const auto& p : parents) h.write(view(p));
    for (const auto& tx : txs) {
        const Id tx_id = tx.compute_id();
        h.write(view(tx_id));
    }
    return h.sum();
}

Bytes Vertex::serialize() const {
    // height(8) | epoch(4) | parentCount(4) | parents | txCount(4) | [len(4) tx]*
    Bytes out;
    out.reserve(8 + 4 + 4 + parents.size() * 32 + 4 + txs.size() * 64);
    put_be64(out, vertex_height);
    put_be32(out, epoch);
    put_be32(out, std::uint32_t(parents.size()));
    for (const auto& p : parents) out.insert(out.end(), p.begin(), p.end());
    put_be32(out, std::uint32_t(txs.size()));
    for (const auto& tx : txs) {
        const Bytes raw = tx.marshal();
        put_be32(out, std::uint32_t(raw.size()));
        out.insert(out.end(), raw.begin(), raw.end());
    }
    return out;
}

void Vertex::finish() {
    tx_ids.clear();
    tx_ids.reserve(txs.size());
    for (const auto& tx : txs) tx_ids.push_back(tx.compute_id());
    id_ = compute_id();
    bytes_ = serialize();
}

ByteSet Vertex::nullifier_set() const {
    ByteSet s;
    for (const auto& tx : txs)
        for (const auto& n : tx.nullifiers) s.insert(n);
    return s;
}

bool Vertex::conflicts(const Vertex& other) const {
    const auto ours = nullifier_set();
    for (const auto& tx : other.txs)
        for (const auto& n : tx.nullifiers)
            if (ours.count(n)) return true;
    return false;
}

wire::Result<void> Vertex::check() {
    if (vm_ == nullptr) return std::unexpected("zkvm: vertex is not bound to a chain");

    if (txs.size() > vm_->z_config().max_tx_per_block)
        return std::unexpected(std::string(kErrInvalidBlock) + ": " +
                               std::to_string(txs.size()) + " transactions over the " +
                               std::to_string(vm_->z_config().max_tx_per_block) + " cap");

    // A vertex extends the frontier. The store keeps one tip for both shapes, so
    // a vertex that names something else moves the chain sideways.
    const Id tip = vm_->chain().tip();
    const std::uint64_t tip_height = vm_->chain().height();
    if (parents.size() != 1 || parents[0] != tip)
        return std::unexpected(std::string(kErrNotOnTip) +
                               ": vertex parents do not name the tip " + hex(tip));
    if (vertex_height != tip_height + 1)
        return std::unexpected(std::string(kErrInvalidHeight) + ": height " +
                               std::to_string(vertex_height) +
                               " does not follow the tip at " + std::to_string(tip_height));

    // Every nullifier in the vertex must be distinct. admit only sees nullifiers
    // already spent in ACCEPTED state; assembly refuses to batch conflicting
    // transactions, and a vertex that arrived on the wire is held to the same
    // rule or one shielded note is spent twice.
    ByteSet spent_here;
    for (const auto& tx : txs)
        for (const auto& n : tx.nullifiers)
            if (!spent_here.insert(n).second) return std::unexpected(kErrDuplicateNullifier);

    for (const auto& tx : txs) {
        if (auto r = vm_->admit(tx, vertex_height); !r) return r;
    }
    return {};
}

wire::Result<void> Vertex::commit() {
    if (vm_ == nullptr) return std::unexpected("zkvm: vertex is not bound to a chain");
    // A vertex is not a block — several parents, no timestamp — but it changes
    // state the same way, and this is that way.
    return vm_->chain().accept(std::static_pointer_cast<Decision>(shared_from_this()));
}

wire::Result<void> Vertex::write(store::Store& view_store) {
    (void)view_store;
    for (const auto& tx : txs) {
        for (const auto& n : tx.nullifiers) {
            if (auto r = vm_->nullifiers().mark_spent(view(n), vertex_height); !r) return r;
        }
        for (std::size_t i = 0; i < tx.outputs.size(); ++i) {
            Utxo u;
            u.tx_id = tx.id;
            u.output_index = std::uint32_t(i);
            u.commitment = tx.outputs[i].commitment;
            u.ciphertext = tx.outputs[i].encrypted_note;
            u.ephemeral_pk = tx.outputs[i].ephemeral_pubkey;
            u.height = vertex_height;
            if (auto r = vm_->utxos().add(u); !r) return r;
        }
    }
    return {};
}

void Vertex::publish() {
    status_ = Status::Accepted;
    for (const auto& tx : txs) vm_->mempool().remove(tx.id);
}

// Decided once, for the reason the block is: a vertex already accepted has
// spent its notes, and handing its transactions back would put a double spend
// into the pool this node builds from.
void Vertex::reject() {
    if (status_ != Status::Processing) return;
    status_ = Status::Rejected;
    for (const auto& tx : txs) (void)vm_->mempool().add(tx);
}

wire::Result<std::shared_ptr<Vertex>> deserialize_vertex(ByteView data, Vm& vm) {
    if (data.size() < 16) return std::unexpected(kErrInvalidBlock);
    std::size_t pos = 0;

    auto v = std::make_shared<Vertex>();
    v->bind_vm(vm);
    v->vertex_height = get_be64(data, pos);
    pos += 8;
    v->epoch = get_be32(data, pos);
    pos += 4;

    const std::uint32_t parent_count = get_be32(data, pos);
    pos += 4;
    if (std::size_t(parent_count) > (data.size() - pos) / 32)
        return std::unexpected(kErrInvalidBlock);
    v->parents.resize(parent_count);
    for (std::uint32_t i = 0; i < parent_count; ++i) {
        std::copy(data.begin() + std::ptrdiff_t(pos), data.begin() + std::ptrdiff_t(pos + 32),
                  v->parents[i].begin());
        pos += 32;
    }

    if (pos + 4 > data.size()) return std::unexpected(kErrInvalidBlock);
    const std::uint32_t tx_count = get_be32(data, pos);
    pos += 4;
    // Every transaction costs at least its 4-byte length prefix.
    if (std::size_t(tx_count) > (data.size() - pos) / 4) return std::unexpected(kErrInvalidBlock);

    v->txs.reserve(tx_count);
    v->tx_ids.reserve(tx_count);
    for (std::uint32_t i = 0; i < tx_count; ++i) {
        if (pos + 4 > data.size()) return std::unexpected(kErrInvalidBlock);
        const std::uint32_t tx_len = get_be32(data, pos);
        pos += 4;
        if (pos + std::size_t(tx_len) > data.size()) return std::unexpected(kErrInvalidBlock);
        auto tx = parse_transaction(data.subspan(pos, tx_len));
        if (!tx) return std::unexpected(tx.error());
        v->tx_ids.push_back(tx->id);
        v->txs.push_back(std::move(*tx));
        pos += tx_len;
    }

    if (pos != data.size()) return std::unexpected(wire::kErrTrailingBytes);

    v->set_raw(bytes_of(data));
    v->set_computed_id(v->compute_id());
    return v;
}

}  // namespace lux::zkvm
