// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/fhevm/json.hpp"

#include <algorithm>
#include <cstdlib>

namespace lux::fhevm::json {
namespace {

bool is_space(char c) { return c == ' ' || c == '\t' || c == '\n' || c == '\r'; }

void skip_space(std::string_view in, std::size_t* i) {
    while (*i < in.size() && is_space(in[*i])) ++*i;
}

bool fail(std::string* err, std::string msg) {
    if (err) *err = std::move(msg);
    return false;
}

// append_rune writes one code point as UTF-8, the way Go's decoder does — an
// unpaired surrogate becomes U+FFFD rather than a malformed sequence.
void append_rune(std::string& out, std::uint32_t r) {
    if (r >= 0xD800 && r <= 0xDFFF) r = 0xFFFD;
    if (r < 0x80) {
        out.push_back(char(r));
    } else if (r < 0x800) {
        out.push_back(char(0xC0 | (r >> 6)));
        out.push_back(char(0x80 | (r & 0x3F)));
    } else if (r < 0x10000) {
        out.push_back(char(0xE0 | (r >> 12)));
        out.push_back(char(0x80 | ((r >> 6) & 0x3F)));
        out.push_back(char(0x80 | (r & 0x3F)));
    } else {
        out.push_back(char(0xF0 | (r >> 18)));
        out.push_back(char(0x80 | ((r >> 12) & 0x3F)));
        out.push_back(char(0x80 | ((r >> 6) & 0x3F)));
        out.push_back(char(0x80 | (r & 0x3F)));
    }
}

int hex4(std::string_view in, std::size_t i, std::uint32_t* out) {
    if (i + 4 > in.size()) return 0;
    std::uint32_t v = 0;
    for (int k = 0; k < 4; ++k) {
        char c = in[i + std::size_t(k)];
        v <<= 4;
        if (c >= '0' && c <= '9') {
            v |= std::uint32_t(c - '0');
        } else if (c >= 'a' && c <= 'f') {
            v |= std::uint32_t(c - 'a' + 10);
        } else if (c >= 'A' && c <= 'F') {
            v |= std::uint32_t(c - 'A' + 10);
        } else {
            return 0;
        }
    }
    *out = v;
    return 1;
}

// utf8_width is utf8.DecodeRune's size for a WELL-FORMED rune starting at i,
// and 0 for anything else — a stray continuation byte, a truncated sequence, an
// overlong form, a surrogate, or a value past U+10FFFF. Go treats every one of
// those as a single bad byte, which is why both callers advance by one on 0.
std::size_t utf8_width(std::string_view s, std::size_t i) {
    unsigned char c = static_cast<unsigned char>(s[i]);
    std::size_t need = 0;
    std::uint32_t r = 0;
    if (c < 0x80) return 1;
    if ((c & 0xE0) == 0xC0) {
        need = 1;
        r = c & 0x1Fu;
    } else if ((c & 0xF0) == 0xE0) {
        need = 2;
        r = c & 0x0Fu;
    } else if ((c & 0xF8) == 0xF0) {
        need = 3;
        r = c & 0x07u;
    } else {
        return 0;
    }
    if (i + need >= s.size()) return 0;
    for (std::size_t k = 1; k <= need; ++k) {
        unsigned char cc = static_cast<unsigned char>(s[i + k]);
        if ((cc & 0xC0) != 0x80) return 0;
        r = (r << 6) | (cc & 0x3Fu);
    }
    if ((need == 1 && r < 0x80) || (need == 2 && r < 0x800) || (need == 3 && r < 0x10000) ||
        r > 0x10FFFF || (r >= 0xD800 && r <= 0xDFFF)) {
        return 0;
    }
    return need + 1;
}

// append_utf8_checked copies a run of raw string bytes, replacing every
// ill-formed sequence with U+FFFD — the substitution Go's decoder makes, so a
// payload carrying invalid UTF-8 decodes to the SAME string on both sides.
void append_utf8_checked(std::string& out, std::string_view s) {
    std::size_t i = 0;
    while (i < s.size()) {
        std::size_t width = utf8_width(s, i);
        if (width == 0) {
            append_rune(out, 0xFFFD);
            ++i;
            continue;
        }
        out.append(s, i, width);
        i += width;
    }
}

// kMaxDepth is how many containers may be open at once. Go's scanner keeps the
// same count and refuses the one that would exceed it ("exceeded max depth",
// encoding/json maxNestingDepth = 10000), so the same document is refused on
// both sides. Without it this parser recurses once per '[' and a payload of
// nothing but brackets — well inside the payload bound — walks off the stack,
// which is not a refusal, it is the node dying to an unauthenticated message.
constexpr int kMaxDepth = 10000;

bool parse_string(std::string_view in, std::size_t* i, std::string* out, std::string* err) {
    if (*i >= in.size() || in[*i] != '"') return fail(err, "expected string");
    ++*i;
    std::string res;
    std::size_t raw_start = *i;
    auto flush_raw = [&](std::size_t end) {
        if (end > raw_start) append_utf8_checked(res, in.substr(raw_start, end - raw_start));
    };
    while (true) {
        if (*i >= in.size()) return fail(err, "unterminated string");
        char c = in[*i];
        if (c == '"') {
            flush_raw(*i);
            ++*i;
            *out = std::move(res);
            return true;
        }
        if (static_cast<unsigned char>(c) < 0x20) return fail(err, "control character in string");
        if (c != '\\') {
            ++*i;
            continue;
        }
        flush_raw(*i);
        ++*i;
        if (*i >= in.size()) return fail(err, "unterminated escape");
        char e = in[*i];
        ++*i;
        switch (e) {
            case '"': res.push_back('"'); break;
            case '\\': res.push_back('\\'); break;
            case '/': res.push_back('/'); break;
            case 'b': res.push_back('\b'); break;
            case 'f': res.push_back('\f'); break;
            case 'n': res.push_back('\n'); break;
            case 'r': res.push_back('\r'); break;
            case 't': res.push_back('\t'); break;
            case 'u': {
                std::uint32_t r = 0;
                if (!hex4(in, *i, &r)) return fail(err, "bad \\u escape");
                *i += 4;
                if (r >= 0xD800 && r <= 0xDBFF && *i + 6 <= in.size() && in[*i] == '\\' &&
                    in[*i + 1] == 'u') {
                    std::uint32_t lo = 0;
                    if (hex4(in, *i + 2, &lo) && lo >= 0xDC00 && lo <= 0xDFFF) {
                        r = 0x10000 + ((r - 0xD800) << 10) + (lo - 0xDC00);
                        *i += 6;
                    }
                }
                append_rune(res, r);
                break;
            }
            default:
                return fail(err, "bad escape");
        }
        raw_start = *i;
    }
}

bool parse_number(std::string_view in, std::size_t* i, std::string* out, std::string* err) {
    std::size_t start = *i;
    if (*i < in.size() && in[*i] == '-') ++*i;
    if (*i >= in.size()) return fail(err, "bad number");
    if (in[*i] == '0') {
        ++*i;
    } else if (in[*i] >= '1' && in[*i] <= '9') {
        while (*i < in.size() && in[*i] >= '0' && in[*i] <= '9') ++*i;
    } else {
        return fail(err, "bad number");
    }
    if (*i < in.size() && in[*i] == '.') {
        ++*i;
        if (*i >= in.size() || in[*i] < '0' || in[*i] > '9') return fail(err, "bad fraction");
        while (*i < in.size() && in[*i] >= '0' && in[*i] <= '9') ++*i;
    }
    if (*i < in.size() && (in[*i] == 'e' || in[*i] == 'E')) {
        ++*i;
        if (*i < in.size() && (in[*i] == '+' || in[*i] == '-')) ++*i;
        if (*i >= in.size() || in[*i] < '0' || in[*i] > '9') return fail(err, "bad exponent");
        while (*i < in.size() && in[*i] >= '0' && in[*i] <= '9') ++*i;
    }
    *out = std::string(in.substr(start, *i - start));
    return true;
}

// parse_value reads one whole value, ITERATIVELY.
//
// It used to recurse once per open container, and a payload is 128 KiB the
// payer chooses, so the parser's stack depth was the payer's to pick. Capping
// the containers at Go's 10,000 was not enough: at that depth a Debug build
// with the sanitizers on still walked off the stack — the same input, refused
// on one build and fatal on another, which is a consensus property decided by
// the compiler's frame size. A parser whose safety depends on how fat its
// frames are is not safe, so the containers now live on the heap in `open` and
// the C++ stack depth is constant.
//
// The cap stays, and it stays at Go's number, because it is not a stack
// argument: it is which documents the two implementations agree to refuse.
bool parse_value(std::string_view in, std::size_t* i, Value* out, std::string* err) {
    // One entry per container still being filled, outermost first. `v` is the
    // container and `object` says which closer and which member shape it takes.
    struct Frame {
        Value* v;
        bool object;
    };
    std::vector<Frame> open;
    Value* cur = out;  // where the value being read is written

    // A slot is appended to the container on top, and `cur` points into it.
    // Only the TOP container is ever appended to, so the Value* an outer frame
    // holds is never moved by a reallocation below it.
    auto member_slot = [&](Frame& f) -> bool {
        skip_space(in, i);
        std::string k;
        if (!parse_string(in, i, &k, err)) return false;
        skip_space(in, i);
        if (*i >= in.size() || in[*i] != ':') return fail(err, "expected ':'");
        ++*i;
        f.v->members.emplace_back(std::move(k), Value{});
        cur = &f.v->members.back().second;
        return true;
    };
    auto element_slot = [&](Frame& f) {
        f.v->array.emplace_back();
        cur = &f.v->array.back();
    };

    for (;;) {
        skip_space(in, i);
        if (*i >= in.size()) return fail(err, "unexpected end of JSON input");
        char c = in[*i];

        if (c == '{' || c == '[') {
            // Go's scanner pushes a parse state per open container and refuses
            // the push that would exceed maxNestingDepth, so the count that
            // matters is how many are open — not how deep the reader has gone.
            if (open.size() >= std::size_t(kMaxDepth)) return fail(err, "exceeded max depth");
            const bool object = (c == '{');
            ++*i;
            cur->kind = object ? Kind::Object : Kind::Array;
            open.push_back(Frame{cur, object});
            skip_space(in, i);
            if (*i < in.size() && in[*i] == (object ? '}' : ']')) {
                ++*i;
                open.pop_back();
                // An empty container is a finished value: fall through.
            } else {
                Frame& f = open.back();
                if (object) {
                    if (!member_slot(f)) return false;
                } else {
                    element_slot(f);
                }
                continue;  // read the value in the slot just made
            }
        } else if (c == '"') {
            cur->kind = Kind::String;
            if (!parse_string(in, i, &cur->str, err)) return false;
        } else if (in.compare(*i, 4, "true") == 0) {
            *i += 4;
            cur->kind = Kind::Bool;
            cur->boolean = true;
        } else if (in.compare(*i, 5, "false") == 0) {
            *i += 5;
            cur->kind = Kind::Bool;
            cur->boolean = false;
        } else if (in.compare(*i, 4, "null") == 0) {
            *i += 4;
            cur->kind = Kind::Null;
        } else {
            cur->kind = Kind::Number;
            if (!parse_number(in, i, &cur->number, err)) return false;
        }

        // A value has just been finished. Either the container on top wants
        // another one, or it ends here — and a container that ends is itself a
        // finished value, so this repeats outwards.
        for (;;) {
            if (open.empty()) return true;  // that value was the whole document
            skip_space(in, i);
            const bool object = open.back().object;
            if (*i < in.size() && in[*i] == ',') {
                ++*i;
                Frame& f = open.back();
                if (object) {
                    if (!member_slot(f)) return false;
                } else {
                    element_slot(f);
                }
                break;  // read the value in the new slot
            }
            if (*i < in.size() && in[*i] == (object ? '}' : ']')) {
                ++*i;
                open.pop_back();
                continue;  // the container is now the finished value
            }
            return fail(err, object ? "expected ',' or '}'" : "expected ',' or ']'");
        }
    }
}

// fold_name is Go's foldName: a member name reduced to the form in which
// encoding/json compares it, so `{"DIGEST":…}` names the same field as
// `{"digest":…}`.
//
// Go folds by unicode.SimpleFold, not by ASCII case. That matters here for
// EXACTLY two runes, and only because every schema name on this chain is ASCII:
// U+017F (ſ) folds onto s, and U+212A (K, KELVIN SIGN) onto k. Nothing else
// outside ASCII folds onto an ASCII letter, so a key carrying anything else can
// never name an ASCII field on either side and is refused by both. Missing
// those two made `{"Key":…}` a member Go assigns and this reader calls
// unknown — one side taking the transaction and the other refusing it.
std::string fold_name(std::string_view s) {
    std::string out;
    out.reserve(s.size());
    std::size_t i = 0;
    while (i < s.size()) {
        unsigned char c = static_cast<unsigned char>(s[i]);
        if (c < 0x80) {
            if (c >= 'a' && c <= 'z') c = static_cast<unsigned char>(c - 'a' + 'A');
            out.push_back(char(c));
            ++i;
            continue;
        }
        // ſ = C5 BF, K = E2 84 AA.
        if (c == 0xC5 && i + 1 < s.size() && static_cast<unsigned char>(s[i + 1]) == 0xBF) {
            out.push_back('S');
            i += 2;
            continue;
        }
        if (c == 0xE2 && i + 2 < s.size() && static_cast<unsigned char>(s[i + 1]) == 0x84 &&
            static_cast<unsigned char>(s[i + 2]) == 0xAA) {
            out.push_back('K');
            i += 3;
            continue;
        }
        out.push_back(char(c));
        ++i;
    }
    return out;
}

bool equal_fold(std::string_view a, std::string_view b) { return fold_name(a) == fold_name(b); }

// integer_literal reports whether the number is one strconv.ParseInt would take
// — no fraction, no exponent. Go hands the literal straight to strconv for an
// integer destination, so "1.0" is a type error there and here.
bool integer_literal(std::string_view s) {
    return s.find('.') == std::string_view::npos && s.find('e') == std::string_view::npos &&
           s.find('E') == std::string_view::npos;
}

}  // namespace

// ~Value drains the tree into a flat worklist rather than letting one
// destructor call the next. Every node is emptied BEFORE it is destroyed, so
// the destructor that runs when it goes out of scope returns at the guard.
Value::~Value() {
    if (array.empty() && members.empty()) return;
    std::vector<Value> pending;
    auto take = [&pending](Value& node) {
        for (auto& child : node.array) pending.push_back(std::move(child));
        node.array.clear();
        for (auto& [name, child] : node.members) pending.push_back(std::move(child));
        node.members.clear();
    };
    take(*this);
    while (!pending.empty()) {
        Value node = std::move(pending.back());
        pending.pop_back();
        take(node);
    }
}

bool parse(std::string_view in, Value* out, std::size_t* consumed, std::string* err) {
    std::size_t i = 0;
    if (!parse_value(in, &i, out, err)) {
        // A refused document may still have built most of a tree, and a caller
        // must not be able to read half of one. Assignment drops it, which is
        // flat because ~Value is.
        *out = Value{};
        return false;
    }
    *consumed = i;
    return true;
}

// more is Go's Decoder.More, and it is deliberately not "are there bytes left".
// Go peeks at the next non-space byte and answers false for ']' and '}' as well
// as for end of input, because More asks whether another ELEMENT follows in the
// container being read. So `{"reason":"x"}}` carries no second value in Go and
// its trailing brace is not a refusal. Reading it as one refused a payload the
// reference takes, which is a fork in the direction that halts a chain rather
// than the one that lets bytes through.
bool more(std::string_view in, std::size_t consumed) {
    std::size_t i = consumed;
    skip_space(in, &i);
    if (i >= in.size()) return false;
    return in[i] != ']' && in[i] != '}';
}

// trailing is json.Unmarshal's rule, which is the stricter of the two: it scans
// the WHOLE document and refuses any non-space byte after the value. A record
// row or a genesis with a stray '}' is an error there, and reading records
// under Decoder.More would have taken one Go refuses.
bool trailing(std::string_view in, std::size_t consumed) {
    std::size_t i = consumed;
    skip_space(in, &i);
    return i < in.size();
}

Reader::Reader(const Value& v, std::initializer_list<std::string_view> schema, std::string* err,
               Unknown unknown) {
    // Go's decoder takes a literal `null` for a struct as a no-op: every field
    // keeps its zero value and there is no error. An object with no members is
    // exactly that, so null is read as one rather than as the wrong kind.
    if (v.null()) {
        ok_ = true;
        return;
    }
    if (v.kind != Kind::Object) {
        if (err) *err = "expected a JSON object";
        return;
    }
    if (unknown == Unknown::Refuse) {
        for (const auto& [k, _] : v.members) {
            bool known = false;
            for (std::string_view f : schema) {
                if (k == f || equal_fold(k, f)) {
                    known = true;
                    break;
                }
            }
            if (!known) {
                if (err) *err = "unknown field \"" + k + "\"";
                return;
            }
        }
    }
    obj_ = &v;
    ok_ = true;
}

// find returns the member Go's decoder would have written LAST into this field.
// Go resolves each key on its own — exact name, else folded — and then decodes
// it, so with two keys naming one field the later key in the document wins
// whichever way each of them matched. Preferring the exact match over a later
// folded one made `{"digest":A,"DIGEST":B}` decode to A here and B there.
const Value* Reader::find(std::string_view name) const {
    if (!obj_) return nullptr;
    const Value* last = nullptr;
    for (const auto& [k, v] : obj_->members) {
        if (k == name || equal_fold(k, name)) last = &v;
    }
    return last;
}

bool read_u64(const Value* v, std::string_view field, std::uint64_t max, std::uint64_t* out,
              std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != Kind::Number) {
        return fail(err, "cannot unmarshal into " + std::string(field));
    }
    if (!integer_literal(v->number)) {
        return fail(err, "cannot unmarshal number " + v->number + " into " + std::string(field));
    }
    if (!v->number.empty() && v->number[0] == '-') {
        return fail(err, "cannot unmarshal number " + v->number + " into " + std::string(field));
    }
    std::uint64_t acc = 0;
    for (char c : v->number) {
        std::uint64_t d = std::uint64_t(c - '0');
        if (acc > (~std::uint64_t(0) - d) / 10) {
            return fail(err, "number " + v->number + " overflows " + std::string(field));
        }
        acc = acc * 10 + d;
    }
    if (acc > max) {
        return fail(err, "number " + v->number + " overflows " + std::string(field));
    }
    *out = acc;
    return true;
}

