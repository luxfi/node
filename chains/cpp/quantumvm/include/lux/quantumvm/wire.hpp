// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// wire.hpp — native-ZAP struct-is-wire for the Q-chain.
//
// Rendered from Go chains/quantumvm/wire.go, field offset for field offset. A
// block written here parses there and hashes to the same id, because both are
// the same arithmetic over the same layout.
//
// The wire is CANONICAL, and canonical means one byte string per block, not
// "the encoder emits one shape". Parse re-serializes what it decoded and
// refuses anything that does not come back byte-identical. Nothing weaker
// holds: a zap message declares its own size and its own root offset, so
// padding after the content, a relocated root struct and a root pointed at the
// wire header all decode to the same logical block under different sha256s. The
// block id is sha256(bytes), so each of those is a distinct id for one block —
// a fork the network builds by itself.
//
// Parse reconstructs the FULL transaction set, signature included. Verify
// checks every transaction's ML-DSA signature over that set, so binding the
// check to a field the parser skipped is what made it run on locally built
// blocks and never on received ones. A signature check a parser can switch off
// is not a check.

#pragma once

#include "lux/quantumvm/error.hpp"
#include "lux/quantumvm/id.hpp"
#include "lux/quantumvm/transaction.hpp"
#include "lux/quantumvm/zap.hpp"

#include <cstdint>
#include <memory>
#include <vector>

namespace lux::quantumvm::wire {

// MaxBlockSize bounds a block on the wire. Without it a peer decides how much
// memory this node allocates and how much its store holds: parse, verify and
// commit each walked whatever arrived.
inline constexpr std::size_t kMaxBlockSize = 2u << 20;  // 2 MiB

// ---- Block ----
//
//  Timestamp i64   @ 0    (Unix seconds — Q-Chain block-time resolution)
//  Height    u64   @ 8
//  ParentID  32B   @ 16
//  ChainID   32B   @ 48   (the chain this block belongs to)
//  NetworkID u32   @ 80   (the network that chain belongs to)
//  TxLens    list  @ 88   (u32 per transaction wire length)
//  TxBlob    bytes @ 96   (concatenated transaction wire bytes)
inline constexpr std::int64_t kBlkTime = 0;
inline constexpr std::int64_t kBlkHeight = 8;
inline constexpr std::int64_t kBlkParent = 16;
inline constexpr std::int64_t kBlkChain = 48;
inline constexpr std::int64_t kBlkNetwork = 80;
inline constexpr std::int64_t kBlkTxLens = 88;
inline constexpr std::int64_t kBlkTxBlob = 96;
inline constexpr std::int64_t kBlkSize = 104;

// ---- BaseTransaction, the signature preimage ----
//
//  Timestamp i64   @ 0   (Unix seconds)
//  Nonce     u64   @ 8
//  Data      bytes @ 16
inline constexpr std::int64_t kTxTime = 0;
inline constexpr std::int64_t kTxNonce = 8;
inline constexpr std::int64_t kTxData = 16;
inline constexpr std::int64_t kTxSize = 24;

// ---- transaction envelope: the preimage plus the signature over it ----
//
//  Body      bytes @ 0
//  Algorithm u32   @ 8
//  Stamped   i64   @ 16   (signature time, Unix nanoseconds)
//  PublicKey bytes @ 24   (ML-DSA public key)
//  Signature bytes @ 32   (ML-DSA signature over body ‖ stamp ‖ stamped)
//  Stamp     bytes @ 40   (quantum stamp)
inline constexpr std::int64_t kEnvBody = 0;
inline constexpr std::int64_t kEnvAlg = 8;
inline constexpr std::int64_t kEnvTime = 16;
inline constexpr std::int64_t kEnvKey = 24;
inline constexpr std::int64_t kEnvSig = 32;
inline constexpr std::int64_t kEnvStamp = 40;
inline constexpr std::int64_t kEnvSize = 48;

// The smallest an envelope can be: the zap header plus the fixed section, with
// every variable field null.
inline constexpr std::size_t kMinTxWire = zap::kHeaderSize + kEnvSize;

// The signature preimage of a transaction: what its id is taken over and what
// its signature covers. It excludes the signature.
Bytes tx_body_bytes(Seconds timestamp, std::uint64_t nonce, ByteView data);

// The envelope: the preimage and the signature over it. The interface hands
// over exactly the two, so any Transaction serializes the same way.
Bytes marshal_tx(const Transaction& tx);

// The signature preimage, decoded. Public because the conformance corpus
// carries a transaction BODY as its own vector — what a signature covers is a
// byte string in its own right, so it is parsed in its own right.
Result<std::shared_ptr<BaseTransaction>> parse_tx_body(ByteView body);

// marshal_tx's inverse. CoronaKey is the public key by construction (sign sets
// both from one key), so it is derived rather than carried — a second copy on
// the wire is a second thing to disagree.
Result<std::shared_ptr<BaseTransaction>> unmarshal_tx(ByteView data);

// The fields of a block, as they sit on the wire. Block (block.hpp) is this
// plus the behaviour; keeping them apart is what lets the wire be tested
// without a VM.
struct BlockFields {
    Seconds timestamp = 0;
    std::uint64_t height = 0;
    Id parent_id{};
    Id chain_id{};
    std::uint32_t network_id = 0;
    std::vector<TxPtr> transactions;
};

// The block's canonical ZAP wire.
Bytes block_bytes(const BlockFields& b);

// Decodes a block, transaction set included, and accepts the bytes only if they
// are the canonical encoding of what came out.
Result<BlockFields> parse_block_bytes(ByteView data);

// Rebuilds the transactions from the length list and the blob the lengths
// partition. The lengths must cover the blob exactly: bytes no length names are
// bytes the block commits to and nothing reads.
Result<std::vector<TxPtr>> parse_tx_set(const zap::List& lens, ByteView blob);

}  // namespace lux::quantumvm::wire
