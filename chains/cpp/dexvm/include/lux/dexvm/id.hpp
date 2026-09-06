// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// id.hpp — the identifier the D-Chain names things with, the hash that derives
// it, and the one error type every admission decision answers with.
//
// Id is deliberately std::array<uint8_t,32>, the SAME type the node's VM seam
// calls lux::consensus::Id — so an id crosses into consensus with no conversion
// and no second definition.
//
// An id has two renderings and they are NOT interchangeable. `cb58` is what Go's
// ids.ID.String()/MarshalJSON produce (base58 of the 32 bytes followed by the
// low 4 bytes of their sha256) and is what a manifest file carries. `hex` is what
// Go's ids.ID.Hex() produces and is what a manifest's chainLabels map is KEYED
// by. Porting one and using it for both would silently fail to read a real
// manifest, so both are here and each is named for what it is.
//
// Err is the error IDENTITY. Go's callers ask errors.Is(err, ErrUnknownAsset)
// and branch on the answer, so a port that returned only prose would drop a
// distinction the reference depends on: the code travels with the message, and
// wrapping (Go's %w) prepends context while keeping the code.

#pragma once

#include <array>
#include <cstdint>
#include <expected>
#include <span>
#include <string>
#include <string_view>
#include <vector>

namespace lux::dexvm {

using Id = std::array<std::uint8_t, 32>;
inline constexpr Id kEmptyId{};

using Bytes = std::vector<std::uint8_t>;
using ByteView = std::span<const std::uint8_t>;

inline ByteView view(const Bytes& b) { return ByteView(b.data(), b.size()); }
template <std::size_t N>
inline ByteView view(const std::array<std::uint8_t, N>& a) {
    return ByteView(a.data(), a.size());
}
inline Bytes to_bytes(ByteView v) { return Bytes(v.begin(), v.end()); }

// sha256 of the input — the D-Chain's ONE hash (Go: crypto/sha256.Sum256).
Id sha256(ByteView data);

// ---- errors ---------------------------------------------------------------

// Err is the error's identity: what Go's errors.Is compares. Every sentinel the
// reference exports has a member here; anything with no sentinel behind it is
// Other, which no test may branch on.
enum class Err : std::uint8_t {
    Other = 0,
    // asset.go
    InvalidKind,
    BadRef,
    EmptyChainID,
    // market.go
    SameAsset,
    NetworkMismatch,
    DuplicateMarket,
    // registry.go
    UnknownAsset,
    AssetDisabled,
    KindNotAllowed,
    DuplicateAsset,
    // gate.go
    SyntheticOnValueNet,
    EnabledMarketUnknownAsset,
    BadAllowedKind,
    // consensus_mode.go
    ValueModeUnset,
    ValueModeIllegal,
    LaunchAssertionsUnmet,
    // manifest.go / embedded.go
    ManifestHashMismatch,
    NoEmbeddedManifest,
};

// text_of is the sentinel's own message — byte-identical to the Go errors.New
// string, so a message built here reads the way the reference's does.
std::string_view text_of(Err code);

struct Error {
    Err code = Err::Other;
    std::string text;

    // Go: errors.Is(err, ErrX).
    bool is(Err c) const { return code == c; }
    // Go: fmt.Errorf("ctx: %w", err) — context in front, identity preserved.
    Error wrap(std::string_view ctx) const { return Error{code, std::string(ctx) + ": " + text}; }
};

template <class T>
using Result = std::expected<T, Error>;

// fail builds the unexpected half. The two-argument form is Go's
// fmt.Errorf("%w: detail", ErrX): the sentinel's text, then the detail.
inline std::unexpected<Error> fail(std::string text) {
    return std::unexpected(Error{Err::Other, std::move(text)});
}
inline std::unexpected<Error> fail(Err code) {
    return std::unexpected(Error{code, std::string(text_of(code))});
}
inline std::unexpected<Error> fail(Err code, std::string_view detail) {
    return std::unexpected(Error{code, std::string(text_of(code)) + ": " + std::string(detail)});
}
// fail_note is the other shape the reference writes: fmt.Errorf("%w (note)", ErrX),
// where the note is a parenthesised list of the values that made the decision.
inline std::unexpected<Error> fail_note(Err code, std::string_view note) {
    return std::unexpected(Error{code, std::string(text_of(code)) + " (" + std::string(note) + ")"});
}

// ---- renderings -----------------------------------------------------------

// hex renders bytes as lowercase hex with no prefix (Go: hex.EncodeToString,
// and ids.ID.Hex for an id).
std::string hex(ByteView b);
inline std::string hex(const Id& id) { return hex(view(id)); }

// hex0x / from_hex are the manifest's canonicalRef encoding (Go: registry.Bytes
// MarshalText/UnmarshalText). from_hex accepts an optional 0x/0X prefix and
// surrounding whitespace, and an empty body decodes to no bytes.
std::string hex0x(ByteView b);
Result<Bytes> from_hex(std::string_view s);

// cb58 is Go's ids.ID.String(): base58 of the 32 bytes followed by the low 4
// bytes of their sha256 — EXCEPT for a native chain id (31 zero bytes and a
// letter), which renders as its well-known alias instead. Both halves are Go's,
// and a manifest carries whichever Go wrote.
std::string cb58(const Id& id);
Result<Id> id_from_string(std::string_view s);

}  // namespace lux::dexvm
