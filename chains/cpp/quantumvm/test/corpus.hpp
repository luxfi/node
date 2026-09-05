// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// corpus.hpp — reading the shared differential corpus.
//
// The corpus (conformance/corpus/chain_differential.json) is the ONE set of
// bytes Go, Rust and C++ are all handed, so the evaluator that reads it must
// not need a library this chain does not otherwise have. What is here is a
// scanner for exactly the JSON the corpus is: objects, arrays, strings,
// numbers, booleans, null. Nothing about it is general — it is a reader for one
// file, kept beside the one thing that reads it.

#pragma once

#include <cctype>
#include <cstdint>
#include <fstream>
#include <map>
#include <memory>
#include <sstream>
#include <string>
#include <vector>

namespace qvmtest::corpus {

struct Value;
using Object = std::map<std::string, Value>;
using Array = std::vector<Value>;

struct Value {
    enum class Kind { Null, Bool, Number, String, Array, Object } kind = Kind::Null;
    bool boolean = false;
    double number = 0;
    std::string text;
    std::shared_ptr<Array> array;
    std::shared_ptr<Object> object;

    const Value* at(const std::string& key) const {
        if (kind != Kind::Object || !object) return nullptr;
        auto it = object->find(key);
        return it == object->end() ? nullptr : &it->second;
    }
    std::string str(const std::string& key) const {
        const Value* v = at(key);
        return (v && v->kind == Kind::String) ? v->text : std::string();
    }
    bool flag(const std::string& key) const {
        const Value* v = at(key);
        return v && v->kind == Kind::Bool && v->boolean;
    }
};

class Reader {
  public:
    explicit Reader(std::string src) : s_(std::move(src)) {}

    bool read(Value& out) {
        skip();
        if (!value(out)) return false;
        skip();
        return i_ == s_.size();
    }

  private:
    void skip() {
        while (i_ < s_.size() && std::isspace(static_cast<unsigned char>(s_[i_]))) ++i_;
    }
    bool literal(const char* lit) {
        const std::size_t n = std::char_traits<char>::length(lit);
        if (s_.compare(i_, n, lit) != 0) return false;
        i_ += n;
        return true;
    }

    bool value(Value& out) {
        if (i_ >= s_.size()) return false;
        switch (s_[i_]) {
            case '{': return object(out);
            case '[': return array(out);
            case '"': {
                out.kind = Value::Kind::String;
                return string(out.text);
            }
            case 't':
                if (!literal("true")) return false;
                out.kind = Value::Kind::Bool;
                out.boolean = true;
                return true;
            case 'f':
                if (!literal("false")) return false;
                out.kind = Value::Kind::Bool;
                out.boolean = false;
                return true;
            case 'n':
                if (!literal("null")) return false;
                out.kind = Value::Kind::Null;
                return true;
            default: return number(out);
        }
    }

    bool string(std::string& out) {
        if (i_ >= s_.size() || s_[i_] != '"') return false;
        ++i_;
        out.clear();
        while (i_ < s_.size() && s_[i_] != '"') {
            char c = s_[i_++];
            if (c != '\\') {
                out += c;
                continue;
            }
            if (i_ >= s_.size()) return false;
            const char esc = s_[i_++];
            switch (esc) {
                case 'n': out += '\n'; break;
                case 't': out += '\t'; break;
                case 'r': out += '\r'; break;
                case 'b': out += '\b'; break;
                case 'f': out += '\f'; break;
                case 'u': {
                    if (i_ + 4 > s_.size()) return false;
                    const int cp = std::stoi(s_.substr(i_, 4), nullptr, 16);
                    i_ += 4;
                    // The corpus is ASCII; anything above it is passed through
                    // as UTF-8 rather than silently dropped.
                    if (cp < 0x80) {
                        out += static_cast<char>(cp);
                    } else if (cp < 0x800) {
                        out += static_cast<char>(0xC0 | (cp >> 6));
                        out += static_cast<char>(0x80 | (cp & 0x3F));
                    } else {
                        out += static_cast<char>(0xE0 | (cp >> 12));
                        out += static_cast<char>(0x80 | ((cp >> 6) & 0x3F));
                        out += static_cast<char>(0x80 | (cp & 0x3F));
                    }
                    break;
                }
                default: out += esc; break;
            }
        }
        if (i_ >= s_.size()) return false;
        ++i_;  // closing quote
        return true;
    }

