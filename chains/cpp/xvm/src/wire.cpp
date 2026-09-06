// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/wire.hpp"

#include <cstring>

namespace lux::xvm::wire {
namespace {

struct Prefix {
    TypeKind tk;
    ShapeKind sk;
    ByteView zap_bytes;
};

Result<Prefix> read_prefix(ByteView b) {
    if (b.size() < std::size_t(kEnvelopePrefix)) return std::unexpected(kErrShortEnvelope);
    return Prefix{TypeKind(b[0]), ShapeKind(b[1]), b.subspan(kEnvelopePrefix)};
}

}  // namespace

Result<Discriminator> peek_discriminator(ByteView b) {
    auto pre = read_prefix(b);
    if (!pre) return std::unexpected(pre.error());
    return Discriminator{pre->tk, pre->sk};
}

Result<zap::Object> payload(ByteView b, ShapeKind shape, TypeKind kind) {
    auto pre = read_prefix(b);
    if (!pre) return std::unexpected(pre.error());
    if (pre->sk != shape) return std::unexpected(kErrWrongShapeKind);
    if (pre->tk != kind) return std::unexpected(kErrWrongTypeKind);
    auto msg = zap::Message::parse(pre->zap_bytes);
    if (!msg) return std::unexpected(std::string(zap::describe(msg.error())));
    return msg->root();
}

Result<Split> next_envelope(ByteView blob) {
    if (std::size_t(kEnvelopePrefix) > blob.size()) return std::unexpected(kErrShortEnvelope);
    std::size_t zap_start = kEnvelopePrefix;
    if (zap_start + zap::kHeaderSize > blob.size()) return std::unexpected(kErrShortEnvelope);
    const auto zap_size = std::size_t(zap::load_u32(blob.data() + zap_start + 12));
    const std::size_t env_end = zap_start + zap_size;
    if (zap_size < zap::kHeaderSize || env_end > blob.size())
        return std::unexpected(kErrShortEnvelope);
    return Split{blob.subspan(0, env_end), blob.subspan(env_end)};
}

Bytes write_envelope_prefix(TypeKind tk, ShapeKind sk, const Bytes& zap_bytes) {
    Bytes out;
    out.reserve(std::size_t(kEnvelopePrefix) + zap_bytes.size());
    out.push_back(std::uint8_t(tk));
    out.push_back(std::uint8_t(sk));
    out.insert(out.end(), zap_bytes.begin(), zap_bytes.end());
    return out;
}

Id to_id(ByteView b) {
    Id out{};
    if (b.size() == out.size()) std::memcpy(out.data(), b.data(), out.size());
    return out;
}

ShortId to_short_id(ByteView b) {
    ShortId out{};
    if (b.size() == out.size()) std::memcpy(out.data(), b.data(), out.size());
    return out;
}

std::vector<std::uint32_t> sig_indices(zap::List l) {
    std::vector<std::uint32_t> out(static_cast<std::size_t>(l.size()));
    for (std::int64_t i = 0; i < l.size(); ++i) out[std::size_t(i)] = l.u32(i);
    return out;
}

Bytes run(zap::List l) {
    Bytes out(static_cast<std::size_t>(l.size()));
    for (std::int64_t i = 0; i < l.size(); ++i) out[std::size_t(i)] = l.u8(i);
    return out;
}

Result<void> verify_owners(std::uint32_t threshold, const std::vector<ShortId>& addrs) {
    if (addrs.empty()) return std::unexpected(kErrOwnerAddrsEmpty);
    if (threshold == 0) return std::unexpected(kErrOwnerThresholdZero);
    if (std::uint64_t(threshold) > std::uint64_t(addrs.size()))
        return std::unexpected(kErrOwnerThresholdExceedsAddrs);
    for (const auto& a : addrs) {
        if (a == kEmptyShortId) return std::unexpected(kErrOwnerAddrZero);
    }
    return {};
}

int signature_count(const Credential& c, int sig_size) {
    if (sig_size <= 0) return 0;
    const int total = int(c.Signatures().size());
    if (total % sig_size != 0) return 0;
    return total / sig_size;
}

Bytes signature_at(const Credential& c, int i, int sig_size) {
    if (sig_size <= 0 || i < 0) return {};
    const Bytes all = run(c.Signatures());
    const std::size_t start = std::size_t(i) * std::size_t(sig_size);
    const std::size_t end = start + std::size_t(sig_size);
    if (end > all.size()) return {};
    return Bytes(all.begin() + std::ptrdiff_t(start), all.begin() + std::ptrdiff_t(end));
}

}  // namespace lux::xvm::wire
