// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/groth16.hpp"

#include "bn254_fp.hpp"
#include "bn254_fp2.hpp"
#include "bn254_pairing.hpp"

#include <cstring>

namespace lux::zkvm::groth16 {
namespace {

using namespace lux::crypto::bn254;

// The encoding's metadata lives in the two most significant bits of byte 0.
constexpr std::uint8_t kMask = 0b11 << 6;
constexpr std::uint8_t kUncompressed = 0b00 << 6;
constexpr std::uint8_t kCompressedSmallest = 0b10 << 6;
constexpr std::uint8_t kCompressedLargest = 0b11 << 6;
constexpr std::uint8_t kCompressedInfinity = 0b01 << 6;

constexpr std::size_t kFpBytes = 32;
constexpr std::size_t kG1Compressed = 32, kG1Uncompressed = 64;
constexpr std::size_t kG2Compressed = 64, kG2Uncompressed = 128;

// (p-1)/2 + 1, the threshold a coordinate is "lexicographically largest" at or
// above — i.e. y > (p-1)/2. Same value the reference compares against.
constexpr U256 kHalfPPlusOne{0x9E10460B6C3E7EA4ULL, 0xCBC0B548B438E546ULL, 0xDC2822DB40C0AC2EULL,
                             0x183227397098D014ULL};

// (p-1)/2, the Legendre exponent.
constexpr U256 kLegendreExp{0x9E10460B6C3E7EA3ULL, 0xCBC0B548B438E546ULL, 0xDC2822DB40C0AC2EULL,
                            0x183227397098D014ULL};

// (p-3)/4 is not needed: p ≡ 3 (mod 4), so fp_sqrt is the (p+1)/4 power, and the
// Fp2 root below is the complex method over it.

bool canonical(const U256& v) { return U256::cmp(v, P) < 0; }

bool is_zeroed(std::uint8_t first, ByteView rest) {
    if (first != 0) return false;
    for (std::uint8_t b : rest)
        if (b != 0) return false;
    return true;
}

// lexicographically_largest mirrors the reference's fp.Element predicate: the
// plain (non-Montgomery) value compared against (p-1)/2.
bool lexicographically_largest(const U256& mont) {
    return U256::cmp(from_mont_fp(mont), kHalfPPlusOne) >= 0;
}

// legendre_fp returns 1, 0 or -1 for a Montgomery-form element.
int legendre_fp(const U256& a) {
    if (a.is_zero()) return 0;
    const U256 r = fp_pow(a, kLegendreExp);
    if (r == R_FP) return 1;
    return -1;
}

// fp2_legendre is the Legendre symbol of the NORM, which is how the reference
// decides whether an Fp2 element has a square root.
int fp2_legendre(const Fp2& z) {
    const U256 n = fp_add(fp_sqr(z.a0), fp_sqr(z.a1));
    return legendre_fp(n);
}

// fp2_sqrt returns ONE square root of z (the complex method for p ≡ 3 mod 4,
// Fp2 = Fp[u]/(u²+1)). WHICH of the two roots comes back does not matter: the
// caller decides the sign from the encoding's lexicographic bit and negates if
// needed, so both roots lead to the same decoded point.
bool fp2_sqrt(const Fp2& z, Fp2& out) {
    if (z.a0.is_zero() && z.a1.is_zero()) {
        out = fp2_zero();
        return true;
    }
    if (z.a1.is_zero()) {
        U256 r;
        if (fp_sqrt(z.a0, r)) {
            out = Fp2{r, U256{}};
            return true;
        }
        if (!fp_sqrt(fp_neg(z.a0), r)) return false;
        out = Fp2{U256{}, r};
        return true;
    }

    const U256 norm = fp_add(fp_sqr(z.a0), fp_sqr(z.a1));
    U256 s;
    if (!fp_sqrt(norm, s)) return false;

    const U256 two_inv = fp_inv(fp_add(R_FP, R_FP));
    U256 t = fp_mul(fp_add(z.a0, s), two_inv);
    U256 x0;
    if (!fp_sqrt(t, x0)) {
        t = fp_mul(fp_sub(z.a0, s), two_inv);
        if (!fp_sqrt(t, x0)) return false;
    }
    if (x0.is_zero()) return false;
    const U256 x1 = fp_mul(z.a1, fp_inv(fp_add(x0, x0)));
    out = Fp2{x0, x1};
    return true;
}

Fp2 b_twist() {
    static const Fp2 b = [] {
        const Fp2 three{to_mont_fp(U256{3, 0, 0, 0}), U256{}};
        return fp2_mul_by_nonres_inv(three);
    }();
    return b;
}

wire::Result<U256> read_coord(ByteView b) {
    U256 v = U256::from_be32(b.data());
    if (!canonical(v)) return std::unexpected("invalid field element encoding");
    return to_mont_fp(v);
}

}  // namespace

wire::Result<void> check_g1(const G1& p) {
    // On this curve G1 has cofactor 1, so being on the curve IS being in the
    // prime-order subgroup — which is the same predicate the reference applies.
    if (!g1_is_on_curve(p)) return std::unexpected(kErrOffSubgroup);
    if (p.infinity) return std::unexpected(kErrAtInfinity);
    return {};
}

wire::Result<void> check_g2(const G2& p) {
    if (!g2_is_on_curve(p) || !g2_in_subgroup(p)) return std::unexpected(kErrOffSubgroup);
    if (p.infinity) return std::unexpected(kErrAtInfinity);
    return {};
}

wire::Result<G1> set_bytes_g1(ByteView buf) {
    if (buf.size() < kG1Compressed) return std::unexpected("buffer too short for a G1 point");
    const std::uint8_t mdata = std::uint8_t(buf[0] & kMask);

    if (mdata == kUncompressed && buf.size() < kG1Uncompressed)
        return std::unexpected("buffer too short for an uncompressed G1 point");

    if (mdata == kCompressedInfinity) {
        if (!is_zeroed(std::uint8_t(buf[0] & ~kMask), buf.subspan(1, kG1Compressed - 1)))
            return std::unexpected("invalid infinity encoding");
        return G1{U256{}, U256{}, true};
    }

    if (mdata == kUncompressed) {
        auto x = read_coord(buf.subspan(0, kFpBytes));
        if (!x) return std::unexpected(x.error());
        auto y = read_coord(buf.subspan(kFpBytes, kFpBytes));
        if (!y) return std::unexpected(y.error());
        // All-zero bytes decode to the affine origin, which this curve family
        // reads as infinity — and reports as in-subgroup. check_g1 is where that
        // is refused; the decoder must not silently normalise it away.
        G1 p{*x, *y, x->is_zero() && y->is_zero()};
        if (!g1_is_on_curve(p)) return std::unexpected("invalid point: subgroup check failed");
        return p;
    }

    // Compressed: solve the curve equation for Y and pick the root the two
    // metadata bits name.
    std::uint8_t xb[kFpBytes];
    std::memcpy(xb, buf.data(), kFpBytes);
    xb[0] = std::uint8_t(xb[0] & ~kMask);
    U256 xv = U256::from_be32(xb);
    if (!canonical(xv)) return std::unexpected("invalid field element encoding");
    const U256 x = to_mont_fp(xv);

    U256 y_squared = fp_add(fp_mul(fp_sqr(x), x), fp_three());
    U256 y;
    if (!fp_sqrt(y_squared, y))
        return std::unexpected("invalid compressed coordinate: square root doesn't exist");
    if (lexicographically_largest(y)) {
        if (mdata == kCompressedSmallest) y = fp_neg(y);
    } else {
        if (mdata == kCompressedLargest) y = fp_neg(y);
    }
    G1 p{x, y, false};
    if (!g1_is_on_curve(p)) return std::unexpected("invalid point: subgroup check failed");
    return p;
}

wire::Result<G2> set_bytes_g2(ByteView buf) {
    if (buf.size() < kG2Compressed) return std::unexpected("buffer too short for a G2 point");
    const std::uint8_t mdata = std::uint8_t(buf[0] & kMask);

    if (mdata == kUncompressed && buf.size() < kG2Uncompressed)
        return std::unexpected("buffer too short for an uncompressed G2 point");

    if (mdata == kCompressedInfinity) {
        if (!is_zeroed(std::uint8_t(buf[0] & ~kMask), buf.subspan(1, kG2Compressed - 1)))
            return std::unexpected("invalid infinity encoding");
        return G2{fp2_zero(), fp2_zero(), true};
    }

    if (mdata == kUncompressed) {
        // The encoding is X.A1 ‖ X.A0 ‖ Y.A1 ‖ Y.A0 — the imaginary part first,
        // which is the reference's order and not the struct's.
        auto x1 = read_coord(buf.subspan(0, kFpBytes));
        if (!x1) return std::unexpected(x1.error());
        auto x0 = read_coord(buf.subspan(kFpBytes, kFpBytes));
        if (!x0) return std::unexpected(x0.error());
        auto y1 = read_coord(buf.subspan(2 * kFpBytes, kFpBytes));
        if (!y1) return std::unexpected(y1.error());
        auto y0 = read_coord(buf.subspan(3 * kFpBytes, kFpBytes));
        if (!y0) return std::unexpected(y0.error());
        const Fp2 x{*x0, *x1};
        const Fp2 y{*y0, *y1};
        G2 p{x, y, x.is_zero() && y.is_zero()};
        if (!g2_is_on_curve(p) || !g2_in_subgroup(p))
            return std::unexpected("invalid point: subgroup check failed");
        return p;
    }

    std::uint8_t x1b[kFpBytes];
    std::memcpy(x1b, buf.data(), kFpBytes);
    x1b[0] = std::uint8_t(x1b[0] & ~kMask);
    U256 x1v = U256::from_be32(x1b);
    if (!canonical(x1v)) return std::unexpected("invalid field element encoding");
    auto x0r = read_coord(buf.subspan(kFpBytes, kFpBytes));
    if (!x0r) return std::unexpected(x0r.error());

    const Fp2 x{*x0r, to_mont_fp(x1v)};
    Fp2 y_squared = fp2_add(fp2_mul(fp2_sqr(x), x), b_twist());
    if (fp2_legendre(y_squared) == -1)
        return std::unexpected("invalid compressed coordinate: square root doesn't exist");
    Fp2 y;
    if (!fp2_sqrt(y_squared, y))
        return std::unexpected("invalid compressed coordinate: square root doesn't exist");

    // The Fp2 predicate is the imaginary part's when that is non-zero, and the
    // real part's otherwise.
    const bool largest = y.a1.is_zero() ? lexicographically_largest(y.a0)
                                        : lexicographically_largest(y.a1);
    if (largest) {
        if (mdata == kCompressedSmallest) y = fp2_neg(y);
    } else {
        if (mdata == kCompressedLargest) y = fp2_neg(y);
    }
    G2 p{x, y, false};
    if (!g2_is_on_curve(p) || !g2_in_subgroup(p))
        return std::unexpected("invalid point: subgroup check failed");
    return p;
}

U256 fr_from_bytes(ByteView b) {
    // Big-endian, reduced modulo the group order — for ANY length, which is what
    // the reference does when the bytes are not a canonical 32-byte element.
    U256 acc_m{};  // Montgomery form of 0
    const U256 base = to_mont_fr(U256{256, 0, 0, 0});
    for (std::uint8_t byte : b) {
        acc_m = fr_mul(acc_m, base);
        acc_m = fr_add(acc_m, to_mont_fr(U256{byte, 0, 0, 0}));
    }
    return from_mont_fr(acc_m);
}

wire::Result<Proof> deserialize_proof(ByteView data) {
    if (data.size() < 256) return std::unexpected("proof data too short");

    Proof p;
    auto ar = set_bytes_g1(data.subspan(0, 64));
    if (!ar) return std::unexpected("failed to unmarshal Ar: " + ar.error());
    if (auto c = check_g1(*ar); !c) return std::unexpected("Ar: " + c.error());
    p.ar = *ar;

    auto bs = set_bytes_g2(data.subspan(64, 128));
    if (!bs) return std::unexpected("failed to unmarshal Bs: " + bs.error());
    if (auto c = check_g2(*bs); !c) return std::unexpected("Bs: " + c.error());
    p.bs = *bs;

    auto krs = set_bytes_g1(data.subspan(192, 64));
    if (!krs) return std::unexpected("failed to unmarshal Krs: " + krs.error());
    if (auto c = check_g1(*krs); !c) return std::unexpected("Krs: " + c.error());
    p.krs = *krs;

    return p;
}

wire::Result<VerifyingKey> deserialize_verifying_key(ByteView data) {
    constexpr std::size_t kMin = 64 + 128 + 128 + 128 + 4;
    if (data.size() < kMin) return std::unexpected("verifying key data too short");

    VerifyingKey vk;
    std::size_t off = 0;

    auto alpha = set_bytes_g1(data.subspan(off, 64));
    if (!alpha) return std::unexpected("failed to unmarshal Alpha: " + alpha.error());
    vk.alpha = *alpha;
    off += 64;

    auto beta = set_bytes_g2(data.subspan(off, 128));
    if (!beta) return std::unexpected("failed to unmarshal Beta: " + beta.error());
    vk.beta = *beta;
    off += 128;

    auto gamma = set_bytes_g2(data.subspan(off, 128));
    if (!gamma) return std::unexpected("failed to unmarshal Gamma: " + gamma.error());
    vk.gamma = *gamma;
    off += 128;

    auto delta = set_bytes_g2(data.subspan(off, 128));
    if (!delta) return std::unexpected("failed to unmarshal Delta: " + delta.error());
    vk.delta = *delta;
    off += 128;

    std::uint32_t num_k = 0;
    for (std::size_t i = 0; i < 4; ++i) num_k = (num_k << 8) | data[off + i];
    off += 4;

    // The count is the peer's. It sizes NOTHING until the bytes to back it are
    // in hand.
    if (data.size() < off + std::size_t(num_k) * 64)
        return std::unexpected("insufficient data for K points");

    vk.k.reserve(num_k);
    for (std::uint32_t i = 0; i < num_k; ++i) {
        auto k = set_bytes_g1(data.subspan(off, 64));
        if (!k)
            return std::unexpected("failed to unmarshal K[" + std::to_string(i) +
                                   "]: " + k.error());
        vk.k.push_back(*k);
        off += 64;
    }
    return vk;
}

wire::Result<void> validate_verifying_key(const VerifyingKey& vk) {
    if (auto r = check_g1(vk.alpha); !r) return std::unexpected("Alpha: " + r.error());
    if (auto r = check_g2(vk.beta); !r) return std::unexpected("Beta: " + r.error());
    if (auto r = check_g2(vk.gamma); !r) return std::unexpected("Gamma: " + r.error());
    if (auto r = check_g2(vk.delta); !r) return std::unexpected("Delta: " + r.error());
    for (std::size_t i = 0; i < vk.k.size(); ++i) {
        if (auto r = check_g1(vk.k[i]); !r)
            return std::unexpected("K[" + std::to_string(i) + "]: " + r.error());
    }
    return {};
}

wire::Result<void> verify_pairing(const Proof& proof, const VerifyingKey& vk,
                                  const std::vector<U256>& witness) {
    const std::ptrdiff_t want = std::ptrdiff_t(vk.k.size()) - 1;
    if (std::ptrdiff_t(witness.size()) != want)
        return std::unexpected("public inputs: " + std::to_string(witness.size()) +
                               " supplied, the verifying key's circuit takes " +
                               std::to_string(want));

    // K[0] + sum(witness_i · K[i+1]).
    G1Jac lc = g1_to_jac(vk.k[0]);
    for (std::size_t i = 0; i < witness.size(); ++i) {
        lc = g1_add(lc, g1_scalar_mul(vk.k[i + 1], witness[i]));
    }
    const G1 lc_affine = g1_to_affine(lc);

    // e(A,B) = e(alpha,beta)·e(LC,gamma)·e(C,delta) is asked as the single
    // product e(-A,B)·e(alpha,beta)·e(LC,gamma)·e(C,delta) = 1. The final
    // exponentiation is a homomorphism, so this is the same verdict as four
    // pairings compared pairwise — one Miller loop instead of four.
    G1 neg_a = proof.ar;
    if (!neg_a.infinity) neg_a.y = fp_neg(neg_a.y);

    const G1 p[4] = {neg_a, vk.alpha, lc_affine, proof.krs};
    const G2 q[4] = {proof.bs, vk.beta, vk.gamma, vk.delta};
    if (!multi_pairing_check(p, q, 4)) return std::unexpected(kErrPairingFailed);
    return {};
}

}  // namespace lux::zkvm::groth16
