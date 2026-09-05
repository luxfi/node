// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/dexvm/id.hpp"

#include "sha256.hpp"

#include <algorithm>
#include <cstddef>

namespace lux::dexvm {
namespace {

constexpr char kBase58Alphabet[] = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz";
constexpr int kChecksumLen = 4;

// A native chain id is 31 zero bytes and one letter in the last position. Go
// renders it as 32 '1's and that letter rather than as base58 — a fast path that
// is also a wire format, because a manifest written by Go carries it.
constexpr char kNativePrefix[] = "11111111111111111111111111111111";
constexpr std::size_t kNativePrefixLen = 32;
constexpr std::size_t kLetterPos = 31;

bool is_native_letter(std::uint8_t c) {
    switch (c) {
        case 'P': case 'C': case 'X': case 'Q': case 'A': case 'B':
        case 'M': case 'F': case 'Z': case 'G': case 'I': case 'K': case 'D':
            return true;
        default:
            return false;
    }
}

// native_string is Go's ids.NativeChainString: the alias when the id is one of
// the well-known chains, the empty string otherwise.
std::string native_string(const Id& id) {
    for (std::size_t i = 0; i < kLetterPos; ++i) {
        if (id[i] != 0) return {};
    }
    if (!is_native_letter(id[kLetterPos])) return {};
    return std::string(kNativePrefix) + char(id[kLetterPos]);
}

std::string base58_encode(ByteView data) {
    // Leading zero bytes each become one '1' — base58's own convention, and the
    // reason an all-zero id is a run of '1's rather than a single character.
    std::size_t zeros = 0;
    while (zeros < data.size() && data[zeros] == 0) ++zeros;

    std::vector<std::uint8_t> digits;
    digits.reserve(data.size() * 138 / 100 + 1);
    for (std::size_t i = zeros; i < data.size(); ++i) {
        int carry = data[i];
        for (auto& d : digits) {
            const int v = int(d) * 256 + carry;
            d = std::uint8_t(v % 58);
            carry = v / 58;
        }
        while (carry > 0) {
            digits.push_back(std::uint8_t(carry % 58));
            carry /= 58;
        }
    }

    std::string out;
    out.reserve(zeros + digits.size());
    out.append(zeros, '1');
    for (auto it = digits.rbegin(); it != digits.rend(); ++it) out.push_back(kBase58Alphabet[*it]);
    return out;
}

Result<Bytes> base58_decode(std::string_view s) {
    std::size_t zeros = 0;
    while (zeros < s.size() && s[zeros] == '1') ++zeros;

    std::vector<std::uint8_t> bytes;
    bytes.reserve(s.size());
    for (std::size_t i = zeros; i < s.size(); ++i) {
        const char* p = std::find(std::begin(kBase58Alphabet), std::end(kBase58Alphabet) - 1, s[i]);
        if (p == std::end(kBase58Alphabet) - 1)
            return fail(std::string("base58 decoding error: invalid character '") + s[i] + "'");
        int carry = int(p - std::begin(kBase58Alphabet));
        for (auto& b : bytes) {
            const int v = int(b) * 58 + carry;
            b = std::uint8_t(v & 0xff);
            carry = v >> 8;
        }
        while (carry > 0) {
            bytes.push_back(std::uint8_t(carry & 0xff));
            carry >>= 8;
        }
    }

    Bytes out;
    out.reserve(zeros + bytes.size());
    out.insert(out.end(), zeros, 0);
    out.insert(out.end(), bytes.rbegin(), bytes.rend());
    return out;
}

}  // namespace

Id sha256(ByteView data) {
    Id out{};
    // The reused sha256 speaks std::byte; the cast is a reinterpretation of the
    // same storage, not a copy.
    cevm::crypto::sha256(reinterpret_cast<std::byte*>(out.data()),
                         reinterpret_cast<const std::byte*>(data.data()), data.size());
    return out;
}

std::string_view text_of(Err code) {
    switch (code) {
        case Err::InvalidKind:
            return "registry: asset kind is not EVM_NATIVE, ERC20 or UTXO";
        case Err::BadRef:
            return "registry: canonical reference does not match asset kind";
        case Err::EmptyChainID:
            return "registry: asset source chain id is empty";
        case Err::SameAsset:
            return "registry: market base and quote assets are identical";
        case Err::NetworkMismatch:
            return "registry: market network does not match asset network";
        case Err::DuplicateMarket:
            return "registry: market already exists";
        case Err::UnknownAsset:
            return "registry: asset is not registered (unknown/synthetic)";
        case Err::AssetDisabled:
            return "registry: asset is registered but disabled";
        case Err::KindNotAllowed:
            return "registry: asset kind not in allowed-kinds policy";
        case Err::DuplicateAsset:
            return "registry: asset already registered";
        case Err::SyntheticOnValueNet:
            return "startup: synthetic asset/market/liquidity flag set on a value-bearing "
                   "network (mainnet/testnet)";
        case Err::EnabledMarketUnknownAsset:
            return "startup: enabled market references an unknown/synthetic asset";
        case Err::BadAllowedKind:
            return "startup: dexAllowedAssetKinds contains a non-real kind";
        case Err::ValueModeUnset:
            return "registry: refuse DEX value activation — consensus mode is UNSET (no "
                   "Byzantine-finality and no labeled CFT-parity declared)";
        case Err::ValueModeIllegal:
            return "registry: refuse DEX value activation — consensus mode is not "
                   "QUORUM_FINALITY or HONEST_VALIDATOR_LABELED";
        case Err::LaunchAssertionsUnmet:
            return "registry: refuse HONEST_VALIDATOR_LABELED value activation — caps-on + "
                   "real-assets-only + halt-ready not all asserted";
        case Err::ManifestHashMismatch:
            return "registry: manifest content hash does not match the pinned expected hash "
                   "(the file was modified from the CI-approved artifact)";
        case Err::NoEmbeddedManifest:
            return "registry: no embedded asset manifest for this EVM chainID";
        case Err::Other:
            break;
    }
    return "";
}

std::string hex(ByteView b) {
    static constexpr char kDigits[] = "0123456789abcdef";
    std::string out;
    out.reserve(b.size() * 2);
    for (std::uint8_t x : b) {
        out.push_back(kDigits[x >> 4]);
        out.push_back(kDigits[x & 0x0f]);
    }
    return out;
}

std::string hex0x(ByteView b) { return "0x" + hex(b); }

Result<Bytes> from_hex(std::string_view s) {
    while (!s.empty() && (s.front() == ' ' || s.front() == '\t' || s.front() == '\n' || s.front() == '\r'))
        s.remove_prefix(1);
    while (!s.empty() && (s.back() == ' ' || s.back() == '\t' || s.back() == '\n' || s.back() == '\r'))
        s.remove_suffix(1);
    if (s.starts_with("0x") || s.starts_with("0X")) s.remove_prefix(2);
    if (s.empty()) return Bytes{};

    const std::string original(s);
    auto nibble = [](char c) -> int {
        if (c >= '0' && c <= '9') return c - '0';
        if (c >= 'a' && c <= 'f') return c - 'a' + 10;
        if (c >= 'A' && c <= 'F') return c - 'A' + 10;
        return -1;
    };
    if (s.size() % 2 != 0)
        return fail("registry: invalid hex reference \"" + original + "\": odd length hex string");
    Bytes out;
    out.reserve(s.size() / 2);
    for (std::size_t i = 0; i < s.size(); i += 2) {
        const int hi = nibble(s[i]);
        const int lo = nibble(s[i + 1]);
        if (hi < 0 || lo < 0)
            return fail("registry: invalid hex reference \"" + original + "\": invalid byte");
        out.push_back(std::uint8_t(hi * 16 + lo));
    }
    return out;
}

std::string cb58(const Id& id) {
    if (std::string native = native_string(id); !native.empty()) return native;

    Bytes checked(id.begin(), id.end());
    const Id sum = sha256(view(id));
    checked.insert(checked.end(), sum.end() - kChecksumLen, sum.end());
    return base58_encode(view(checked));
}

Result<Id> id_from_string(std::string_view s) {
    if (s.empty()) return kEmptyId;

    // The native alias first, exactly as Go checks NativeChainFromString before
    // it reaches for base58.
    if (s.size() == kNativePrefixLen + 1 && s.substr(0, kNativePrefixLen) == kNativePrefix &&
        is_native_letter(std::uint8_t(s[kNativePrefixLen]))) {
        Id id{};
        id[kLetterPos] = std::uint8_t(s[kNativePrefixLen]);
        return id;
    }

    auto decoded = base58_decode(s);
    if (!decoded) return std::unexpected(decoded.error());
    Bytes& raw = *decoded;
    if (raw.size() < kChecksumLen) return fail("input string is smaller than the checksum size");

    const Bytes body(raw.begin(), raw.end() - kChecksumLen);
    const Id sum = sha256(view(body));
    if (!std::equal(raw.end() - kChecksumLen, raw.end(), sum.end() - kChecksumLen))
        return fail("invalid input checksum");
    if (body.size() != 32)
        return fail("registry: id must be 32 bytes, got " + std::to_string(body.size()));

    Id id{};
    std::copy(body.begin(), body.end(), id.begin());
    return id;
}

}  // namespace lux::dexvm