    bool number(Value& out) {
        const std::size_t start = i_;
        while (i_ < s_.size() && (std::isdigit(static_cast<unsigned char>(s_[i_])) || s_[i_] == '-' ||
                                  s_[i_] == '+' || s_[i_] == '.' || s_[i_] == 'e' || s_[i_] == 'E'))
            ++i_;
        if (i_ == start) return false;
        out.kind = Value::Kind::Number;
        out.number = std::stod(s_.substr(start, i_ - start));
        return true;
    }

    bool array(Value& out) {
        ++i_;  // '['
        out.kind = Value::Kind::Array;
        out.array = std::make_shared<Array>();
        skip();
        if (i_ < s_.size() && s_[i_] == ']') {
            ++i_;
            return true;
        }
        while (true) {
            Value v;
            skip();
            if (!value(v)) return false;
            out.array->push_back(std::move(v));
            skip();
            if (i_ < s_.size() && s_[i_] == ',') {
                ++i_;
                continue;
            }
            if (i_ < s_.size() && s_[i_] == ']') {
                ++i_;
                return true;
            }
            return false;
        }
    }

    bool object(Value& out) {
        ++i_;  // '{'
        out.kind = Value::Kind::Object;
        out.object = std::make_shared<Object>();
        skip();
        if (i_ < s_.size() && s_[i_] == '}') {
            ++i_;
            return true;
        }
        while (true) {
            skip();
            std::string key;
            if (!string(key)) return false;
            skip();
            if (i_ >= s_.size() || s_[i_] != ':') return false;
            ++i_;
            skip();
            Value v;
            if (!value(v)) return false;
            out.object->emplace(std::move(key), std::move(v));
            skip();
            if (i_ < s_.size() && s_[i_] == ',') {
                ++i_;
                continue;
            }
            if (i_ < s_.size() && s_[i_] == '}') {
                ++i_;
                return true;
            }
            return false;
        }
    }

    std::string s_;
    std::size_t i_ = 0;
};

inline bool load(const std::string& path, Value& out) {
    std::ifstream in(path, std::ios::binary);
    if (!in) return false;
    std::ostringstream buf;
    buf << in.rdbuf();
    Reader r(buf.str());
    return r.read(out);
}

inline std::vector<std::uint8_t> unhex(const std::string& hex) {
    std::vector<std::uint8_t> out;
    out.reserve(hex.size() / 2);
    auto nibble = [](char c) -> int {
        if (c >= '0' && c <= '9') return c - '0';
        if (c >= 'a' && c <= 'f') return c - 'a' + 10;
        if (c >= 'A' && c <= 'F') return c - 'A' + 10;
        return -1;
    };
    for (std::size_t i = 0; i + 1 < hex.size(); i += 2) {
        const int hi = nibble(hex[i]), lo = nibble(hex[i + 1]);
        if (hi < 0 || lo < 0) return {};
        out.push_back(static_cast<std::uint8_t>((hi << 4) | lo));
    }
    return out;
}

// Where the corpus is, tried in the order a caller is likely to be standing in.
inline std::string find_corpus(const char* argv0_dir_hint) {
    const char* env = std::getenv("NODE2_CORPUS_PATH");
    if (env != nullptr) return env;
    const std::vector<std::string> candidates = {
        "conformance/corpus/chain_differential.json",
        "../conformance/corpus/chain_differential.json",
        "../../conformance/corpus/chain_differential.json",
        "../../../conformance/corpus/chain_differential.json",
        "../../../../conformance/corpus/chain_differential.json",
        "../../../../../conformance/corpus/chain_differential.json",
        std::string(argv0_dir_hint ? argv0_dir_hint : "") + "/../../../../conformance/corpus/chain_differential.json",
    };
    for (const auto& c : candidates) {
        std::ifstream in(c);
        if (in) return c;
    }
    return {};
}

}  // namespace qvmtest::corpus
