// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/dexvm/json.hpp"

#include <cctype>
#include <cstdlib>

namespace lux::dexvm::json {
namespace {

struct Parser {
    std::string_view s;
    std::size_t i = 0;

    void skip_space() {
        while (i < s.size() && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r')) ++i;
    }
    bool eof() const { return i >= s.size(); }
    char peek() const { return s[i]; }

    Result<Value> value(int depth) {
        if (depth > 64) return fail("json: nesting too deep");
        skip_space();
        if (eof()) return fail("json: unexpected end of input");
        switch (peek()) {
            case '{': return object(depth);
            case '[': return array(depth);
            case '"': {
                auto str = string_lit();
                if (!str) return std::unexpected(str.error());
                return Value::string(*str);
            }
            case 't':
                if (s.substr(i, 4) == "true") { i += 4; return Value::boolean(true); }
                return fail("json: invalid literal");
            case 'f':
                if (s.substr(i, 5) == "false") { i += 5; return Value::boolean(false); }
                return fail("json: invalid literal");
            case 'n':
                if (s.substr(i, 4) == "null") { i += 4; return Value::null(); }
                return fail("json: invalid literal");
            default: return number();
        }
    }

    Result<Value> number() {
        const std::size_t start = i;
        if (!eof() && peek() == '-') ++i;
        bool any = false;
        while (!eof() && std::isdigit(static_cast<unsigned char>(peek()))) { ++i; any = true; }
        if (!eof() && peek() == '.') {
            ++i;
            while (!eof() && std::isdigit(static_cast<unsigned char>(peek()))) ++i;
        }
        if (!eof() && (peek() == 'e' || peek() == 'E')) {
            ++i;
            if (!eof() && (peek() == '+' || peek() == '-')) ++i;
            while (!eof() && std::isdigit(static_cast<unsigned char>(peek()))) ++i;
        }
        if (!any) return fail("json: invalid number");
        return Value::number(std::string(s.substr(start, i - start)));
    }

    Result<std::string> string_lit() {
        if (eof() || peek() != '"') return fail("json: expected a string");
        ++i;
        std::string out;
        while (true) {
            if (eof()) return fail("json: unterminated string");
            const char c = s[i++];
            if (c == '"') return out;
            if (c != '\\') {
                out.push_back(c);
                continue;
            }
            if (eof()) return fail("json: unterminated escape");
            const char e = s[i++];
            switch (e) {
                case '"':  out.push_back('"');  break;
                case '\\': out.push_back('\\'); break;
                case '/':  out.push_back('/');  break;
                case 'b':  out.push_back('\b'); break;
                case 'f':  out.push_back('\f'); break;
                case 'n':  out.push_back('\n'); break;
                case 'r':  out.push_back('\r'); break;
                case 't':  out.push_back('\t'); break;
                case 'u': {
                    auto hex4 = [&](std::uint32_t* cp) -> bool {
                        if (i + 4 > s.size()) return false;
                        std::uint32_t v = 0;
                        for (int k = 0; k < 4; ++k) {
                            const char h = s[i + std::size_t(k)];
                            int d;
                            if (h >= '0' && h <= '9') d = h - '0';
                            else if (h >= 'a' && h <= 'f') d = h - 'a' + 10;
                            else if (h >= 'A' && h <= 'F') d = h - 'A' + 10;
                            else return false;
                            v = v * 16 + std::uint32_t(d);
                        }
                        i += 4;
                        *cp = v;
                        return true;
                    };
                    std::uint32_t cp = 0;
                    if (!hex4(&cp)) return fail("json: invalid \\u escape");
                    // A surrogate pair is two escapes; a lone surrogate becomes
                    // U+FFFD, which is what Go's decoder does.
                    if (cp >= 0xD800 && cp <= 0xDBFF) {
                        std::uint32_t lo = 0;
                        if (i + 6 <= s.size() && s[i] == '\\' && s[i + 1] == 'u') {
                            const std::size_t save = i;
                            i += 2;
                            if (hex4(&lo) && lo >= 0xDC00 && lo <= 0xDFFF) {
                                cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
                            } else {
                                i = save;
                                cp = 0xFFFD;
                            }
                        } else {
                            cp = 0xFFFD;
                        }
                    } else if (cp >= 0xDC00 && cp <= 0xDFFF) {
                        cp = 0xFFFD;
                    }
                    // UTF-8 encode.
                    if (cp < 0x80) {
                        out.push_back(char(cp));
                    } else if (cp < 0x800) {
                        out.push_back(char(0xC0 | (cp >> 6)));
                        out.push_back(char(0x80 | (cp & 0x3F)));
                    } else if (cp < 0x10000) {
                        out.push_back(char(0xE0 | (cp >> 12)));
                        out.push_back(char(0x80 | ((cp >> 6) & 0x3F)));
                        out.push_back(char(0x80 | (cp & 0x3F)));
                    } else {
                        out.push_back(char(0xF0 | (cp >> 18)));
                        out.push_back(char(0x80 | ((cp >> 12) & 0x3F)));
                        out.push_back(char(0x80 | ((cp >> 6) & 0x3F)));
                        out.push_back(char(0x80 | (cp & 0x3F)));
                    }
                    break;
                }
                default:
                    return fail("json: invalid escape character");
            }
        }
    }

    Result<Value> object(int depth) {
        ++i;  // '{'
        Object o;
        skip_space();
        if (!eof() && peek() == '}') { ++i; return Value::object(std::move(o)); }
        while (true) {
            skip_space();
            auto k = string_lit();
            if (!k) return std::unexpected(k.error());
            skip_space();
            if (eof() || peek() != ':') return fail("json: expected ':' after object key");
            ++i;
            auto v = value(depth + 1);
            if (!v) return std::unexpected(v.error());
            // Go's decoder lets a later duplicate key win; so does this.
            o[*k] = std::move(*v);
            skip_space();
            if (eof()) return fail("json: unterminated object");
            if (peek() == ',') { ++i; continue; }
            if (peek() == '}') { ++i; return Value::object(std::move(o)); }
            return fail("json: expected ',' or '}' in object");
        }
    }

    Result<Value> array(int depth) {
        ++i;  // '['
        Array a;
        skip_space();
        if (!eof() && peek() == ']') { ++i; return Value::array(std::move(a)); }
        while (true) {
            auto v = value(depth + 1);
            if (!v) return std::unexpected(v.error());
            a.push_back(std::move(*v));
            skip_space();
            if (eof()) return fail("json: unterminated array");
            if (peek() == ',') { ++i; continue; }
            if (peek() == ']') { ++i; return Value::array(std::move(a)); }
            return fail("json: expected ',' or ']' in array");
        }
    }
};

}  // namespace

Value Value::null() { return Value(); }

Value Value::boolean(bool b) {
    Value v;
    v.kind_ = Kind::Bool;
    v.bool_ = b;
    return v;
}

Value Value::number(std::string text) {
    Value v;
    v.kind_ = Kind::Number;
    v.text_ = std::move(text);
    return v;
}

Value Value::string(std::string s) {
    Value v;
    v.kind_ = Kind::String;
    v.text_ = std::move(s);
    return v;
}

Value Value::array(Array a) {
    Value v;
    v.kind_ = Kind::Array;
    v.array_ = std::make_shared<Array>(std::move(a));
    return v;
}

Value Value::object(Object o) {
    Value v;
    v.kind_ = Kind::Object;
    v.object_ = std::make_shared<Object>(std::move(o));
    return v;
}

Result<bool> Value::as_bool() const {
    if (kind_ != Kind::Bool) return fail("json: value is not a bool");
    return bool_;
}

Result<std::string> Value::as_string() const {
    if (kind_ != Kind::String) return fail("json: value is not a string");
    return text_;
}

Result<std::uint64_t> Value::as_u64(std::uint64_t max) const {
    if (kind_ != Kind::Number) return fail("json: value is not a number");
    // Go refuses a float, a sign or an overflow where an unsigned integer is
    // declared, and so does this: the digits are the whole of a legal value.
    if (text_.empty()) return fail("json: empty number");
    for (char c : text_) {
        if (!std::isdigit(static_cast<unsigned char>(c)))
            return fail("json: cannot unmarshal " + text_ + " into an unsigned integer");
    }
    std::uint64_t v = 0;
    for (char c : text_) {
        const std::uint64_t d = std::uint64_t(c - '0');
        if (v > (0xFFFFFFFFFFFFFFFFull - d) / 10)
            return fail("json: number " + text_ + " overflows the declared field");
        v = v * 10 + d;
    }
    if (v > max) return fail("json: number " + text_ + " overflows the declared field");
    return v;
}

Result<const Array*> Value::as_array() const {
    if (kind_ != Kind::Array) return fail("json: value is not an array");
    return array_.get();
}

Result<const Object*> Value::as_object() const {
    if (kind_ != Kind::Object) return fail("json: value is not an object");
    return object_.get();
}

Result<Value> parse(std::string_view src) {
    Parser p{src, 0};
    auto v = p.value(0);
    if (!v) return v;
    p.skip_space();
    if (!p.eof()) return fail("json: trailing content after the top-level value");
    return v;
}

// ---- the writer -----------------------------------------------------------

std::string escape(std::string_view s) {
    static constexpr char kHex[] = "0123456789abcdef";
    std::string out = "\"";
    for (std::size_t i = 0; i < s.size(); ++i) {
        const unsigned char c = static_cast<unsigned char>(s[i]);
        switch (c) {
            case '"':  out += "\\\""; continue;
            case '\\': out += "\\\\"; continue;
            case '\n': out += "\\n";  continue;
            case '\r': out += "\\r";  continue;
            case '\t': out += "\\t";  continue;
            // Go's encoder escapes these three by default so a document is safe
            // to embed in HTML. It is a quirk, and reproducing it is the whole
            // point of a byte-identical writer.
            case '<':  out += "\\u003c"; continue;
            case '>':  out += "\\u003e"; continue;
            case '&':  out += "\\u0026"; continue;
            default: break;
        }
        if (c < 0x20) {
            out += "\\u00";
            out.push_back(kHex[c >> 4]);
            out.push_back(kHex[c & 0x0f]);
            continue;
        }
        // U+2028 / U+2029 are escaped too, for the same embedding reason.
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

void Writer::indent() {
    out_.push_back('\n');
    out_.append(std::size_t(depth_) * 2, ' ');
}

void Writer::comma_and_indent() {
    if (after_key_) {
        after_key_ = false;
        return;
    }
    if (!has_member_.empty()) {
        if (has_member_.back()) out_.push_back(',');
        has_member_.back() = true;
        indent();
    }
}

void Writer::begin_object() {
    comma_and_indent();
    out_.push_back('{');
    ++depth_;
    has_member_.push_back(false);
}

void Writer::end_object() {
    const bool any = has_member_.back();
    has_member_.pop_back();
    --depth_;
    if (any) indent();
    out_.push_back('}');
}

void Writer::begin_array() {
    comma_and_indent();
    out_.push_back('[');
    ++depth_;
    has_member_.push_back(false);
}

void Writer::end_array() {
    const bool any = has_member_.back();
    has_member_.pop_back();
    --depth_;
    if (any) indent();
    out_.push_back(']');
}

void Writer::key(std::string_view k) {
    comma_and_indent();
    out_ += escape(k);
    out_ += ": ";
    after_key_ = true;
}

void Writer::string(std::string_view s) {
    comma_and_indent();
    out_ += escape(s);
}

void Writer::number(std::uint64_t v) {
    comma_and_indent();
    out_ += std::to_string(v);
}

void Writer::boolean(bool b) {
    comma_and_indent();
    out_ += b ? "true" : "false";
}

void Writer::null() {
    comma_and_indent();
    out_ += "null";
}

}  // namespace lux::dexvm::json