bool read_i64(const Value* v, std::string_view field, std::int64_t* out, std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != Kind::Number) {
        return fail(err, "cannot unmarshal into " + std::string(field));
    }
    if (!integer_literal(v->number)) {
        return fail(err, "cannot unmarshal number " + v->number + " into " + std::string(field));
    }
    bool neg = !v->number.empty() && v->number[0] == '-';
    std::string_view digits = v->number;
    if (neg) digits.remove_prefix(1);
    std::uint64_t acc = 0;
    const std::uint64_t limit = neg ? std::uint64_t(1) << 63 : (std::uint64_t(1) << 63) - 1;
    for (char c : digits) {
        std::uint64_t d = std::uint64_t(c - '0');
        if (acc > (limit - d) / 10) {
            return fail(err, "number " + v->number + " overflows " + std::string(field));
        }
        acc = acc * 10 + d;
    }
    *out = neg ? -std::int64_t(acc - 1) - 1 : std::int64_t(acc);
    return true;
}

bool read_string(const Value* v, std::string_view field, std::string* out, std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != Kind::String) {
        return fail(err, "cannot unmarshal into " + std::string(field));
    }
    *out = v->str;
    return true;
}

bool read_byte_array(const Value* v, std::string_view field, std::uint8_t* out, std::size_t n,
                     std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != Kind::Array) {
        return fail(err, "cannot unmarshal into " + std::string(field));
    }
    // Go's array rule: extra JSON elements are DISCARDED — decoded into an
    // invalid destination, which skips them without ever asking what type they
    // are — and missing Go elements stay zero. Type-checking the discarded tail
    // refused `[…32 numbers…,"x"]`, which the reference takes.
    const std::size_t take = std::min(n, v->array.size());
    for (std::size_t i = 0; i < take; ++i) {
        std::uint64_t b = 0;
        if (!read_u64(&v->array[i], field, 255, &b, err)) return false;
        out[i] = std::uint8_t(b);
    }
    return true;
}

