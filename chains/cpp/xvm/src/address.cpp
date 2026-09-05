// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/address.hpp"

#include <algorithm>
#include <array>
#include <cstdint>

namespace lux::xvm::address {
namespace {

// The bech32 alphabet: thirty-two characters chosen so a single mistyped one
// cannot be mistaken for another.
constexpr const char* kCharset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l";
constexpr std::size_t kChecksumLength = 6;
constexpr std::size_t kMinLength = 8;  // one hrp character, a separator, a checksum

int charset_index(char c) {
    for (int i = 0; i < 32; ++i) {
        if (kCharset[i] == c) return i;
    }
    return -1;
}

// The BIP-173 polynomial. Its five generators are what give the code its
// guarantee: any four errors in a string of this length are detected.
std::uint32_t polymod(const std::vector<std::uint8_t>& values) {
    static constexpr std::array<std::uint32_t, 5> kGen = {0x3b6a57b2, 0x26508e6d, 0x1ea119fa,
                                                          0x3d4233dd, 0x2a1462b3};
    std::uint32_t chk = 1;
    for (std::uint8_t v : values) {
        const std::uint32_t b = chk >> 25;
        chk = ((chk & 0x1ffffff) << 5) ^ v;
        for (int i = 0; i < 5; ++i) {
            if (((b >> i) & 1) == 1) chk ^= kGen[std::size_t(i)];
        }
    }
    return chk;
}

std::vector<std::uint8_t> hrp_expand(std::string_view hrp) {
    std::vector<std::uint8_t> out;
    out.reserve(hrp.size() * 2 + 1);
    for (char c : hrp) out.push_back(std::uint8_t(std::uint8_t(c) >> 5));
    out.push_back(0);
    for (char c : hrp) out.push_back(std::uint8_t(std::uint8_t(c) & 31));
    return out;
}

bool checksum_holds(std::string_view hrp, const std::vector<std::uint8_t>& values) {
    std::vector<std::uint8_t> all = hrp_expand(hrp);
    all.insert(all.end(), values.begin(), values.end());
    return polymod(all) == 1;
}

std::vector<std::uint8_t> checksum_of(std::string_view hrp,
                                      const std::vector<std::uint8_t>& values) {
    std::vector<std::uint8_t> all = hrp_expand(hrp);
    all.insert(all.end(), values.begin(), values.end());
    all.insert(all.end(), kChecksumLength, 0);
    const std::uint32_t mod = polymod(all) ^ 1;
    std::vector<std::uint8_t> out(kChecksumLength);
    for (std::size_t i = 0; i < kChecksumLength; ++i)
        out[i] = std::uint8_t((mod >> (5 * (5 - i))) & 31);
    return out;
}

char lower(char c) { return (c >= 'A' && c <= 'Z') ? char(c - 'A' + 'a') : c; }

}  // namespace

Result<Bytes> convert_bits(ByteView data, int from, int to, bool pad) {
    if (from < 1 || from > 8 || to < 1 || to > 8)
        return std::unexpected("bech32 bit groups must be between 1 and 8");

    Bytes out;
    out.reserve(data.size() * std::size_t(from) / std::size_t(to) + 1);
    std::uint8_t next = 0;
    int filled = 0;
    for (std::uint8_t byte : data) {
        std::uint8_t b = std::uint8_t(byte << (8 - from));
        int remaining = from;
        while (remaining > 0) {
            const int room = to - filled;
            const int take = remaining < room ? remaining : room;
            next = std::uint8_t((next << take) | (b >> (8 - take)));
            b = std::uint8_t(b << take);
            remaining -= take;
            filled += take;
            if (filled == to) {
                out.push_back(next);
                filled = 0;
                next = 0;
            }
        }
    }
    if (pad && filled > 0) {
        out.push_back(std::uint8_t(next << (to - filled)));
        filled = 0;
        next = 0;
    }
    // Bits left over that are not simply zero padding are a payload that was
    // cut, not one that was padded.
    if (filled > 0 && (filled > 4 || next != 0)) return std::unexpected(kErrBadPadding);
    return out;
}

Result<std::pair<std::string, Bytes>> parse(std::string_view s) {
    if (s.size() > kMaxLength) return std::unexpected(kErrTooLong);
    if (s.size() < kMinLength) return std::unexpected(kErrNoSeparator);

    bool has_lower = false, has_upper = false;
    for (char c : s) {
        const auto u = std::uint8_t(c);
        if (u < 33 || u > 126) return std::unexpected(kErrBadCharacter);
        has_lower = has_lower || (u >= 'a' && u <= 'z');
        has_upper = has_upper || (u >= 'A' && u <= 'Z');
        // Case carries no information here, so a string that mixes it is a
        // string whose checksum could be read two ways.
        if (has_lower && has_upper) return std::unexpected(kErrMixedCase);
    }

    std::string bech;
    bech.reserve(s.size());
    for (char c : s) bech.push_back(lower(c));

    const std::size_t one = bech.rfind('1');
    if (one == std::string::npos || one < 1 || one + 7 > bech.size())
        return std::unexpected(kErrNoSeparator);

    const std::string hrp = bech.substr(0, one);
    std::vector<std::uint8_t> values;
    values.reserve(bech.size() - one - 1);
    for (std::size_t i = one + 1; i < bech.size(); ++i) {
        const int idx = charset_index(bech[i]);
        if (idx < 0) return std::unexpected(kErrBadCharacter);
        values.push_back(std::uint8_t(idx));
    }

    if (!checksum_holds(hrp, values)) return std::unexpected(kErrBadChecksum);
    values.resize(values.size() - kChecksumLength);

    auto payload = convert_bits(ByteView(values.data(), values.size()), 5, 8, true);
    if (!payload) return std::unexpected(payload.error());
    return std::pair<std::string, Bytes>{hrp, std::move(*payload)};
}

Result<std::string> format(std::string_view hrp, ByteView payload) {
    auto five = convert_bits(payload, 8, 5, true);
    if (!five) return std::unexpected(five.error());

    std::string low;
    low.reserve(hrp.size());
    for (char c : hrp) low.push_back(lower(c));
    if (low.empty()) return std::unexpected(kErrBadPrefix);
    for (char c : low) {
        const auto u = std::uint8_t(c);
        if (u < 33 || u > 126) return std::unexpected(kErrBadPrefix);
    }

    std::vector<std::uint8_t> values(five->begin(), five->end());
    const auto check = checksum_of(low, values);
    values.insert(values.end(), check.begin(), check.end());

    std::string out = low;
    out.push_back('1');
    for (std::uint8_t v : values) {
        if (v >= 32) return std::unexpected(kErrBadCharacter);
        out.push_back(kCharset[v]);
    }
    if (out.size() > kMaxLength) return std::unexpected(kErrTooLong);
    return out;
}

Result<ShortId> short_id(ByteView payload) {
    if (payload.size() != 20) return std::unexpected(kErrNotTwentyBytes);
    ShortId a{};
    std::copy(payload.begin(), payload.end(), a.begin());
    return a;
}

Result<ShortId> parse_short(std::string_view s) {
    auto parsed = parse(s);
    if (!parsed) return std::unexpected(parsed.error());
    return short_id(view(parsed->second));
}

}  // namespace lux::xvm::address
