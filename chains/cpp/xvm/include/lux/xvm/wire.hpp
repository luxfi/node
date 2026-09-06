// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// wire.hpp — what the X-Chain's bytes MEAN. What they SAY is in
// gen/wire_zap.hpp, which zapgen writes from schema/wire.zap.
//
// The split is the point. A field's offset, its width and the run its list is
// are questions about the payload, and the schema answers them once for every
// language. Which fx family owns a shape, whether a threshold is satisfiable,
// and how many signatures a run holds are questions about the chain, and they
// are answered here.
//
// Every fx primitive names itself with a 2-byte discriminator, OUTSIDE the ZAP
// message:
//
//   [TypeKind:1][ShapeKind:1][ZAP message: N]
//
// TypeKind names the fx FAMILY (secp256k1, nft, property, ...); ShapeKind
// names the SHAPE within it (TransferOutput, TransferInput, MintOutput,
// Credential, ...). Those are two independent questions, so they are two bytes
// rather than one dense slot id — which is why there is no codec registry.
// Adding an (fx, shape) is a new branch, never a growing shared table.

#pragma once

#include "lux/xvm/gen/wire_zap.hpp"
#include "lux/xvm/id.hpp"

#include <expected>
#include <string>
#include <vector>

namespace lux::xvm::wire {

enum class TypeKind : std::uint8_t {
    Reserved = 0x00,
    Secp256k1 = 0x01,
    MLDSA = 0x02,
    SLHDSA = 0x03,
    Ed25519 = 0x04,
    Secp256r1 = 0x05,
    Schnorr = 0x06,
    BLS12381 = 0x07,
    // Application fx families (built on secp256k1 credentials).
    NFT = 0x08,
    Property = 0x09,
};

enum class ShapeKind : std::uint8_t {
    Reserved = 0x00,
    TransferOutput = 0x01,
    TransferInput = 0x02,
    MintOutput = 0x03,
    MintInput = 0x04,
    MintOperation = 0x05,
    Credential = 0x06,
    AttestationOut = 0x07,
    AttestationIn = 0x08,
    OutputOwners = 0x09,
    UTXO = 0x0A,
    TransferableOut = 0x0B,
    TransferableIn = 0x0C,
    PChainOwner = 0x0D,
    SignedTx = 0x0E,
    LockedOutput = 0x0F,
    NFTMintOutput = 0x10,
    NFTTransferOutput = 0x11,
    XVMBaseTx = 0x12,
    NFTMintOperation = 0x13,
    NFTTransferOp = 0x14,
    OwnedOutput = 0x15,
    BurnOperation = 0x16,
};

// The error set every parse returns. These are the exact Go sentinels — a
// discriminator that does not match the shape being read is a cross-type
// confusion attempt, not a parse detail.
inline constexpr const char* kErrWrongTypeKind =
    "wire: TypeKind discriminator does not match expected fx family";
inline constexpr const char* kErrWrongShapeKind =
    "wire: ShapeKind discriminator does not match expected primitive shape";
inline constexpr const char* kErrShortEnvelope =
    "wire: envelope shorter than 2-byte discriminator prefix";
inline constexpr const char* kErrTrailingBytes =
    "wire: trailing bytes after zap message (non-canonical envelope)";

// Semantic gates on an owner group, read off an untrusted buffer.
inline constexpr const char* kErrOwnerThresholdZero =
    "wire: OutputOwners.Threshold must be > 0; threshold=0 disables authorization";
inline constexpr const char* kErrOwnerThresholdExceedsAddrs =
    "wire: OutputOwners.Threshold exceeds Addresses.Len() — unsatisfiable signer quorum";
inline constexpr const char* kErrOwnerAddrsEmpty =
    "wire: OutputOwners.Addresses is empty — signer set undefined";
inline constexpr const char* kErrOwnerAddrZero =
    "wire: OutputOwners.Addresses contains the zero ShortID — phantom signer";

template <class T>
using Result = std::expected<T, std::string>;

inline constexpr int kEnvelopePrefix = 2;

struct Discriminator {
    TypeKind type_kind;
    ShapeKind shape_kind;
};

// peek_discriminator reads the (TypeKind, ShapeKind) without committing to a
// shape. Composite dispatchers use it to recurse.
Result<Discriminator> peek_discriminator(ByteView b);

// payload is the ONE gate every shape goes through: the two discriminator
// bytes have to say what the caller expects, and only then is what follows
// parsed. It returns the root object, which the caller names with the
// generated view for that shape.
//
// The expected TypeKind is an argument rather than a field to re-check later,
// because the caller always knows which fx family it is reading for. A shape
// that no family owns — a UTXO, a base transaction — asks for Reserved.
Result<zap::Object> payload(ByteView b, ShapeKind shape, TypeKind kind);

// next_envelope splits the FIRST self-describing envelope off a packed run and
// returns (envelope, rest). The length comes from the inner ZAP header's own
// size field, which is why a packed list needs no separate length array. This
// is the ONE walker for every packed envelope run.
struct Split {
    ByteView envelope;
    ByteView rest;
};
Result<Split> next_envelope(ByteView blob);

Bytes write_envelope_prefix(TypeKind tk, ShapeKind sk, const Bytes& zap_bytes);

// ---- from what the bytes say to what the chain holds ----

Id to_id(ByteView b);
ShortId to_short_id(ByteView b);

// The signature indices of a spend, as the chain counts them.
std::vector<std::uint32_t> sig_indices(zap::List l);

// A stride-1 run, as bytes. Credentials carry signatures and public keys this
// way: the fx knows its own widths, so the wire carries the run and not a
// length per element.
Bytes run(zap::List l);

// The owner addresses of any shape that carries a stride-20 address run.
template <class View>
std::vector<ShortId> addresses(const View& v) {
    std::vector<ShortId> out;
    const auto n = v.Addrs().size();
    out.reserve(static_cast<std::size_t>(n));
    for (std::int64_t i = 0; i < n; ++i) out.push_back(to_short_id(v.AddrsAt(i)));
    return out;
}

// ---- what an owner group has to satisfy to authorize anything ----
//
// Every consumer that treats (Threshold, Addresses) as a quorum runs this, so
// the gate is stated once rather than at each of them.
Result<void> verify_owners(std::uint32_t threshold, const std::vector<ShortId>& addrs);

template <class View>
Result<void> verify_owners(const View& v) {
    return verify_owners(v.Threshold(), addresses(v));
}

// ---- credentials ----
//
// The signature COUNT is derived, because the wire stores concatenated bytes:
// a run that does not divide by the fx's signature width is 0 signatures,
// never a partial one.
int signature_count(const Credential& c, int sig_size);
Bytes signature_at(const Credential& c, int i, int sig_size);

}  // namespace lux::xvm::wire