bool read_bytes(const Value* v, std::string_view field, Bytes* out, std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != Kind::String) {
        return fail(err, "cannot unmarshal into " + std::string(field));
    }
    if (!base64_decode(v->str, out)) {
        return fail(err, "illegal base64 in " + std::string(field));
    }
    return true;
}

bool read_account(const Value* v, std::string_view field, Account* out, std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != Kind::String) {
        return fail(err, "cannot unmarshal into " + std::string(field));
    }
    // ShortID.UnmarshalText leaves the destination alone for two words before
    // it ever reaches cb58: the empty one, and the literal "null" INSIDE the
    // quotes — which is a different thing from the JSON literal null and is
    // spelled out separately in the reference. Both give the zero address.
    if (v->str.empty() || v->str == "null") return true;
    Bytes b;
    if (!cb58_decode(v->str, &b) || b.size() != out->size()) {
        return fail(err, "couldn't decode " + std::string(field) + " to bytes");
    }
    std::copy(b.begin(), b.end(), out->begin());
    return true;
}

bool read_node_id(const Value* v, std::string_view field, NodeId* out, std::string* err) {
    if (v == nullptr || v->null()) return true;
    if (v->kind != Kind::String) {
        return fail(err, "cannot unmarshal into " + std::string(field));
    }
    // Same two words as an address: "" and a quoted "null" leave the zero node.
    // Anything else must carry the prefix and decode, so "NodeID-" alone is a
    // refusal.
    if (v->str.empty() || v->str == "null") return true;
    if (!node_id_from_string(v->str, out)) {
        return fail(err, "couldn't decode " + std::string(field) + " to a node id");
    }
    return true;
}

