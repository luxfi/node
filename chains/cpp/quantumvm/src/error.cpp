// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/quantumvm/error.hpp"

namespace lux::quantumvm {

std::string_view err_name(Err e) {
    switch (e) {
        case Err::None: return "ok";

        case Err::BlockTooLarge: return "quantumvm: block exceeds the wire bound";
        case Err::NonCanonical: return "quantumvm: block wire is not the canonical encoding of its content";
        case Err::TxCountAbsurd: return "quantumvm: block declares more transactions than its bytes can hold";
        case Err::TxBlobMismatch: return "quantumvm: transaction lengths do not partition the transaction blob";
        case Err::NotAMessage: return "quantumvm: bytes are not a zap message";
        case Err::TrailingBytes: return "quantumvm: trailing bytes";
        case Err::ShortHeader: return "quantumvm: the wire ends inside the header";

        case Err::BlockVerificationFailed: return "quantumvm: block transaction signatures failed verification";
        case Err::InvalidBlockHeight: return "quantumvm: invalid block height";
        case Err::InvalidParentID: return "quantumvm: parent block not found";
        case Err::TimeBeforeParent: return "quantumvm: block timestamp precedes its parent";
        case Err::TimeTooFarAhead: return "quantumvm: block timestamp is beyond the skew allowance";
        case Err::EmptyBlock: return "quantumvm: block carries no transactions";
        case Err::ForeignChain: return "quantumvm: block belongs to another chain";
        case Err::Execute: return "quantumvm: block transaction could not be applied";

        case Err::PoolClosed: return "quantumvm: transaction pool is closed";
        case Err::PoolFull: return "quantumvm: transaction pool is full";
        case Err::DuplicateTx: return "quantumvm: transaction already in the pool";
        case Err::TxNotInPool: return "quantumvm: transaction not in the pool";
        case Err::MissingStamp: return "quantumvm: missing quantum signature";

        case Err::NoPendingTxs: return "quantumvm: no pending transactions";
        case Err::VMShutdown: return "quantumvm: VM is shutting down";
        case Err::ParallelProcessingFailed: return "quantumvm: no pending transaction survived verification";
        case Err::NoBlockAtHeight: return "quantumvm: no block at height";
        case Err::NoIdentity: return "quantumvm: the node has no identity to sign under";
        case Err::TipUnreadable: return "quantumvm: the chain tip cannot be read";
        case Err::NotTheTip: return "quantumvm: block does not extend the tip";
        case Err::ClockBehindTip: return "quantumvm: the node's clock trails its own tip beyond the skew allowance";
        case Err::NoStamp: return "quantumvm: no stamp to check";
        case Err::NotConfigured: return "quantumvm: config";

        case Err::DuplicateSigner: return "quantumvm: validator already signed this block";
        case Err::UnverifiedSigner: return "quantumvm: signature does not verify for the validator it claims";
        case Err::UnknownBlock: return "quantumvm: no block awaiting signatures";
        case Err::NoValidatorID: return "quantumvm: a signer with no identity cannot be a member of a quorum";
        case Err::AggregateRefused: return "quantumvm: the aggregate of the collected signatures does not verify";
        case Err::AlreadyRegistered: return "quantumvm: validator is already in the committee";
        case Err::CommitteeFull: return "quantumvm: the committee is full";
        case Err::CommitteeTooSmall: return "quantumvm: a committee that small tolerates no fault";
        case Err::NotEnoughSignatures: return "quantumvm: insufficient signatures";
        case Err::SignRefused: return "quantumvm: sign failed";
        case Err::Cancelled: return "quantumvm: cancelled";

        case Err::InvalidQuantumSignature: return "invalid quantum signature";
        case Err::InvalidCoronaKey: return "invalid corona key";
        case Err::QuantumStampExpired: return "quantum stamp expired";
        case Err::QuantumVerificationFailed: return "quantum verification failed";
        case Err::UnsupportedAlgorithm: return "unsupported quantum algorithm";
        case Err::BatchMismatch: return "message and signature count mismatch";
        case Err::OffWidth: return "quantumvm: ML-DSA takes a fixed width";
        case Err::NoAccelerator: return "quantumvm: no accelerator to verify on";

        case Err::ChainAcceptsNoUserTxs: return "this chain accepts no user transactions";
        case Err::NoFeePolicy: return "quantumvm: fee policy not initialized";

        case Err::NotFound: return "not found";
        case Err::StoreClosed: return "quantumvm: the store is closed";
        case Err::StoreUnwritable: return "quantumvm: the store would not take the write";
        case Err::StoreCorrupt: return "quantumvm: what came back is not what this chain writes";
    }
    return "unknown";
}

}  // namespace lux::quantumvm
