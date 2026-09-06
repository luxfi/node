// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// error.hpp — every reason this chain refuses something, named once.
//
// A refusal is a VALUE, not a string and not a bool. That is what lets a test
// say "this input is rejected for THIS reason", the way the Go tests do
// (`require.ErrorIs(err, errNotTheTip)`), rather than only "something went
// wrong" — a rule that rejects for an unintended reason is a bug the weaker
// assertion cannot see.
//
// One enumerator per sentinel error in the Go reference, carrying the Go name it
// renders so the two cannot drift apart quietly. Detail text is for humans and
// is never compared.

#pragma once

#include <expected>
#include <string>
#include <string_view>
#include <utility>

namespace lux::quantumvm {

enum class Err {
    None = 0,

    // ── the wire (wire.go)
    BlockTooLarge,   // errBlockTooLarge
    NonCanonical,    // errNonCanonical
    TxCountAbsurd,   // errTxCountAbsurd
    TxBlobMismatch,  // errTxBlobMismatch
    NotAMessage,     // zap.Parse refused the bytes
    TrailingBytes,   // "block trailing bytes" / "transaction trailing bytes"
    ShortHeader,     // "block wire ends N bytes into a K-byte header"

    // ── the block (block.go)
    BlockVerificationFailed,  // errBlockVerificationFailed
    InvalidBlockHeight,       // errInvalidBlockHeight
    InvalidParentID,          // errInvalidParentID
    TimeBeforeParent,         // errTimeBeforeParent
    TimeTooFarAhead,          // errTimeTooFarAhead
    EmptyBlock,               // errEmptyBlock
    ForeignChain,             // errForeignChain
    Execute,                  // errExecute

    // ── the mempool (transaction.go)
    PoolClosed,    // errPoolClosed
    PoolFull,      // errPoolFull
    DuplicateTx,   // errDuplicateTx
    TxNotInPool,   // errTxNotInPool
    MissingStamp,  // errMissingStamp

    // ── the VM (vm.go)
    NoPendingTxs,             // errNoPendingTxs
    VMShutdown,               // errVMShutdown
    ParallelProcessingFailed, // errParallelProcessingFailed
    NoBlockAtHeight,          // errNoBlockAtHeight
    NoIdentity,               // errNoIdentity
    TipUnreadable,            // errTipUnreadable
    NotTheTip,                // errNotTheTip
    ClockBehindTip,           // errClockBehindTip
    NoStamp,                  // errNoStamp
    NotConfigured,            // config: the VM was handed something it cannot run on

    // ── the finality bridge (quasar.go)
    DuplicateSigner,    // errDuplicateSigner
    UnverifiedSigner,   // errUnverifiedSigner
    UnknownBlock,       // errUnknownBlock
    NoValidatorID,      // errNoValidatorID
    AggregateRefused,   // errAggregateRefused
    AlreadyRegistered,  // errAlreadyRegistered
    CommitteeFull,      // errCommitteeFull
    CommitteeTooSmall,  // "a committee of n tolerates no fault"
    NotEnoughSignatures,// signer.AggregateSignatures "insufficient signatures"
    SignRefused,        // the core would not produce a signature
    Cancelled,          // the caller gave up before the signature was made

    // ── the quantum signer (quantum/signer.go)
    InvalidQuantumSignature,   // ErrInvalidQuantumSignature
    InvalidCoronaKey,          // ErrInvalidCoronaKey
    QuantumStampExpired,       // ErrQuantumStampExpired
    QuantumVerificationFailed, // ErrQuantumVerificationFailed
    UnsupportedAlgorithm,      // ErrUnsupportedAlgorithm
    BatchMismatch,             // "message and signature count mismatch"
    OffWidth,                  // packBatch: a key or signature is not the mode's width
    NoAccelerator,             // gpuBatchVerify with no accelerator to verify on

    // ── the fee policy (feegate.go, chains/fee)
    ChainAcceptsNoUserTxs,  // fee.ErrChainAcceptsNoUserTxs
    NoFeePolicy,            // "fee policy not initialized"

    // ── what the chain remembers across a restart
    NotFound,         // database.ErrNotFound — the store holds no such key
    StoreUnwritable,  // the store would not take a write, or would not make it durable
    StoreCorrupt,     // what came back is not what this chain writes
    StoreClosed,      // the store was shut down and cannot answer or stage anything
};

std::string_view err_name(Err e);

struct Error {
    Err code = Err::None;
    std::string detail;

    Error() = default;
    Error(Err c) : code(c) {}
    Error(Err c, std::string d) : code(c), detail(std::move(d)) {}

    friend bool operator==(const Error& a, const Error& b) { return a.code == b.code; }
    std::string message() const {
        std::string s(err_name(code));
        if (!detail.empty()) {
            s += ": ";
            s += detail;
        }
        return s;
    }
};

template <class T>
using Result = std::expected<T, Error>;

using Status = std::expected<void, Error>;

inline std::unexpected<Error> fail(Err e) { return std::unexpected(Error(e)); }
inline std::unexpected<Error> fail(Err e, std::string d) { return std::unexpected(Error(e, std::move(d))); }
inline Status ok() { return Status{}; }

}  // namespace lux::quantumvm