// ---- writing -----------------------------------------------------------------

std::string escape_string(std::string_view s) {
    std::string out = "\"";
    for (std::size_t i = 0; i < s.size(); ++i) {
        unsigned char c = static_cast<unsigned char>(s[i]);
        switch (c) {
            case '"': out += "\\\""; continue;
            case '\\': out += "\\\\"; continue;
            case '\n': out += "\\n"; continue;
            case '\r': out += "\\r"; continue;
            case '\t': out += "\\t"; continue;
            default: break;
        }
        // Marshal escapes <, > and & so a JSON document is safe to embed in
        // HTML; that is a byte difference, so the port has to do it too.
        if (c == '<' || c == '>' || c == '&') {
            static const char* hexd = "0123456789abcdef";
            out += "\\u00";
            out.push_back(hexd[c >> 4]);
            out.push_back(hexd[c & 0x0f]);
            continue;
        }
        if (c < 0x20) {
            static const char* hexd = "0123456789abcdef";
            out += "\\u00";
            out.push_back(hexd[c >> 4]);
            out.push_back(hexd[c & 0x0f]);
            continue;
        }
        // U+2028 / U+2029 are escaped too (e2 80 a8 / e2 80 a9).
        if (c == 0xE2 && i + 2 < s.size() && static_cast<unsigned char>(s[i + 1]) == 0x80 &&
            (static_cast<unsigned char>(s[i + 2]) == 0xA8 ||
             static_cast<unsigned char>(s[i + 2]) == 0xA9)) {
            out += (static_cast<unsigned char>(s[i + 2]) == 0xA8) ? "\\u2028" : "\\u2029";
            i += 2;
            continue;
        }
        // Marshal decodes each rune and writes `�` for every byte that is
        // not the start of a well-formed one, so a Go string holding invalid
        // UTF-8 comes out as an escape and not as the bytes it went in as.
        // Copying them through wrote a record the reference never writes.
        std::size_t width = utf8_width(s, i);
        if (width == 0) {
            out += "\\ufffd";
            continue;
        }
        out.append(s, i, width);
        i += width - 1;
    }
    out.push_back('"');
    return out;
}

