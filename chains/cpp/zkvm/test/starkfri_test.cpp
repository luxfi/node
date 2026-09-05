// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// starkfri_test.cpp — the strict-PQ verification seam itself.
//
// UNBOUND MEANS REFUSE, and the refusal is distinguishable from "this proof did
// not verify" — an operator has to be able to tell a missing binding from a bad
// proof. There is no state in which a structurally well-formed proof is accepted
// without the real verifier, so there is no forgery oracle.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/zkvm/starkfri.hpp"

using namespace lux::zkvm;
using namespace lux::zkvm::test;

namespace {

Bytes framed(const std::string& payload) {
    Bytes out = b(starkfri::kMagicHeader);
    const Bytes p = b(payload);
    out.insert(out.end(), p.begin(), p.end());
    return out;
}

void unbound_refuses() {
    std::printf("with nothing bound, nothing verifies\n");
    starkfri::register_verifier(nullptr);
    check(!starkfri::registered(), "no verifier is registered");
    check_err(starkfri::verify(view(framed("payload")), {}),
              starkfri::kErrVerifierNotRegistered,
              "a well-formed proof is refused, and the refusal names the missing binding");
}

void the_magic_header_is_structural() {
    std::printf("\nthe magic header is checked BEFORE any callback\n");
    bool called = false;
    starkfri::register_verifier([&called](std::uint8_t, ByteView, ByteView) -> wire::Result<bool> {
        called = true;
        return true;
    });
    check_err(starkfri::verify(view(Bytes(1024, 0)), {}), starkfri::kErrInvalidProof,
              "a proof without the header is refused");
    check(!called, "and the verifier was never asked");
    check_err(starkfri::verify(view(b("P3Q")), {}), starkfri::kErrInvalidProof,
              "a proof shorter than the header is refused too");
    starkfri::register_verifier(nullptr);
}

void bound_decides() {
    std::printf("\nwith a verifier bound, the verifier decides\n");

    std::uint8_t saw_version = 0;
    Bytes saw_proof, saw_pub;
    starkfri::register_verifier([&](std::uint8_t v, ByteView proof,
                                    ByteView pub) -> wire::Result<bool> {
        saw_version = v;
        saw_proof.assign(proof.begin(), proof.end());
        saw_pub.assign(pub.begin(), pub.end());
        return true;
    });

    const Bytes proof = framed("body");
    const Bytes pub = b("public");
    auto ok = starkfri::verify(view(proof), view(pub));
    check_ok(ok, "an accepting verifier accepts");
    if (ok) check(*ok, "with a true verdict");
    check(saw_version == starkfri::kVersionV1, "the wire version is the one and only one");
    check_eq(hex_of(saw_proof), hex_of(proof), "the whole proof reaches the verifier");
    check_eq(hex_of(saw_pub), hex_of(pub), "and so do the public inputs, unaltered");

    starkfri::register_verifier(
        [](std::uint8_t, ByteView, ByteView) -> wire::Result<bool> { return false; });
    auto no = starkfri::verify(view(proof), view(pub));
    check_ok(no, "a rejecting verifier answers rather than failing");
    if (no) check(!*no, "with a false verdict");

    starkfri::register_verifier([](std::uint8_t, ByteView, ByteView) -> wire::Result<bool> {
        return std::unexpected("the bridge fell over");
    });
    check_err(starkfri::verify(view(proof), view(pub)), "the bridge fell over",
              "and an internal failure is reported as one, not as a verdict");
    starkfri::register_verifier(nullptr);
}

void the_two_registrations_do_not_clobber_each_other() {
    std::printf("\n\"this IS the verifier\" and \"be the verifier iff nobody volunteered\"\n");
    starkfri::register_verifier(nullptr);

    check(starkfri::register_default_verifier(
              [](std::uint8_t, ByteView, ByteView) -> wire::Result<bool> { return false; }),
          "a default installs when nothing is registered");
    check(!starkfri::register_default_verifier(
              [](std::uint8_t, ByteView, ByteView) -> wire::Result<bool> { return true; }),
          "and a second default does NOT clobber the first");

    auto verdict = starkfri::verify(view(framed("x")), {});
    check_ok(verdict, "the first default is the one that answers");
    if (verdict) check(!*verdict, "with its own verdict");

    // The authoritative seam overrides whatever volunteered.
    starkfri::register_verifier(
        [](std::uint8_t, ByteView, ByteView) -> wire::Result<bool> { return true; });
    auto forced = starkfri::verify(view(framed("x")), {});
    check_ok(forced, "the real binding takes over");
    if (forced) check(*forced, "and its verdict is the chain's");

    check(!starkfri::register_default_verifier(nullptr), "a null default installs nothing");
    starkfri::register_verifier(nullptr);
    check(!starkfri::registered(), "and clearing leaves nothing registered");
}

}  // namespace

int main() {
    unbound_refuses();
    the_magic_header_is_structural();
    bound_decides();
    the_two_registrations_do_not_clobber_each_other();
    return report("starkfri");
}
