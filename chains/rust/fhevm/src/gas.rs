// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What an operation costs.
//!
//! Two things are priced and they are priced separately, because they are two
//! different costs. The STRUCTURAL cost of an operation — authenticating a
//! signature, writing a record, indexing it — is the same whatever scheme the
//! ciphertext is under. The CRYPTOGRAPHIC cost an operation dispatches to the
//! off-chain committee is not: a ciphertext's size is linear in the ring
//! dimension N and its transform cost is N log N, so doubling N roughly doubles
//! the work, and at equal N the schemes differ because they carry different
//! numbers of ring elements and moduli. One flat rate would charge a TFHE
//! boolean what a CKKS n=2^15 vector costs, which is out by an order of
//! magnitude.
//!
//! [`SCHEMES`] is also the single answer to "which schemes does F accept". An
//! operation naming a scheme that is not in it is refused, never priced at the
//! bare base cost — so a scheme nobody can price is a scheme nobody can use.

use crate::error::{Code, Error, Result};
use crate::fee::{self, Gas};
use crate::tx::{Transaction, TX_ADVANCE_EPOCH, TX_FULFILL_DECRYPT, TX_GRANT_PERMIT};
use crate::tx::{TX_REGISTER_CIPHERTEXT, TX_REQUEST_DECRYPT, TX_REVOKE_PERMIT};

/// nLUX per unit of gas. Chosen so the cheapest priced operation still settles
/// at or above the node's own minimum-fee floor, which is what keeps the
/// admission surface and the settlement surface from drifting apart.
pub const GAS_PRICE: Gas = 1_000;

/// What a byte costs. This prices the two fields whose length the payer
/// chooses — the payload and the scheme name — because those are what a
/// transaction puts on the chain forever. It follows Ethereum's non-zero
/// calldata rate for the same reason: storage is the cost a base fee cannot
/// express, and it is what stops a ciphertext body riding onto F for the price
/// of the handle that was supposed to replace it.
pub const GAS_PER_BYTE: Gas = 16;

/// The structural cost of each operation.
fn base(tx_type: u8) -> Option<Gas> {
    Some(match tx_type {
        TX_REGISTER_CIPHERTEXT => 21_000,
        TX_GRANT_PERMIT => 8_000,
        TX_REVOKE_PERMIT => 3_000,
        TX_REQUEST_DECRYPT => 15_000,
        TX_FULFILL_DECRYPT => 10_000,
        TX_ADVANCE_EPOCH => 5_000,
        _ => return None,
    })
}

/// The schemes F prices, and therefore the schemes F accepts.
pub const SCHEMES: &[(&str, Gas)] = &[
    ("tfhe-n10", 12_000),
    ("tfhe-n11", 24_000),
    ("bfv-n13", 25_000),
    ("bfv-n14", 50_000),
    ("bgv-n13", 26_000),
    ("bgv-n14", 52_000),
    ("ckks-n13", 30_000),
    ("ckks-n14", 60_000),
    ("ckks-n15", 120_000),
];

fn scheme_gas(scheme: &[u8]) -> Option<Gas> {
    SCHEMES.iter().find(|(n, _)| n.as_bytes() == scheme).map(|(_, g)| *g)
}

/// Whether F prices this operation is a scheme.
///
/// Registering a ciphertext and requesting its decryption both scale with the
/// scheme — the first in the size the network carries and indexes, the second
/// in the committee's partial-decryption and combination work. Granting,
/// revoking, attesting a finished result and advancing an epoch are fixed-size
/// record writes that touch no ciphertext, so pricing them under a scheme would
/// be a price for work nobody does.
pub fn uses_scheme(tx_type: u8) -> bool {
    tx_type == TX_REGISTER_CIPHERTEXT || tx_type == TX_REQUEST_DECRYPT
}

/// The metered gas for a transaction. Fails closed on an operation F does not
/// run and on a scheme it cannot price.
pub fn gas_for(tx: &Transaction) -> Result<Gas> {
    let Some(base) = base(tx.tx_type) else {
        // gas.go returns this one WITHOUT wrapping a sentinel, so the whole
        // sentence is what a classifier reads — and the word in it that decides
        // the class is "unknown".
        return Err(Error {
            code: Code::UnknownTxTypeGas,
            detail: format!("{}", tx.tx_type),
        });
    };
    let mut total = base;
    if uses_scheme(tx.tx_type) {
        let Some(sg) = scheme_gas(&tx.scheme) else {
            return Err(Error::detail(
                Code::UnknownScheme,
                format!("{:?}", String::from_utf8_lossy(&tx.scheme)),
            ));
        };
        total += sg;
    }
    Ok(total + (tx.payload.len() + tx.scheme.len()) as Gas * GAS_PER_BYTE)
}

