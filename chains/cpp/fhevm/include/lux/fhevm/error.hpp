// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// error.hpp — the F-Chain's refusals, one enumerated value each.
//
// Every one of these DENIES an operation: the package fails secure, and there
// is no error here that a caller may proceed past. They render Go's sentinel
// errors one for one, and the code is the identity — a test asks whether a
// refusal IS ErrPermitRevoked the way the Go tests ask errors.Is, so the
// message can gain detail without breaking the check.

#pragma once

#include <expected>
#include <string>
#include <string_view>
#include <utility>

namespace lux::fhevm {

enum class Err {
    // Consensus / block shape
    InvalidBlock,
    NotOnTip,
    MempoolFull,

    // Transaction shape and pricing
    InvalidTxType,
    InvalidPayload,
    UnknownScheme,
    InvalidThreshold,
    InvalidCommittee,
    HandleMismatch,

    // Authentication
    UnsignedTx,
    PayerMismatch,
    BadSignature,

    // Objects
    CiphertextExists,
    CiphertextNotFound,
    PermitNotFound,
    PermitRevoked,
    PermitExpired,
    PermitInvalid,
    RequestNotFound,
    RequestClosed,
    RequestExpired,
    EpochNotFound,
    EpochMismatch,
    NotCommittee,
    Unauthorized,

    // Ordering
    BadNonce,
    DuplicateEffect,

    // Settlement (Go: github.com/luxfi/chains/fee)
    InsufficientFunds,
    BalanceOverflow,
    OutOfGas,

    // Node lifecycle. Not refusals of a transaction — refusals to act at all.
    VMShutdown,
    NoPendingTxs,
    NoParentBlock,
    ClockBehind,
    // A read that FAILED is not a read that found nothing: conflating the two
    // is how a live chain reads as a fresh one.
    Database,
};

// name is the sentinel's own text, so a failure message says which refusal it
// was rather than a number.
std::string_view name(Err e);

struct Error {
    Err code{};
    std::string detail;

    std::string message() const;
};

template <class T>
using Result = std::expected<T, Error>;

inline std::unexpected<Error> fail(Err code) { return std::unexpected(Error{code, {}}); }
inline std::unexpected<Error> fail(Err code, std::string detail) {
    return std::unexpected(Error{code, std::move(detail)});
}

// is reports whether a failed Result refused for this reason — the shape Go
// spells errors.Is.
template <class T>
inline bool is(const Result<T>& r, Err code) {
    return !r.has_value() && r.error().code == code;
}

}  // namespace lux::fhevm
