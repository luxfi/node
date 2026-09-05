// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/fhevm/error.hpp"

namespace lux::fhevm {

std::string_view name(Err e) {
    switch (e) {
        case Err::InvalidBlock:       return "fhevm: malformed block";
        case Err::NotOnTip:           return "fhevm: block does not extend the accepted tip";
        case Err::MempoolFull:        return "fhevm: mempool is full";
        case Err::InvalidTxType:      return "fhevm: invalid transaction type";
        case Err::InvalidPayload:     return "fhevm: invalid transaction payload";
        case Err::UnknownScheme:      return "fhevm: unsupported FHE scheme";
        case Err::InvalidThreshold:   return "fhevm: invalid threshold (need 0 < t <= n)";
        case Err::InvalidCommittee:   return "fhevm: invalid committee";
        case Err::HandleMismatch:     return "fhevm: subject does not match its payload";
        case Err::UnsignedTx:         return "fhevm: transaction missing payer auth/signature";
        case Err::PayerMismatch:      return "fhevm: payer does not match auth public key";
        case Err::BadSignature:       return "fhevm: invalid payer signature";
        case Err::CiphertextExists:   return "fhevm: ciphertext already registered";
        case Err::CiphertextNotFound: return "fhevm: ciphertext not found";
        case Err::PermitNotFound:     return "fhevm: permit not found";
        case Err::PermitRevoked:      return "fhevm: permit is revoked";
        case Err::PermitExpired:      return "fhevm: permit expired";
        case Err::PermitInvalid:      return "fhevm: permit does not authorize this operation";
        case Err::RequestNotFound:    return "fhevm: decrypt request not found";
        case Err::RequestClosed:      return "fhevm: decrypt request already answered";
        case Err::RequestExpired:     return "fhevm: decrypt request expired";
        case Err::EpochNotFound:      return "fhevm: epoch not found";
        case Err::EpochMismatch:      return "fhevm: epoch is not the next one";
        case Err::NotCommittee:       return "fhevm: payer is not a committee member";
        case Err::Unauthorized:       return "fhevm: payer not authorized for operation";
        case Err::BadNonce:           return "fhevm: bad or replayed nonce";
        case Err::DuplicateEffect:    return "fhevm: effect already claimed by a pending transaction";
        case Err::InsufficientFunds:  return "fee: insufficient funds";
        case Err::BalanceOverflow:    return "fee: balance overflow";
        case Err::OutOfGas:           return "fee: out of gas";
        case Err::VMShutdown:         return "fhevm: shutting down";
        case Err::NoPendingTxs:       return "fhevm: no pending transactions";
        case Err::NoParentBlock:      return "fhevm: no parent block";
        case Err::ClockBehind:
            return "fhevm: local clock trails the chain tip by more than the skew allowance";
        case Err::Database:           return "fhevm: state read failed";
    }
    return "fhevm: unknown error";
}

std::string Error::message() const {
    std::string s(name(code));
    if (!detail.empty()) {
        s += ": ";
        s += detail;
    }
    return s;
}

}  // namespace lux::fhevm
