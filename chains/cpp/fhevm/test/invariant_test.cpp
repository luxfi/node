// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// invariant_test.cpp — the F-Chain's defining property, checked structurally.
//
// F coordinates confidential compute without ever holding the confidential
// part. No ciphertext body, no plaintext, no FHE secret key, no decryption
// share. Three things hold that line here:
//
//  1. The persisted schema is PINNED, field for field, in records_test — a
//     record that gains a field fails until someone writes it down, at which
//     point "should F be storing that?" is a question somebody answers rather
//     than one an omission answers.
//
//  2. This file SCANS the package's own source and fails if it names any
//     identifier by which F could generate a key, produce or combine shares, or
//     decrypt. Go does the same walk over its AST; here it is over the text
//     with comments and string literals stripped, so a comment that NAMES one
//     of these to explain the invariant is not flagged — only real code is.
//
//  3. The same scan forbids a wall clock, a random source and anything
//     machine-dependent in the apply path, because a record F wrote from any of
//     those would differ between two validators replaying one block.
//
// The Go package walks its type graph reflectively for a fourth check. C++ has
// no reflection, and rather than fake one this port pins the SIZE of every
// persisted record beside its key list (records_test) — which catches the same
// thing the walk does: a field added and not written down.

#include "check.hpp"
#include "fixtures.hpp"

#include <filesystem>
#include <fstream>
#include <sstream>

using namespace lux::fhevm;
using namespace lux::fhevm::test;

