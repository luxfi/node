// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/fhevm/id.hpp"

#include <algorithm>
#include <cstring>

namespace lux::fhevm {
namespace {

constexpr std::uint32_t kK[64] = {
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2};

inline std::uint32_t rotr(std::uint32_t x, int n) { return (x >> n) | (x << (32 - n)); }

void compress(std::array<std::uint32_t, 8>& h, const std::uint8_t* p) {
    std::uint32_t w[64];
    for (int i = 0; i < 16; ++i) {
        w[i] = (std::uint32_t(p[i * 4]) << 24) | (std::uint32_t(p[i * 4 + 1]) << 16) |
               (std::uint32_t(p[i * 4 + 2]) << 8) | std::uint32_t(p[i * 4 + 3]);
    }
    for (int i = 16; i < 64; ++i) {
        std::uint32_t s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >> 3);
        std::uint32_t s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >> 10);
        w[i] = w[i - 16] + s0 + w[i - 7] + s1;
    }
    std::uint32_t a = h[0], b = h[1], c = h[2], d = h[3];
    std::uint32_t e = h[4], f = h[5], g = h[6], hh = h[7];
    for (int i = 0; i < 64; ++i) {
        std::uint32_t s1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
        std::uint32_t ch = (e & f) ^ (~e & g);
        std::uint32_t t1 = hh + s1 + ch + kK[i] + w[i];
        std::uint32_t s0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
        std::uint32_t maj = (a & b) ^ (a & c) ^ (b & c);
        std::uint32_t t2 = s0 + maj;
        hh = g;
        g = f;
        f = e;
        e = d + t1;
        d = c;
        c = b;
        b = a;
        a = t1 + t2;
    }
    h[0] += a;
    h[1] += b;
    h[2] += c;
    h[3] += d;
    h[4] += e;
    h[5] += f;
    h[6] += g;
    h[7] += hh;
}

constexpr char kHexDigits[] = "0123456789abcdef";
constexpr char kB58[] = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz";
constexpr char kB64[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

int hex_val(char c) {
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    if (c >= 'A' && c <= 'F') return c - 'A' + 10;
    return -1;
}

int b58_val(char c) {
    for (int i = 0; i < 58; ++i) {
        if (kB58[i] == c) return i;
    }
    return -1;
}

int b64_val(char c) {
    for (int i = 0; i < 64; ++i) {
        if (kB64[i] == c) return i;
    }
    return -1;
}

constexpr const char* kNativePrefix = "11111111111111111111111111111111";

}  // namespace

Hasher::Hasher() {
    h_ = {0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
          0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19};
}

void Hasher::write(ByteView b) {
    // An empty span may carry a null pointer, and memcpy is not defined for one
    // even at length zero. A domain tag with no bytes and a nil public key both
    // arrive here, so this is the ordinary case rather than a corner.
    if (b.empty()) return;
    total_ += b.size();
    std::size_t i = 0;
    if (block_len_ > 0) {
        std::size_t take = std::min(std::size_t(64) - block_len_, b.size());
        std::memcpy(block_.data() + block_len_, b.data(), take);
        block_len_ += take;
        i = take;
        if (block_len_ == 64) {
            compress(h_, block_.data());
            block_len_ = 0;
        }
    }
    for (; i + 64 <= b.size(); i += 64) compress(h_, b.data() + i);
    if (i < b.size()) {
        std::memcpy(block_.data(), b.data() + i, b.size() - i);
        block_len_ = b.size() - i;
    }
}

void Hasher::be64(std::uint64_t v) {
    std::uint8_t u[8];
    for (int i = 0; i < 8; ++i) u[i] = std::uint8_t(v >> (56 - 8 * i));
    write(ByteView(u, 8));
}

void Hasher::len_prefixed(ByteView b) {
    be64(std::uint64_t(b.size()));
    write(b);
}