void Writer::separate() {
    if (stack_.empty()) return;
    if (stack_.back().second) {
        out_.push_back(',');
    } else {
        stack_.back().second = true;
    }
}

void Writer::begin_object() {
    if (!stack_.empty() && stack_.back().first == Ctx::Arr) separate();
    out_.push_back('{');
    stack_.emplace_back(Ctx::Obj, false);
}

void Writer::end_object() {
    out_.push_back('}');
    stack_.pop_back();
}

void Writer::begin_array() {
    if (!stack_.empty() && stack_.back().first == Ctx::Arr) separate();
    out_.push_back('[');
    stack_.emplace_back(Ctx::Arr, false);
}

void Writer::end_array() {
    out_.push_back(']');
    stack_.pop_back();
}

void Writer::key(std::string_view k) {
    separate();
    out_ += escape_string(k);
    out_.push_back(':');
}

// value_sep is what every scalar writer owes: inside an array the comma is the
// value's own, inside an object the key already paid it.
#define FHEVM_JSON_VALUE_SEP()                                        \
    do {                                                              \
        if (!stack_.empty() && stack_.back().first == Ctx::Arr) separate(); \
    } while (0)

void Writer::u64(std::uint64_t v) {
    FHEVM_JSON_VALUE_SEP();
    out_ += std::to_string(v);
}

