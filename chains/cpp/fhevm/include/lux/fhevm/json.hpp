// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// json.hpp — JSON as Go's encoding/json does it, because on this chain that is
// a consensus question and not a formatting one.
//
// A transaction's payload is an opaque byte string the chain keeps verbatim,
// and whether it DECODES decides whether the transaction is valid. So the two
// implementations have to accept and refuse exactly the same bytes:
//
//   - a member the schema does not describe is REFUSED, not ignored
//     (Go: Decoder.DisallowUnknownFields). A megabyte of ciphertext body in a
//     "body" member used to decode fine and come back out of the block store.
//   - a second value after the first is refused (Go: dec.More()).
//   - a field name matches EXACTLY, or failing that case-insensitively — the
//     rule Go's struct decoder uses, so `{"DIGEST":...}` is the same member
//     here as it is there.
//   - `null` for any member leaves it at its zero value and is not an error.
//   - a number decoded into an integer must BE an integer: Go hands the literal
//     to strconv, so "1.0" and "1e2" are refused where "100" is taken.
//   - a duplicate member takes its last value, silently, as Go does.
//
// And the writer matches Marshal, down to the parts that look like decoration:
// <, > and & are escaped, map keys come out sorted, a [N]byte writes as an
// array of numbers while a []byte writes as base64. Those are what the Go chain
// PERSISTS, so a record written any other way is a different database.

#pragma once

#include "lux/fhevm/id.hpp"

#include <cstdint>
#include <initializer_list>
#include <map>
#include <optional>
#include <string>
#include <string_view>
#include <utility>
#include <vector>

namespace lux::fhevm::json {

enum class Kind { Null, Bool, Number, String, Array, Object };

struct Value {
    Kind kind = Kind::Null;
    bool boolean = false;
    // The number's LITERAL text. Integer conversion reads this, so it can be
    // exactly as strict as Go's strconv is.
    std::string number;
    std::string str;
    std::vector<Value> array;
    std::vector<std::pair<std::string, Value>> members;

    bool null() const { return kind == Kind::Null; }
};

// parse reads ONE value and reports how many bytes it consumed (leading
// whitespace included). It is the whole grammar: anything malformed is refused
// with a reason.
bool parse(std::string_view in, Value* out, std::size_t* consumed, std::string* err);

// more reports whether another value begins after the first — Go's dec.More(),
// which is what makes trailing content a refusal rather than a shrug.
bool more(std::string_view in, std::size_t consumed);

// Reader is one JSON object being read as a struct: it holds the schema, so an
// unknown member is caught once here rather than at each field.
class Reader {
public:
    // ok is false when the value is not an object, or carries a member the
    // schema does not describe. err says which.
    Reader(const Value& v, std::initializer_list<std::string_view> schema, std::string* err);

    bool ok() const { return ok_; }
    // find returns the LAST member with this name — exact match preferred, then
    // case-insensitive, exactly as Go resolves a field.
    const Value* find(std::string_view name) const;

private:
    bool ok_ = false;
    const Value* obj_ = nullptr;
};

// ---- typed reads. Each returns false and sets err on a Go-visible mismatch --
//
// Every one treats null as "absent": Go's decoder leaves the destination alone.

bool read_u64(const Value* v, std::string_view field, std::uint64_t max, std::uint64_t* out,
              std::string* err);
bool read_i64(const Value* v, std::string_view field, std::int64_t* out, std::string* err);
bool read_string(const Value* v, std::string_view field, std::string* out, std::string* err);
// read_byte_array fills a fixed-width Go array from a JSON array of numbers.
// Go's rule, and it is deliberate: extra elements are DISCARDED and missing
// ones stay zero.
bool read_byte_array(const Value* v, std::string_view field, std::uint8_t* out, std::size_t n,
                     std::string* err);
// read_bytes reads a Go []byte: a base64 string, or null for nil.
bool read_bytes(const Value* v, std::string_view field, Bytes* out, std::string* err);
// read_account / read_node_id read the cb58 words Go's ids types unmarshal.
bool read_account(const Value* v, std::string_view field, Account* out, std::string* err);
bool read_node_id(const Value* v, std::string_view field, NodeId* out, std::string* err);

// ---- writing -----------------------------------------------------------------

// Writer emits Marshal's exact bytes. Members are written in the order asked
// for, which is the Go struct's field order (an embedded struct's fields inline
// where the embedding sits).
class Writer {
public:
    void begin_object();
    void end_object();
    void begin_array();
    void end_array();
    // key names the next member. Every value writer below follows one.
    void key(std::string_view k);

    void u64(std::uint64_t v);
    void i64(std::int64_t v);
    void string(std::string_view s);
    void boolean(bool b);
    void null();
    // byte_array writes a Go [N]byte: an array of numbers.
    void byte_array(ByteView b);
    // bytes writes a Go []byte: base64, or null when nil.
    void bytes(const Bytes& b, bool is_nil);
    void raw(std::string_view already_json);

    const std::string& str() const { return out_; }

private:
    enum class Ctx { Obj, Arr };
    // separate emits the comma a container owes before its next element. An
    // object owes it before the KEY, an array before the value, which is the
    // whole difference and the reason the container kind is tracked.
    void separate();

    std::string out_;
    std::vector<std::pair<Ctx, bool>> stack_;
};

// escape_string renders a Go JSON string literal, HTML escaping included.
std::string escape_string(std::string_view s);

}  // namespace lux::fhevm::json