Id Hasher::sum() {
    // The padding is built here rather than pushed through write(), so the
    // message length is fixed before a byte of it is added and sum() cannot
    // teach itself a longer message than it hashed.
    const std::uint64_t bits = total_ * 8;
    std::uint8_t tail[128] = {0x80};
    const std::size_t pad_len = (block_len_ < 56) ? (56 - block_len_) : (120 - block_len_);
    for (int i = 0; i < 8; ++i) tail[pad_len + std::size_t(i)] = std::uint8_t(bits >> (56 - 8 * i));

    std::array<std::uint32_t, 8> h = h_;
    std::array<std::uint8_t, 128> last{};
    std::memcpy(last.data(), block_.data(), block_len_);
    std::memcpy(last.data() + block_len_, tail, pad_len + 8);
    const std::size_t blocks = (block_len_ + pad_len + 8) / 64;
    for (std::size_t i = 0; i < blocks; ++i) compress(h, last.data() + i * 64);

    Id out{};
    for (int i = 0; i < 8; ++i) {
        out[std::size_t(i * 4)] = std::uint8_t(h[std::size_t(i)] >> 24);
        out[std::size_t(i * 4 + 1)] = std::uint8_t(h[std::size_t(i)] >> 16);
        out[std::size_t(i * 4 + 2)] = std::uint8_t(h[std::size_t(i)] >> 8);
        out[std::size_t(i * 4 + 3)] = std::uint8_t(h[std::size_t(i)]);
    }
    return out;
}

Id sha256(ByteView data) {
    Hasher h;
    h.write(data);
    return h.sum();
}

std::string hex(ByteView b) {
    std::string out;
    out.reserve(b.size() * 2);
    for (std::uint8_t c : b) {
        out.push_back(kHexDigits[c >> 4]);
        out.push_back(kHexDigits[c & 0x0f]);
    }
    return out;
}

bool from_hex(std::string_view s, Bytes* out) {
    if (s.size() >= 2 && s[0] == '0' && s[1] == 'x') s.remove_prefix(2);
    if (s.size() % 2 != 0) return false;
    Bytes b;
    b.reserve(s.size() / 2);
    for (std::size_t i = 0; i < s.size(); i += 2) {
        int hi = hex_val(s[i]), lo = hex_val(s[i + 1]);
        if (hi < 0 || lo < 0) return false;
        b.push_back(std::uint8_t((hi << 4) | lo));
    }
    *out = std::move(b);
    return true;
}

namespace {

// base58 over a big-endian byte string, leading zero bytes rendered as '1'.
std::string b58_encode(ByteView b) {
    std::size_t zeros = 0;
    while (zeros < b.size() && b[zeros] == 0) ++zeros;
    std::vector<std::uint8_t> digits;
    digits.reserve(b.size() * 138 / 100 + 1);
    for (std::size_t i = zeros; i < b.size(); ++i) {
        int carry = b[i];
        for (auto& d : digits) {
            int cur = int(d) * 256 + carry;
            d = std::uint8_t(cur % 58);
            carry = cur / 58;
        }
        while (carry > 0) {
            digits.push_back(std::uint8_t(carry % 58));
            carry /= 58;
        }
    }
    std::string out(zeros, '1');
    for (auto it = digits.rbegin(); it != digits.rend(); ++it) out.push_back(kB58[*it]);
    return out;
}

bool b58_decode(std::string_view s, Bytes* out) {
    std::size_t zeros = 0;
    while (zeros < s.size() && s[zeros] == '1') ++zeros;
    std::vector<std::uint8_t> bytes;
    for (std::size_t i = zeros; i < s.size(); ++i) {
        int v = b58_val(s[i]);
        if (v < 0) return false;
        int carry = v;
        for (auto& d : bytes) {
            int cur = int(d) * 58 + carry;
            d = std::uint8_t(cur & 0xff);
            carry = cur >> 8;
        }
        while (carry > 0) {
            bytes.push_back(std::uint8_t(carry & 0xff));
            carry >>= 8;
        }
    }
    Bytes res(zeros, 0);
    for (auto it = bytes.rbegin(); it != bytes.rend(); ++it) res.push_back(*it);
    *out = std::move(res);
    return true;
}

}  // namespace

std::string cb58(ByteView b) {
    Bytes buf(b.begin(), b.end());
    Id ck = sha256(b);
    buf.insert(buf.end(), ck.end() - 4, ck.end());
    return b58_encode(view(buf));
}