void Writer::i64(std::int64_t v) {
    FHEVM_JSON_VALUE_SEP();
    out_ += std::to_string(v);
}

void Writer::string(std::string_view s) {
    FHEVM_JSON_VALUE_SEP();
    out_ += escape_string(s);
}

void Writer::boolean(bool b) {
    FHEVM_JSON_VALUE_SEP();
    out_ += b ? "true" : "false";
}

void Writer::null() {
    FHEVM_JSON_VALUE_SEP();
    out_ += "null";
}

void Writer::byte_array(ByteView b) {
    FHEVM_JSON_VALUE_SEP();
    out_.push_back('[');
    for (std::size_t i = 0; i < b.size(); ++i) {
        if (i > 0) out_.push_back(',');
        out_ += std::to_string(unsigned(b[i]));
    }
    out_.push_back(']');
}

void Writer::bytes(const Bytes& b, bool is_nil) {
    FHEVM_JSON_VALUE_SEP();
    if (is_nil) {
        out_ += "null";
        return;
    }
    out_ += escape_string(base64(view(b)));
}

void Writer::raw(std::string_view already_json) {
    FHEVM_JSON_VALUE_SEP();
    out_ += already_json;
}

#undef FHEVM_JSON_VALUE_SEP


}  // namespace lux::fhevm::json
