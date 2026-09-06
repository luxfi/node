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

// utf8_fix copies a run of raw string bytes, replacing every ill-formed
// sequence with U+FFFD — the substitution Go's decoder makes, so a payload
// carrying invalid UTF-8 decodes to the SAME string on both sides.
void append_utf8_checked(std::string& out, std::string_view s) {
    std::size_t i = 0;
    while (i < s.size()) {
        unsigned char c = static_cast<unsigned char>(s[i]);
        std::size_t need = 0;
        std::uint32_t r = 0;
        if (c < 0x80) {
            out.push_back(char(c));
            ++i;
            continue;
        } else if ((c & 0xE0) == 0xC0) {
            need = 1;
            r = c & 0x1F;
        } else if ((c & 0xF0) == 0xE0) {
            need = 2;
            r = c & 0x0F;
        } else if ((c & 0xF8) == 0xF0) {
            need = 3;
            r = c & 0x07;
        } else {
            append_rune(out, 0xFFFD);
            ++i;
            continue;
        }
        if (i + need >= s.size()) {
            append_rune(out, 0xFFFD);
            ++i;
            continue;
        }
        bool ok = true;
        for (std::size_t k = 1; k <= need; ++k) {
            unsigned char cc = static_cast<unsigned char>(s[i + k]);
            if ((cc & 0xC0) != 0x80) {
                ok = false;
                break;
            }
            r = (r << 6) | (cc & 0x3F);
        }
        // Overlong forms, surrogates and out-of-range are all ill-formed.
        if (ok) {
            if ((need == 1 && r < 0x80) || (need == 2 && r < 0x800) ||
                (need == 3 && r < 0x10000) || r > 0x10FFFF || (r >= 0xD800 && r <= 0xDFFF)) {
                ok = false;
            }
        }
        if (!ok) {
            append_rune(out, 0xFFFD);
            ++i;
            continue;
        }
        out.append(s.substr(i, need + 1));
        i += need + 1;
    }
}

bool parse_value(std::string_view in, std::size_t* i, Value* out, std::string* err);

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

bool parse_value(std::string_view in, std::size_t* i, Value* out, std::string* err) {
    skip_space(in, i);
    if (*i >= in.size()) return fail(err, "unexpected end of JSON input");
    char c = in[*i];
    if (c == '{') {
        ++*i;
        out->kind = Kind::Object;
        skip_space(in, i);
        if (*i < in.size() && in[*i] == '}') {
            ++*i;
            return true;
        }
        while (true) {
            skip_space(in, i);
            std::string k;
            if (!parse_string(in, i, &k, err)) return false;
            skip_space(in, i);
            if (*i >= in.size() || in[*i] != ':') return fail(err, "expected ':'");
            ++*i;
            Value v;
            if (!parse_value(in, i, &v, err)) return false;
            out->members.emplace_back(std::move(k), std::move(v));
            skip_space(in, i);
            if (*i < in.size() && in[*i] == ',') {
                ++*i;
                continue;
            }
            if (*i < in.size() && in[*i] == '}') {
                ++*i;
                return true;
            }
            return fail(err, "expected ',' or '}'");
        }
    }
    if (c == '[') {
        ++*i;
        out->kind = Kind::Array;
        skip_space(in, i);
        if (*i < in.size() && in[*i] == ']') {
            ++*i;
            return true;
        }
        while (true) {
            Value v;
            if (!parse_value(in, i, &v, err)) return false;
            out->array.push_back(std::move(v));
            skip_space(in, i);
            if (*i < in.size() && in[*i] == ',') {
                ++*i;
                continue;
            }
            if (*i < in.size() && in[*i] == ']') {
                ++*i;
                return true;
            }
            return fail(err, "expected ',' or ']'");
        }
    }
    if (c == '"') {
        out->kind = Kind::String;
        return parse_string(in, i, &out->str, err);
    }
    if (in.compare(*i, 4, "true") == 0) {
        *i += 4;
        out->kind = Kind::Bool;
        out->boolean = true;
        return true;
    }
    if (in.compare(*i, 5, "false") == 0) {
        *i += 5;
        out->kind = Kind::Bool;
        out->boolean = false;
        return true;
    }
    if (in.compare(*i, 4, "null") == 0) {
        *i += 4;
        out->kind = Kind::Null;
        return true;
    }
    out->kind = Kind::Number;
    return parse_number(in, i, &out->number, err);
}

bool equal_fold(std::string_view a, std::string_view b) {
    if (a.size() != b.size()) return false;
    for (std::size_t i = 0; i < a.size(); ++i) {
        char x = a[i], y = b[i];
        if (x >= 'A' && x <= 'Z') x = char(x - 'A' + 'a');
        if (y >= 'A' && y <= 'Z') y = char(y - 'A' + 'a');
        if (x != y) return false;
    }
    return true;
}

// integer_literal reports whether the number is one strconv.ParseInt would take
// — no fraction, no exponent. Go hands the literal straight to strconv for an
// integer destination, so "1.0" is a type error there and here.
bool integer_literal(std::string_view s) {
    return s.find('.') == std::string_view::npos && s.find('e') == std::string_view::npos &&
           s.find('E') == std::string_view::npos;
}

}  // namespace

bool parse(std::string_view in, Value* out, std::size_t* consumed, std::string* err) {
    std::size_t i = 0;
    if (!parse_value(in, &i, out, err)) return false;
    *consumed = i;
    return true;
}

bool more(std::string_view in, std::size_t consumed) {
    std::size_t i = consumed;
    skip_space(in, &i);
    return i < in.size();
}

Reader::Reader(const Value& v, std::initializer_list<std::string_view> schema, std::string* err) {
    if (v.kind != Kind::Object) {
        if (err) *err = "expected a JSON object";
        return;
    }
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
    obj_ = &v;
    ok_ = true;
}

const Value* Reader::find(std::string_view name) const {
    if (!obj_) return nullptr;
    const Value* exact = nullptr;
    const Value* folded = nullptr;
    for (const auto& [k, v] : obj_->members) {
        if (k == name) exact = &v;
        else if (equal_fold(k, name)) folded = &v;
    }
    return exact ? exact : folded;
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
    // Go's array rule: extra JSON elements are discarded, missing Go elements
    // stay zero. Both halves are deliberate and neither is an error.
    for (std::size_t i = 0; i < v->array.size(); ++i) {
        std::uint64_t b = 0;
        if (!read_u64(&v->array[i], field, 255, &b, err)) return false;
        if (i < n) out[i] = std::uint8_t(b);
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
    // Go's ShortID.UnmarshalText takes the empty word as a no-op.
    if (v->str.empty()) return true;
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
    if (v->str.empty()) return true;
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
        out.push_back(char(c));
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
