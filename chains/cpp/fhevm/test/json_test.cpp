// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// json_test.cpp — the decoder's acceptance set, which on this chain is a
// consensus question.
//
// A transaction's payload is opaque bytes the chain keeps verbatim, and whether
// it DECODES decides whether the transaction is valid. So the C++ and Go
// decoders must accept and refuse exactly the same bytes. The cross-language
// checks in differential_test cover what Go actually WRITES; this file covers
// what an adversary might SEND — which is the larger half, because a payload is
// a field an attacker chooses.

#include "check.hpp"

#include "lux/fhevm/json.hpp"
#include "lux/fhevm/transaction.hpp"

using namespace lux::fhevm;
using namespace lux::fhevm::test;

namespace {

ByteView bv(std::string_view s) { return view(s); }

void parses(std::string_view in, const std::string& what) {
    json::Value v;
    std::size_t consumed = 0;
    std::string err;
    check(json::parse(in, &v, &consumed, &err), what);
}

// refuses asks the question the chain actually asks: is this whole buffer ONE
// value and nothing else? Go's decoder answers it the same way — a value
// followed by anything but whitespace is a refusal, not a value with a tail.
void refuses(std::string_view in, const std::string& what) {
    json::Value v;
    std::size_t consumed = 0;
    std::string err;
    bool one = json::parse(in, &v, &consumed, &err) && !json::more(in, consumed);
    check(!one, what);
}

void grammar() {
    parses("{}", "the empty object");
    parses("[]", "the empty array");
    parses("null", "null");
    parses("  \t\n {\"a\":1} ", "leading whitespace");
    parses("0", "a bare zero");
    parses("{\"a\":{\"b\":[1,2,{\"c\":null}]}}", "nesting");
    parses("\"\\u00e9\\ud83d\\ude00\\/\\b\\f\\n\\r\\t\"", "every escape, surrogate pair included");
    parses("-0", "negative zero");
    parses("1e10", "an exponent");
    parses("1.5E-3", "a signed exponent");

    refuses("", "the empty document is not a value");
    refuses("{", "an unterminated object");
    refuses("[1,", "an unterminated array");
    refuses("{\"a\"}", "an object member with no value");
    refuses("{a:1}", "an unquoted key");
    refuses("{'a':1}", "a single-quoted key");
    refuses("[1,]", "a trailing comma");
    refuses("01", "a leading zero");
    refuses("+1", "a leading plus");
    refuses(".5", "a bare fraction");
    refuses("1.", "a fraction with no digits");
    refuses("1e", "an exponent with no digits");
    refuses("\"unterminated", "an unterminated string");
    refuses("\"\\x41\"", "an escape JSON does not have");
    refuses("\"a\nb\"", "a raw control character in a string");
    refuses("tru", "a truncated keyword");
    refuses("NaN", "a value JSON does not have");

    // Trailing content is a refusal, not a shrug — Go's dec.More().
    json::Value v;
    std::size_t consumed = 0;
    std::string err;
    check(json::parse("{} {}", &v, &consumed, &err) && json::more("{} {}", consumed),
          "a second value after the first is visible to the caller");
    check(json::parse("{}   ", &v, &consumed, &err) && !json::more("{}   ", consumed),
          "trailing whitespace is not a second value");
}

void unknown_fields_are_refused() {
    // The rule the ciphertext-body invariant rests on: a member the schema does
    // not describe is REFUSED. A megabyte of body in a "body" member used to
    // decode fine, cost nothing extra, and come back out of the block store.
    refused(decode_revoke(bv(R"({"reason":"x","body":"AAAA"})")), Err::InvalidPayload,
            "a member the schema does not describe");
    refused(decode_register(bv(R"({"digest":[1],"type":1,"level":0,"size":1,"extra":0})")),
            Err::InvalidPayload, "and on every operation");
    // Nested too: the option applies to the whole decode, not to the top level.
    refused(decode_advance(
                bv(R"({"epoch":1,"committee":[{"node_id":"NodeID-11111111111111111111111111111111LpoYY","public_key":null,"weight":1,"index":0,"secret":"x"}],"threshold":1,"publicKey":null})")),
            Err::InvalidPayload, "including inside a committee member");

    accepted(decode_revoke(bv(R"({"reason":"x"})")), "the control: exactly the schema");
    accepted(decode_revoke(bv("{}")), "an absent member is not an unknown one");
}

void field_names_match_as_go_matches_them() {
    // Go resolves a field name exactly, then case-insensitively. Both spellings
    // are the same member, so a decoder that only did one of them would accept
    // or refuse a payload the other implementation did not.
    auto a = decode_revoke(bv(R"({"reason":"exact"})"));
    auto b = decode_revoke(bv(R"({"REASON":"folded"})"));
    check(a && a->reason == "exact", "an exact name matches");
    check(b && b->reason == "folded", "and so does a differently-cased one");

    // A duplicate takes its LAST value, silently, as Go does.
    auto dup = decode_revoke(bv(R"({"reason":"first","reason":"last"})"));
    check(dup && dup->reason == "last", "a duplicate member takes its last value");

    // Exact beats folded, whatever order they arrive in.
    auto both = decode_revoke(bv(R"({"REASON":"folded","reason":"exact"})"));
    check(both && both->reason == "exact", "an exact match wins over a folded one");
}

void null_is_the_zero_value() {
    auto p = decode_register(bv(R"({"digest":null,"type":null,"level":null,"size":null})"));
    check(p && p->digest == kEmptyId && p->type == 0 && p->level == 0 && p->size == 0,
          "null leaves a member at its zero value rather than failing");
    auto g = decode_grant(bv(R"({"grantee":null,"operations":3,"expiry":0})"));
    check(g && g->grantee == kEmptyAccount && g->operations == 3,
          "including an account");
    auto adv = decode_advance(bv(R"({"epoch":1,"committee":null,"threshold":1,"publicKey":null})"));
    check(adv && adv->committee.empty() && adv->public_key.empty(),
          "and a slice, which comes back nil");
}

void numbers_are_read_as_go_reads_them() {
    // Go hands the LITERAL to strconv for an integer destination, so a number
    // with a fraction or an exponent is a type error even when its value is a
    // whole number.
    refused(decode_register(bv(R"({"digest":[1],"type":1,"level":0,"size":1.0})")),
            Err::InvalidPayload, "1.0 is not an integer");
    refused(decode_register(bv(R"({"digest":[1],"type":1,"level":0,"size":1e2})")),
            Err::InvalidPayload, "nor is 1e2");
    refused(decode_register(bv(R"({"digest":[1],"type":1,"level":0,"size":-1})")),
            Err::InvalidPayload, "a negative into an unsigned field");
    refused(decode_register(bv(R"({"digest":[1],"type":256,"level":0,"size":1})")),
            Err::InvalidPayload, "a byte that does not fit in a byte");
    refused(decode_register(bv(R"({"digest":[1],"type":1,"level":0,"size":4294967296})")),
            Err::InvalidPayload, "a uint32 that does not fit in a uint32");
    refused(decode_register(bv(R"({"digest":[1],"type":1,"level":0,"size":"1"})")),
            Err::InvalidPayload, "a string where a number belongs");

    accepted(decode_register(bv(R"({"digest":[1],"type":255,"level":-9223372036854775808,"size":4294967295})")),
             "the widest values each field can hold");
    auto p = decode_register(bv(R"({"digest":[1],"type":255,"level":-5,"size":4294967295})"));
    check(p && p->level == -5 && p->size == 4294967295u, "and they read back");
}

void byte_arrays_follow_gos_array_rule() {
    // Go: extra JSON elements are DISCARDED, missing Go elements stay zero.
    // Both halves are deliberate, and a port that erred either way would accept
    // or refuse payloads the other does not.
    auto shortened = decode_fulfill(bv(R"({"result":[1,2,3]})"));
    check(shortened && shortened->result[0] == 1 && shortened->result[2] == 3 &&
              shortened->result[31] == 0,
          "a short array leaves the rest zero");

    std::string over = R"({"result":[)";
    for (int i = 0; i < 40; ++i) over += (i ? "," : "") + std::to_string(i % 256);
    over += "]}";
    auto extra = decode_fulfill(bv(over));
    check(extra && extra->result[31] == 31, "and a long one discards what does not fit");

    refused(decode_fulfill(bv(R"({"result":"deadbeef"})")), Err::InvalidPayload,
            "a hex string is not a Go byte array");
    refused(decode_fulfill(bv(R"({"result":[1,"2"]})")), Err::InvalidPayload,
            "nor is an array of strings");
    refused(decode_fulfill(bv(R"({"result":[300]})")), Err::InvalidPayload,
            "nor one whose elements are not bytes");
}

void slices_are_base64() {
    auto p = decode_advance(
        bv(R"({"epoch":1,"committee":null,"threshold":1,"publicKey":"Zm9vYmFy"})"));
    check(p && std::string(p->public_key.begin(), p->public_key.end()) == "foobar",
          "a Go []byte reads from base64");
    refused(decode_advance(bv(R"({"epoch":1,"committee":null,"threshold":1,"publicKey":"!!!!"})")),
            Err::InvalidPayload, "and refuses what is not base64");
    refused(decode_advance(bv(R"({"epoch":1,"committee":null,"threshold":1,"publicKey":[1,2]})")),
            Err::InvalidPayload, "or an array where a base64 string belongs");
}

void accounts_are_cb58() {
    Account a{};
    a[0] = 0xab;
    a[19] = 0xcd;
    std::string doc = R"({"grantee":")" + account_string(a) + R"(","operations":1,"expiry":0})";
    auto p = decode_grant(bv(doc));
    check(p && p->grantee == a, "an account reads from its cb58 word");

    refused(decode_grant(bv(R"({"grantee":"not-cb58!","operations":1,"expiry":0})")),
            Err::InvalidPayload, "a word that is not cb58 is refused");
    refused(decode_grant(bv(R"({"grantee":[1,2],"operations":1,"expiry":0})")),
            Err::InvalidPayload, "so is an array");
    // Go's ShortID.UnmarshalText takes the empty word as a no-op.
    accepted(decode_grant(bv(R"({"grantee":"","operations":1,"expiry":0})")),
             "and the empty word leaves it zero, as Go does");
}

void writing_matches_marshal() {
    json::Writer w;
    w.begin_object();
    w.key("a");
    w.u64(1);
    w.key("b");
    w.begin_array();
    w.u64(1);
    w.string("two");
    w.begin_object();
    w.key("c");
    w.null();
    w.end_object();
    w.end_array();
    w.key("d");
    w.byte_array(view(Bytes{1, 2, 255}));
    w.end_object();
    check_eq(w.str(), R"({"a":1,"b":[1,"two",{"c":null}],"d":[1,2,255]})",
             "the writer separates exactly where JSON does");

    // Marshal escapes <, > and & so a document is safe to embed in HTML. That
    // is a byte difference, so the port has to do it too.
    check_eq(json::escape_string("<a href=\"x\">&</a>"),
             "\"\\u003ca href=\\\"x\\\"\\u003e\\u0026\\u003c/a\\u003e\"",
             "HTML escaping, as Marshal does it");
    check_eq(json::escape_string("\n\t\\"), "\"\\n\\t\\\\\"", "the short escapes");
    check_eq(json::escape_string("\x01"), "\"\\u0001\"", "and a control character");
    check_eq(json::escape_string("a\xe2\x80\xa8" "b"), "\"a\\u2028b\"", "U+2028 is escaped too");
}

void a_payload_survives_its_own_round_trip() {
    // Everything the writer emits, the reader takes back — for every operation,
    // which is what makes a client and a node agree on the subject a payload
    // derives.
    RegisterPayload r;
    r.digest = sha256(view(std::string_view("round")));
    r.type = 7;
    r.level = -3;
    r.size = 4096;
    auto rb = decode_register(bv(marshal(r)));
    check(rb && rb->digest == r.digest && rb->type == r.type && rb->level == r.level &&
              rb->size == r.size,
          "register round-trips");

    GrantPayload g;
    g.grantee = address_of(view(std::string_view("someone")));
    g.operations = kPermitOpMask;
    g.expiry = 1234567;
    auto gb = decode_grant(bv(marshal(g)));
    check(gb && gb->grantee == g.grantee && gb->operations == g.operations &&
              gb->expiry == g.expiry,
          "grant round-trips");

    RevokePayload rv;
    rv.reason = "a reason with \"quotes\", a <tag> and a newline\n";
    auto rvb = decode_revoke(bv(marshal(rv)));
    check(rvb && rvb->reason == rv.reason, "revoke round-trips, escapes and all");

    RequestPayload q;
    q.permit_id = sha256(view(std::string_view("permit")));
    q.callback[3] = 9;
    q.selector = {4, 3, 2, 1};
    q.expiry = 42;
    auto qb = decode_request(bv(marshal(q)));
    check(qb && qb->permit_id == q.permit_id && qb->callback == q.callback &&
              qb->selector == q.selector && qb->expiry == q.expiry,
          "request round-trips");

    FulfillPayload f;
    f.result = sha256(view(std::string_view("result")));
    auto fb = decode_fulfill(bv(marshal(f)));
    check(fb && fb->result == f.result, "fulfill round-trips");

    AdvancePayload a;
    a.epoch = 9;
    a.threshold = 2;
    a.public_key = Bytes{0, 1, 2, 250};
    a.public_key_nil = false;
    CommitteeMember m;
    m.node_id[0] = 3;
    m.public_key = Bytes(1952, 0x7f);
    m.public_key_nil = false;
    m.weight = 11;
    m.index = 0;
    a.committee.push_back(m);
    a.committee_nil = false;
    auto ab = decode_advance(bv(marshal(a)));
    check(ab && ab->epoch == a.epoch && ab->threshold == a.threshold &&
              ab->public_key == a.public_key && ab->committee == a.committee,
          "advance round-trips, committee and all");
}

void trailing_content_in_a_payload_is_refused() {
    // A well-formed payload with a second document after it is refused, which
    // is what stops a body riding along behind a sound-looking value.
    std::string doc = marshal(RevokePayload{"ok"}) + R"({"body":"more"})";
    refused(decode_revoke(bv(doc)), Err::InvalidPayload, "trailing content after a payload");
    std::string spaced = marshal(RevokePayload{"ok"}) + "   ";
    accepted(decode_revoke(bv(spaced)), "but trailing whitespace is not content");
}

}  // namespace

int main() {
    std::printf("fhevm — JSON as Go's encoding/json does it\n\n");
    grammar();
    unknown_fields_are_refused();
    field_names_match_as_go_matches_them();
    null_is_the_zero_value();
    numbers_are_read_as_go_reads_them();
    byte_arrays_follow_gos_array_rule();
    slices_are_base64();
    accounts_are_cb58();
    writing_matches_marshal();
    a_payload_survives_its_own_round_trip();
    trailing_content_in_a_payload_is_refused();
    return report("json");
}
