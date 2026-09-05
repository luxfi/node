// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// address.hpp — how a person writes down an address, and how this chain reads
// it back.
//
// A UTXO's owner is twenty bytes. A human types bech32: a human-readable part,
// a separator, the payload in five-bit groups, and six characters of checksum
// that catch a typo instead of sending value to nobody. Ported from
// luxfi/address (ParseBech32 / FormatBech32), which is BIP-173 bech32 with the
// payload converted 8→5 bits on the way out and 5→8 on the way in.
//
// This lives here because a genesis definition names its holders by address
// (genesis.hpp, `from_definitions`), and a chain that cannot read the string a
// definition is written in cannot build its own genesis.

#pragma once

#include "lux/xvm/id.hpp"
#include "lux/xvm/wire.hpp"

#include <string>
#include <string_view>
#include <utility>

namespace lux::xvm::address {

template <class T>
using Result = wire::Result<T>;

inline constexpr const char* kErrMixedCase = "bech32 string mixes upper and lower case";
inline constexpr const char* kErrTooLong = "bech32 string exceeds 90 characters";
inline constexpr const char* kErrNoSeparator = "bech32 string has no separator";
inline constexpr const char* kErrBadPrefix = "bech32 human-readable part is invalid";
inline constexpr const char* kErrBadCharacter = "bech32 string has a character outside the charset";
inline constexpr const char* kErrBadChecksum = "bech32 checksum does not verify";
inline constexpr const char* kErrBadPadding = "bech32 payload has invalid padding";
inline constexpr const char* kErrNotTwentyBytes = "address is not twenty bytes";

// The longest a bech32 string may be, checksum and separator included.
inline constexpr std::size_t kMaxLength = 90;

// parse returns the human-readable part and the eight-bit payload.
// Go: address.ParseBech32.
Result<std::pair<std::string, Bytes>> parse(std::string_view s);

// format is the other direction. Go: address.FormatBech32.
Result<std::string> format(std::string_view hrp, ByteView payload);

// short_id narrows a payload to the twenty bytes an owner is. Go: ids.ToShortID.
Result<ShortId> short_id(ByteView payload);

// parse_short is the whole journey a genesis holder's address makes: string to
// owner. Anything the chain does with an address goes through it.
Result<ShortId> parse_short(std::string_view s);

// convert_bits regroups `from`-bit groups into `to`-bit groups. It is exposed
// because it is where a malformed payload is caught, and a test that cannot
// state it cannot pin that.
Result<Bytes> convert_bits(ByteView data, int from, int to, bool pad);

}  // namespace lux::xvm::address