bool cb58_decode(std::string_view s, Bytes* out) {
    Bytes raw;
    if (!b58_decode(s, &raw)) return false;
    if (raw.size() < 4) return false;
    Bytes payload(raw.begin(), raw.end() - 4);
    Id ck = sha256(view(payload));
    for (int i = 0; i < 4; ++i) {
        if (raw[raw.size() - 4 + std::size_t(i)] != ck[ck.size() - 4 + std::size_t(i)]) return false;
    }
    *out = std::move(payload);
    return true;
}

std::string base64(ByteView b) {
    std::string out;
    out.reserve((b.size() + 2) / 3 * 4);
    std::size_t i = 0;
    for (; i + 3 <= b.size(); i += 3) {
        std::uint32_t v = (std::uint32_t(b[i]) << 16) | (std::uint32_t(b[i + 1]) << 8) | b[i + 2];
        out.push_back(kB64[(v >> 18) & 0x3f]);
        out.push_back(kB64[(v >> 12) & 0x3f]);
        out.push_back(kB64[(v >> 6) & 0x3f]);
        out.push_back(kB64[v & 0x3f]);
    }
    if (i + 1 == b.size()) {
        std::uint32_t v = std::uint32_t(b[i]) << 16;
        out.push_back(kB64[(v >> 18) & 0x3f]);
        out.push_back(kB64[(v >> 12) & 0x3f]);
        out.push_back('=');
        out.push_back('=');
    } else if (i + 2 == b.size()) {
        std::uint32_t v = (std::uint32_t(b[i]) << 16) | (std::uint32_t(b[i + 1]) << 8);
        out.push_back(kB64[(v >> 18) & 0x3f]);
        out.push_back(kB64[(v >> 12) & 0x3f]);
        out.push_back(kB64[(v >> 6) & 0x3f]);
        out.push_back('=');
    }
    return out;
}

bool base64_decode(std::string_view s, Bytes* out) {
    if (s.size() % 4 != 0) return false;
    Bytes res;
    res.reserve(s.size() / 4 * 3);
    for (std::size_t i = 0; i < s.size(); i += 4) {
        int v[4];
        int pad = 0;
        for (int j = 0; j < 4; ++j) {
            char c = s[i + std::size_t(j)];
            if (c == '=') {
                // Padding is only legal in the final quantum's last two slots.
                if (i + 4 != s.size() || j < 2) return false;
                v[j] = 0;
                ++pad;
                continue;
            }
            if (pad > 0) return false;
            v[j] = b64_val(c);
            if (v[j] < 0) return false;
        }
        std::uint32_t n = (std::uint32_t(v[0]) << 18) | (std::uint32_t(v[1]) << 12) |
                          (std::uint32_t(v[2]) << 6) | std::uint32_t(v[3]);
        res.push_back(std::uint8_t(n >> 16));
        if (pad < 2) res.push_back(std::uint8_t(n >> 8));
        if (pad < 1) res.push_back(std::uint8_t(n));
    }
    *out = std::move(res);
    return true;
}

std::string account_string(const Account& a) { return cb58(view(a)); }

std::string node_id_string(const NodeId& n) { return "NodeID-" + cb58(view(n)); }

bool node_id_from_string(std::string_view s, NodeId* out) {
    constexpr std::string_view kPrefix = "NodeID-";
    if (s.size() <= kPrefix.size() || s.substr(0, kPrefix.size()) != kPrefix) return false;
    Bytes b;
    if (!cb58_decode(s.substr(kPrefix.size()), &b)) return false;
    if (b.size() != out->size()) return false;
    std::copy(b.begin(), b.end(), out->begin());
    return true;
}

std::string native_chain_string(const Id& id) {
    for (std::size_t i = 0; i < 31; ++i) {
        if (id[i] != 0) return {};
    }
    switch (id[31]) {
        case 'P': case 'C': case 'X': case 'Q': case 'A': case 'B':
        case 'M': case 'F': case 'Z': case 'G': case 'I': case 'K': case 'D':
            return std::string(kNativePrefix) + char(id[31]);
        default:
            return {};
    }
}

std::string id_string(const Id& id) {
    std::string native = native_chain_string(id);
    if (!native.empty()) return native;
    return cb58(view(id));
}

}  // namespace lux::fhevm
