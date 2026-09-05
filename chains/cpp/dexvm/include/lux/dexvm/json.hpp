// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// json.hpp — the manifest's on-disk encoding, and only that.
//
// The manifest is the one artifact this chain reads from outside itself, and its
// format is Go's encoding/json because Go wrote it. So this is not a general
// JSON library: it is a reader that accepts exactly what Go's decoder accepts
// (including DisallowUnknownFields, which is why a typo'd field must be visible
// rather than silently dropped) and a writer that reproduces Go's
// MarshalIndent(v, "", "  ") BYTE FOR BYTE.
//
// Byte-for-byte matters because a manifest is pinned by the SHA-256 of its
// bytes. A writer that merely produced equivalent JSON would hash differently
// from the artifact CI approved, and the pin would reject a file that is in fact
// correct. So the writer carries Go's exact quirks: two-space indent, `<`, `>`
// and `&` escaped as < > &, map keys sorted, a nil slice written
// as null and an empty one as [].

#pragma once

#include "lux/dexvm/id.hpp"

#include <cstdint>
#include <map>
#include <memory>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

namespace lux::dexvm::json {

class Value;
using Object = std::map<std::string, Value>;
using Array = std::vector<Value>;

// Value is a parsed JSON document. Numbers keep their source text so an integer
// field can be converted strictly — Go refuses a float where a uint is declared,
// and a parser that had already gone through double could not tell.
class Value {
public:
    enum class Kind { Null, Bool, Number, String, Array, Object };

    Value() = default;
    static Value null();
    static Value boolean(bool b);
    static Value number(std::string text);
    static Value string(std::string s);
    static Value array(Array a);
    static Value object(Object o);

    Kind kind() const { return kind_; }
    bool is_null() const { return kind_ == Kind::Null; }

    // The typed readers. Each fails rather than coercing: a string where a
    // number is declared is a malformed manifest, not a value to convert.
    Result<bool> as_bool() const;
    Result<std::string> as_string() const;
    Result<std::uint64_t> as_u64(std::uint64_t max) const;
    Result<const Array*> as_array() const;
    Result<const Object*> as_object() const;

private:
    Kind kind_ = Kind::Null;
    bool bool_ = false;
    std::string text_;  // number source text, or string contents
    std::shared_ptr<Array> array_;
    std::shared_ptr<Object> object_;
};

// parse decodes a whole document. Trailing content after the top-level value is
// an error, as it is in Go's Decoder for a single Decode.
Result<Value> parse(std::string_view src);

// ---- the writer -----------------------------------------------------------

// Writer emits Go's MarshalIndent(v, "", "  ") shape. The caller drives it in
// field order, because Go writes a struct's fields in declaration order and a
// map's keys sorted — two different rules that only the caller can tell apart.
class Writer {
public:
    void begin_object();
    void end_object();
    void begin_array();
    void end_array();

    // key opens a member of the current object.
    void key(std::string_view k);

    void string(std::string_view s);
    void number(std::uint64_t v);
    void boolean(bool b);
    void null();

    const std::string& str() const { return out_; }

private:
    void comma_and_indent();
    void indent();

    std::string out_;
    int depth_ = 0;
    // Whether the container at the current depth has already had a member. Go
    // writes the separator before a member rather than after, and empty
    // containers collapse to {} / [], so this is what tells the two apart.
    std::vector<bool> has_member_;
    bool after_key_ = false;
};

// escape renders a string the way Go's encoding/json does, quotes included.
// Exposed because it is the half most likely to drift, and a test can state it.
std::string escape(std::string_view s);

}  // namespace lux::dexvm::json