namespace {

#ifndef FHEVM_SOURCE_DIR
#error "FHEVM_SOURCE_DIR must name the package's own source tree"
#endif

// strip removes comments and string literals, so the scan sees CODE. A doc
// comment that names a forbidden identifier to explain why it is forbidden is
// not a use of it.
std::string strip(const std::string& in) {
    std::string out;
    out.reserve(in.size());
    enum class St { Code, Line, Block, Str, Chr, Raw } st = St::Code;
    std::string raw_delim;
    for (std::size_t i = 0; i < in.size(); ++i) {
        char c = in[i];
        char n = (i + 1 < in.size()) ? in[i + 1] : '\0';
        switch (st) {
            case St::Code:
                if (c == '/' && n == '/') {
                    st = St::Line;
                    ++i;
                } else if (c == '/' && n == '*') {
                    st = St::Block;
                    ++i;
                } else if (c == 'R' && n == '"') {
                    // A raw string literal: R"delim( ... )delim".
                    std::size_t open = in.find('(', i + 2);
                    if (open == std::string::npos) {
                        out.push_back(c);
                        break;
                    }
                    raw_delim = ")" + in.substr(i + 2, open - (i + 2)) + "\"";
                    i = open;
                    st = St::Raw;
                } else if (c == '"') {
                    st = St::Str;
                } else if (c == '\'') {
                    st = St::Chr;
                } else {
                    out.push_back(c);
                }
                break;
            case St::Line:
                if (c == '\n') {
                    st = St::Code;
                    out.push_back('\n');
                }
                break;
            case St::Block:
                if (c == '*' && n == '/') {
                    st = St::Code;
                    ++i;
                }
                break;
            case St::Str:
                if (c == '\\') {
                    ++i;
                } else if (c == '"') {
                    st = St::Code;
                }
                break;
            case St::Chr:
                if (c == '\\') {
                    ++i;
                } else if (c == '\'') {
                    st = St::Code;
                }
                break;
            case St::Raw:
                if (in.compare(i, raw_delim.size(), raw_delim) == 0) {
                    i += raw_delim.size() - 1;
                    st = St::Code;
                }
                break;
        }
    }
    return out;
}

std::string read_file(const std::filesystem::path& p) {
    std::ifstream f(p);
    std::ostringstream ss;
    ss << f.rdbuf();
    return ss.str();
}

// The package's own sources: what F compiles into a node. Test files are not
// part of it.
std::vector<std::filesystem::path> package_sources() {
    std::vector<std::filesystem::path> out;
    std::filesystem::path root(FHEVM_SOURCE_DIR);
    for (const char* dir : {"src", "include/lux/fhevm"}) {
        for (const auto& e : std::filesystem::directory_iterator(root / dir)) {
            if (!e.is_regular_file()) continue;
            std::string name = e.path().filename().string();
            if (name.size() > 4 && (name.substr(name.size() - 4) == ".cpp" ||
                                    name.substr(name.size() - 4) == ".hpp")) {
                out.push_back(e.path());
            }
        }
    }
    std::sort(out.begin(), out.end());
    return out;
}

bool names(const std::string& code, const std::string& ident) {
    // An identifier, not a substring of a longer one.
    auto is_word = [](char c) {
        return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
               c == '_';
    };
    std::size_t at = 0;
    while ((at = code.find(ident, at)) != std::string::npos) {
        bool left = at == 0 || !is_word(code[at - 1]);
        std::size_t end = at + ident.size();
        bool right = end >= code.size() || !is_word(code[end]);
        if (left && right) return true;
        at = end;
    }
    return false;
}

void the_package_names_nothing_that_produces_a_secret() {
    // Any function or type by which F could generate a key, produce or combine
    // shares, decrypt, or persist through a wall-clock-stamped registry.
    struct Forbidden {
        const char* ident;
        const char* why;
    };
    const Forbidden exact[] = {
        {"GenerateKey", "key generation"},
        {"generate_key", "key generation"},
        {"NewSecretKey", "secret key construction"},
        {"secret_key", "F holds no secret key"},
        {"private_key", "F holds no private key"},
        {"decapsulate", "KEM decapsulation uses the private key"},
        {"encapsulate", "F performs no cryptographic compute"},
        {"zeroize", "only needed if secret material were held"},
        {"reconstruct", "share reconstruction"},
        {"combine_shares", "share combination"},
        {"recover_secret", "secret recovery"},
        {"generate_share", "would make this node a share producer"},
        {"contribute_share", "would make this node a share consumer"},
        {"initiate_decryption", "decryption happens off-chain, on the committee"},
        {"threshold_decryptor", "decryption happens off-chain, on the committee"},
        {"plaintext", "F never holds a plaintext"},
        {"ciphertext_body", "F never holds a ciphertext body"},
        {"new_registry", "a wall-clock-stamped registry cannot back a state root"},
    };

    int scanned = 0;
    bool clean = true;
    for (const auto& path : package_sources()) {
        std::string code = strip(read_file(path));
        ++scanned;
        for (const auto& f : exact) {
            if (!names(code, f.ident)) continue;
            std::printf("        %s names %s — %s\n", path.filename().string().c_str(), f.ident,
                        f.why);
            clean = false;
        }
    }
    check(scanned > 15, "the scan actually read the package's sources");
    check(clean, "the package names nothing by which it could produce a secret");

    // The control: the scanner can see an identifier when one is there, or it
    // proves nothing about the files above.
    check(names(strip("void f() { plaintext(); }"), "plaintext"),
          "the scanner finds an identifier in code");
    check(!names(strip("// plaintext is never held here\nvoid f() {}"), "plaintext"),
          "and does not find one in a comment that explains the rule");
    check(!names(strip("const char* s = \"plaintext\";"), "plaintext"),
          "nor in a string literal");
    check(!names(strip("auto s = R\"(plaintext)\";"), "plaintext"),
          "nor in a raw one");

    // And NOTHING in the package can sign or make a key. Verification is the
    // whole of what F performs, so the two directions are not both available to
    // it: a library that could sign would have to be handed something to sign
    // with, and F is never handed anything.
    int signers = 0;
    for (const auto& path : package_sources()) {
        std::string code = strip(read_file(path));
        for (const char* ident : {"sign_65", "sign_44", "sign_87", "keypair_65", "keypair_44",
                                  "keypair_87"}) {
            if (!names(code, ident)) continue;
            std::printf("        %s can sign (%s)\n", path.filename().string().c_str(), ident);
            ++signers;
        }
    }
    check_eq(std::uint64_t(signers), 0, "and nothing in the package can sign or make a key");
}

void the_apply_path_reads_no_clock_and_no_randomness() {
    // The one clock F may read is its own, and only OUTSIDE consensus —
    // admission filtering and the read surface, where a wrong answer costs a
    // retry rather than a fork. Nothing under acceptance may read it.
    //
    // The apply path: what acceptance reaches. transaction.cpp holds the
    // effects, block.cpp the settlement, records.cpp the derivations, and the
    // batch lives in vm.cpp with admission — so vm.cpp is scanned for the
    // sources of nondeterminism rather than for clock use, which it legitimately
    // has.
    struct Forbidden {
        const char* ident;
        const char* why;
    };
    const Forbidden anywhere[] = {
        {"rand", "randomness in an applied path diverges between validators"},
        {"random_device", "the same"},
        {"mt19937", "the same"},
        {"getentropy", "the same"},
        {"thread", "a result must not depend on how many run"},
        {"hardware_concurrency", "a result must not depend on the machine"},
    };
    const char* apply_path[] = {"src/transaction.cpp", "src/block.cpp", "src/records.cpp"};

    bool clean = true;
    for (const char* rel : apply_path) {
        std::string code = strip(read_file(std::filesystem::path(FHEVM_SOURCE_DIR) / rel));
        check(!code.empty(), std::string("the scan read ") + rel);
        for (const auto& f : anywhere) {
            if (names(code, f.ident)) {
                std::printf("        %s names %s — %s\n", rel, f.ident, f.why);
                clean = false;
            }
        }
        // The clock is the specific hazard: a record stamped from a validator's
        // clock rather than the accepting block's timestamp. What is forbidden
        // is READING one — `now` itself is the parameter the block's timestamp
        // arrives in, which is the rule rather than a breach of it, so the scan
        // names the readers instead of the word.
        for (const char* reader : {"system_clock", "steady_clock", "real_now", "high_resolution_clock",
                                   "gettimeofday", "clock_gettime"}) {
            if (names(code, reader)) {
                std::printf("        %s names %s — a stored field must come from the block\n", rel,
                            reader);
                clean = false;
            }
        }
        // The VM's OWN clock is deliberately not forbidden. Verify reads it once
        // — to bound how far ahead of local time a proposer may stamp a block —
        // and that is the one place a chain trusts a clock, as a bound. What
        // must never happen is a STORED field taking its value from it, and no
        // text scan can prove that: replay_test does, by driving one chain onto
        // two nodes whose clocks differ by years and requiring their databases
        // to come out byte-identical.
    }
    check(clean, "nothing under acceptance reads a clock, a random source, or the machine");

    // And the whole package pulls in no source of randomness at all.
    bool no_random = true;
    for (const auto& path : package_sources()) {
        std::string text = read_file(path);
        for (const char* header : {"<random>", "<thread>", "<chrono>"}) {
            if (text.find(std::string("#include ") + header) == std::string::npos) continue;
            // <chrono> is legitimate in exactly one place: the clock the VM
            // holds, which is read outside consensus.
            if (std::string(header) == "<chrono>" && path.filename() == "vm.cpp") continue;
            std::printf("        %s includes %s\n", path.filename().string().c_str(), header);
            no_random = false;
        }
    }
    check(no_random, "and the package includes no randomness and no threads of its own");
}

void the_read_surface_offers_no_way_to_ask_for_what_f_does_not_hold() {
    // The public surface must offer no method a caller could mistake for the
    // plaintext itself. `decrypt` is present and named for what it returns — the
    // RECORD of a decryption request — so the check is against the things it is
    // not.
    std::string vm_header =
        strip(read_file(std::filesystem::path(FHEVM_SOURCE_DIR) / "include/lux/fhevm/vm.hpp"));
    std::string service_header =
        strip(read_file(std::filesystem::path(FHEVM_SOURCE_DIR) / "include/lux/fhevm/service.hpp"));

    bool clean = true;
    for (const char* bad : {"private_key", "secret_key", "plaintext", "share", "seed", "body",
                            "reconstruct", "reencrypt", "evaluate"}) {
        if (names(vm_header, bad)) {
            std::printf("        the VM surface offers %s\n", bad);
            clean = false;
        }
        if (names(service_header, bad)) {
            std::printf("        the read surface offers %s\n", bad);
            clean = false;
        }
    }
    check(clean, "neither surface offers access to material F does not hold");
    check(names(service_header, "decrypt"),
          "the one decrypt-named call is there, and it reads the request record");
}

void a_record_holds_only_public_coordinates() {
    // What the four records are made of: hashes, addresses, bitmasks, sizes,
    // epochs, timestamps. Each is a public coordinate, and the marshalled form
    // is the whole of what F persists — so the values below are everything a
    // node writes about one confidential value.
    Committee c = new_committee(1);
    CiphertextRecord ct;
    ct.handle = digest_of("h");
    ct.owner = c.keys[0].addr;
    ct.digest = digest_of("d");
    ct.scheme = "ckks-n14";
    ct.size = 4096;
    std::string doc = marshal(ct);

    // The digest is there — that is how a client checks a body it fetched from
    // off-chain storage — and the body is not, because F never has it.
    check(doc.find("\"digest\"") != std::string::npos, "a record carries the body's digest");
    check(doc.find("\"body\"") == std::string::npos, "and never the body");
    check(doc.find(base64(view(Bytes(32, 0x11)))) == std::string::npos,
          "there is no opaque blob in it at all");

    // A payload that tries to smuggle one is refused before it is priced, let
    // alone stored — which is the check that actually holds the line.
    refused(decode_register(view(std::string_view(R"({"digest":[1],"type":1,"level":0,"size":1,"body":"AAAA"})"))),
            Err::InvalidPayload, "and a payload that tries to carry one is refused");
}

}  // namespace

int main() {
    std::printf("fhevm — the ciphertext-body invariant, checked structurally\n\n");
    the_package_names_nothing_that_produces_a_secret();
    the_apply_path_reads_no_clock_and_no_randomness();
    the_read_surface_offers_no_way_to_ask_for_what_f_does_not_hold();
    a_record_holds_only_public_coordinates();
    return report("invariant");
}
