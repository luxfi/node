// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// id_test.cpp — the identifiers and every rendering of them.
//
// These are consensus-visible: a record persists its chain id as cb58 and a
// committee member as "NodeID-<cb58>", so a port that renders either differently
// writes a different database from the chain it is meant to agree with. The
// cross-language check is in differential_test; this file covers the edges a
// golden vector does not reach — the empty input, the round trip, the refusal.

#include "check.hpp"

#include "lux/fhevm/records.hpp"

using namespace lux::fhevm;
using namespace lux::fhevm::test;

namespace {

// The FIPS 180-4 vectors, so sha256 is checked against the standard rather than
// against itself.
void sha256_vectors() {
    check_eq(hex_of(sha256(view(std::string_view("")))),
             "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
             "sha256(\"\")");
    check_eq(hex_of(sha256(view(std::string_view("abc")))),
             "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
             "sha256(\"abc\")");
    check_eq(hex_of(sha256(view(std::string_view(
                 "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq")))),
             "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1",
             "sha256 of the 56-byte vector, the one that spills a block");

    // A megabyte of 'a', fed in awkward pieces: the incremental hasher must
    // agree with the one-shot, or a derivation that hashes fields one at a time
    // differs from one that concatenates first.
    Bytes big(1000000, 'a');
    Hasher h;
    std::size_t i = 0;
    for (std::size_t step : {1u, 63u, 64u, 65u, 127u}) {
        while (i + step <= big.size()) {
            h.write(ByteView(big.data() + i, step));
            i += step;
            if (i > big.size() / 2) break;
        }
    }
    h.write(ByteView(big.data() + i, big.size() - i));
    check_eq(hex_of(h.sum()), hex_of(sha256(view(big))),
             "an incrementally-fed million bytes hash as one buffer");
    check_eq(hex_of(sha256(view(big))),
             "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0",
             "and match the standard's million-'a' vector");
}

void hex_round_trips() {
    Bytes b;
    check(from_hex("", &b) && b.empty(), "empty hex is empty bytes");
    check(from_hex("0x00ff", &b) && b.size() == 2 && b[0] == 0 && b[1] == 0xff, "0x prefix");
    check(from_hex("00FF", &b) && b[1] == 0xff, "upper case");
    check(!from_hex("0", &b), "an odd digit count is not hex");
    check(!from_hex("zz", &b), "a non-digit is not hex");
    check_eq(hex(view(Bytes{0x00, 0x0f, 0xf0, 0xff})), "000ff0ff", "hex renders lower case");
}

void base64_matches_go() {
    // Go's encoding/base64 StdEncoding, which is what encoding/json writes a
    // []byte as.
    struct Case {
        const char* raw;
        const char* want;
    } cases[] = {
        {"", ""}, {"f", "Zg=="}, {"fo", "Zm8="}, {"foo", "Zm9v"},
        {"foob", "Zm9vYg=="}, {"fooba", "Zm9vYmE="}, {"foobar", "Zm9vYmFy"},
        {"network-fhe-public-key", "bmV0d29yay1maGUtcHVibGljLWtleQ=="},
    };
    for (const auto& c : cases) {
        std::string_view raw(c.raw);
        check_eq(base64(view(raw)), c.want, std::string("base64(\"") + c.raw + "\")");
        Bytes back;
        check(base64_decode(c.want, &back) &&
                  std::string(back.begin(), back.end()) == std::string(raw),
              std::string("and it reads back"));
    }
    Bytes out;
    check(!base64_decode("Zg=", &out), "a length that is not a multiple of four is refused");
    check(!base64_decode("Z!==", &out), "an alphabet violation is refused");
    check(!base64_decode("Z===", &out), "three pad characters is not a quantum");
    check(!base64_decode("Zg==Zg==", &out), "padding in the middle is refused");
}

void cb58_matches_go() {
    // The addresses in the golden vectors are cb58 words the Go chain wrote;
    // these are the structural properties around them.
    Bytes empty;
    std::string enc = cb58(view(empty));
    Bytes back;
    check(cb58_decode(enc, &back) && back.empty(), "cb58 round-trips the empty payload");

    Bytes twenty(20, 0);
    twenty[19] = 1;
    enc = cb58(view(twenty));
    check(cb58_decode(enc, &back) && back == twenty, "cb58 round-trips leading zeros");
    check(enc.rfind("1111", 0) == 0, "and renders each leading zero byte as '1'");

    // A checksum that does not match is refused: the four trailing bytes are
    // the whole point of the encoding.
    std::string corrupt = enc;
    corrupt[corrupt.size() - 1] = (corrupt[corrupt.size() - 1] == 'A') ? 'B' : 'A';
    check(!cb58_decode(corrupt, &back), "a bad checksum is refused");
    check(!cb58_decode("0OIl", &back), "characters base58 does not have are refused");
    check(!cb58_decode("1", &back), "a word too short to carry a checksum is refused");
}

void native_chain_names() {
    // Go's ids.ID renders a well-known chain by NAME rather than as cb58, and a
    // record persists that string — so both spellings have to be known here.
    for (char letter : std::string_view("PCXQABMFZGIKD")) {
        Id id{};
        id[31] = std::uint8_t(letter);
        std::string want = std::string(32, '1') + letter;
        check_eq(id_string(id), want, std::string("the ") + letter + "-chain names itself");
    }
    Id empty{};
    check(native_chain_string(empty).empty(), "the empty id is not a native chain");
    check_eq(id_string(empty), cb58(view(empty)), "so it renders as cb58");

    Id nearly{};
    nearly[0] = 1;
    nearly[31] = 'P';
    check(native_chain_string(nearly).empty(),
          "a non-zero leading byte is not the P-chain however it ends");
}

void node_ids() {
    NodeId n{};
    for (std::size_t i = 0; i < n.size(); ++i) n[i] = std::uint8_t(i);
    std::string s = node_id_string(n);
    check(s.rfind("NodeID-", 0) == 0, "a node id carries its prefix");
    NodeId back{};
    check(node_id_from_string(s, &back) && back == n, "and reads back");
    check(!node_id_from_string("NodeID-", &back), "the bare prefix is not a node id");
    check(!node_id_from_string(s.substr(7), &back), "and the word without it is not either");
    check(!node_id_from_string("NodeID-" + cb58(view(Bytes(19, 0))), &back),
          "nor is a word of the wrong width");
}

void addresses() {
    // address_of is the derivation that both authenticates a payer and
    // recognises a committee member, so it is the same twenty bytes either way.
    Bytes key(1952, 0x11);
    Account a = address_of(view(key));
    check_eq(hex_of(a), hex_of(address_of(view(key))), "address_of is a function of the key");
    Id h = sha256(view(key));
    check(std::equal(a.begin(), a.end(), h.begin()),
          "and it is the first twenty bytes of the key's digest");

    key[0] = 0x12;
    check(address_of(view(key)) != a, "a different key is a different account");
}

}  // namespace

int main() {
    std::printf("fhevm — the identifiers, and the renderings a record persists\n\n");
    sha256_vectors();
    hex_round_trips();
    base64_matches_go();
    cb58_matches_go();
    native_chain_names();
    node_ids();
    addresses();
    return report("id");
}