/// The nLUX a transaction settles.
pub fn fee_for(tx: &Transaction) -> Result<u64> {
    fee::cost(gas_for(tx)?, GAS_PRICE)
}

/// Whether a scheme is priced, and therefore accepted.
pub fn supported_scheme(scheme: &[u8]) -> bool {
    scheme_gas(scheme).is_some()
}

/// The smallest fee any valid operation can settle.
pub fn min_scheduled_fee() -> u64 {
    let cheapest = [
        TX_REGISTER_CIPHERTEXT,
        TX_GRANT_PERMIT,
        TX_REVOKE_PERMIT,
        TX_REQUEST_DECRYPT,
        TX_FULFILL_DECRYPT,
        TX_ADVANCE_EPOCH,
    ]
    .iter()
    .filter_map(|t| base(*t))
    .min()
    .unwrap_or(0);
    fee::cost(cheapest, GAS_PRICE).unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn tx(t: u8, scheme: &str, payload: &[u8]) -> Transaction {
        Transaction {
            tx_type: t,
            scheme: scheme.as_bytes().to_vec(),
            payload: payload.to_vec(),
            ..Transaction::default()
        }
    }

    #[test]
    fn a_scheme_bearing_operation_is_priced_by_its_scheme() {
        // register base 21_000, ckks-n14 60_000, and the bytes of both fields.
        let t = tx(TX_REGISTER_CIPHERTEXT, "ckks-n14", b"{}");
        assert_eq!(gas_for(&t).unwrap(), 21_000 + 60_000 + (2 + 8) * 16);
        // The same operation under a bigger ring costs roughly twice as much.
        let big = tx(TX_REGISTER_CIPHERTEXT, "ckks-n15", b"{}");
        assert_eq!(gas_for(&big).unwrap() - gas_for(&t).unwrap(), 120_000 - 60_000);
    }

    #[test]
    fn an_operation_that_touches_no_ciphertext_is_not_priced_by_a_scheme() {
        // A revoke carries no scheme and is priced without one; naming one
        // would be paying for work nobody does.
        let t = tx(TX_REVOKE_PERMIT, "", b"{}");
        assert_eq!(gas_for(&t).unwrap(), 3_000 + 2 * 16);
        // And naming one anyway does not change the price beyond its bytes.
        let named = tx(TX_REVOKE_PERMIT, "ckks-n15", b"{}");
        assert_eq!(gas_for(&named).unwrap(), 3_000 + (2 + 8) * 16);
    }

    #[test]
    fn a_scheme_nobody_prices_is_refused_rather_than_charged_the_base() {
        let t = tx(TX_REGISTER_CIPHERTEXT, "no-such-scheme", b"{}");
        let e = gas_for(&t).unwrap_err();
        assert_eq!(e.code, Code::UnknownScheme);
        assert_eq!(e.class(), crate::error::UNSUPPORTED);
        assert!(!supported_scheme(b"no-such-scheme"));
        assert!(supported_scheme(b"ckks-n14"));
    }

    #[test]
    fn an_operation_f_does_not_run_has_no_price() {
        let e = gas_for(&tx(9, "", b"{}")).unwrap_err();
        assert_eq!(e.code, Code::UnknownTxTypeGas);
        assert_eq!(e.class(), crate::error::UNSUPPORTED);
    }

    #[test]
    fn every_byte_the_payer_chooses_is_paid_for() {
        let small = tx(TX_GRANT_PERMIT, "", b"{}");
        let large = tx(TX_GRANT_PERMIT, "", &vec![b'x'; 1002]);
        assert_eq!(gas_for(&large).unwrap() - gas_for(&small).unwrap(), 1000 * GAS_PER_BYTE);
    }

    #[test]
    fn the_cheapest_operation_still_settles_above_the_nodes_floor() {
        // node/vms/types/fee.MinTxFeeFloor is 1 mLUX = 1_000_000 nLUX.
        assert_eq!(min_scheduled_fee(), 3_000 * GAS_PRICE);
        assert!(min_scheduled_fee() >= 1_000_000);
    }

    #[test]
    fn a_fee_is_its_gas_at_the_price() {
        let t = tx(TX_FULFILL_DECRYPT, "", b"{}");
        assert_eq!(fee_for(&t).unwrap(), gas_for(&t).unwrap() * GAS_PRICE);
    }
}
