// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! On what terms a transaction is admitted at all.
//!
//! A chain that is post-quantum in its signatures and classical in its mempool
//! is classical: an attacker who can forge a classical signature does not care
//! that the block format could have carried a lattice one. So the bar is held
//! where a transaction first enters — [`crate::mempool`] — and this module is
//! the rule it holds.
//!
//! Go states the same thing in `vms/txs/auth`, one package with one entry
//! point, and the P-Chain and the X-Chain both call it from their mempool's
//! `Add`. [`admits`] is that entry point. The rule:
//!
//! - no profile at all is a WIRING mistake, not a permission — a mempool that
//!   was never told its chain's terms fails loud rather than open;
//! - a profile that does not require typed authorisation admits anything;
//! - a profile that does admits a transaction whose credentials are all
//!   post-quantum, and refuses one carrying a classical secp256k1 credential
//!   unless the originator is on a named list.
//!
//! THE LIST IS OPT-IN AND IT IS NOT A BACK DOOR. Its purpose is a chain part
//! way through a migration that has to carry a small, named set of operators
//! over. A chain that supplies none refuses every classical credential
//! outright, which is the default, and the list is read-only here on purpose:
//! widening it is a governance transaction, never something a mempool can do
//! while admitting.
//!
//! WHAT THIS CHAIN'S CREDENTIALS ARE. Every fx family the X-Chain runs —
//! secp256k1, nft, property — spends with a secp256k1 signature; the
//! post-quantum transaction shapes are their own families and are not part of
//! this chain. So under [`strict_pq`] with no list, this chain admits nothing,
//! and that is the correct answer rather than a gap: a strict-post-quantum
//! X-Chain is one whose spends are lattice-signed, and until that family
//! exists here the honest thing for the gate to say is no. Go's own X-Chain
//! test asserts exactly that refusal.
//!
//! WHAT IS NOT HERE. Go's `ChainSecurityProfile` carries some forty fields —
//! proof backends, hash suites, KEM choices. Those are `luxfi/consensus`
//! policy about a network, read by things that verify proofs. The two fields
//! below are the ones an admission gate reads, and a chain restating the other
//! thirty-eight would be a second place for them to be wrong.

use std::collections::BTreeSet;

use crate::error::{Error, Result};
use crate::fx::{Cred, Family};
use crate::ids::ShortId;

/// Which canonical profile this is. The numbers are the ones Go writes, so a
/// profile byte crossing between the two means the same thing.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u8)]
pub enum Which {
    /// Not a profile. A chain running this one has not been configured.
    None = 0x00,
    StrictPq = 0x01,
    Permissive = 0x02,
    Fips = 0x03,
}

impl Which {
    /// The name Go prints, so a log line reads the same in both languages.
    pub fn name(self) -> &'static str {
        match self {
            Which::None => "",
            Which::StrictPq => "STRICT",
            Which::Permissive => "PERMISSIVE",
            Which::Fips => "FIPS",
        }
    }

    /// The profile a wire byte names, or the byte itself when it names none.
    pub fn from_u8(v: u8) -> std::result::Result<Which, u8> {
        Ok(match v {
            0x01 => Which::StrictPq,
            0x02 => Which::Permissive,
            0x03 => Which::Fips,
            other => return Err(other),
        })
    }
}

/// A chain's terms, as far as admission is concerned.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Profile {
    pub which: Which,
    /// Whether a transaction must authorise itself with a post-quantum
    /// credential. This is the one field the gate reads.
    pub typed_auth: bool,
}

/// The strict post-quantum profile. This is the one a Lux chain pins.
pub fn strict_pq() -> Profile {
    Profile {
        which: Which::StrictPq,
        typed_auth: true,
    }
}

/// Testnet and devnet: legacy clients still sign classically.
pub fn permissive() -> Profile {
    Profile {
        which: Which::Permissive,
        typed_auth: false,
    }
}

/// The FIPS-aligned profile. Also post-quantum at the transaction.
pub fn fips() -> Profile {
    Profile {
        which: Which::Fips,
        typed_auth: true,
    }
}

/// The profile a wire byte names. [`Which::None`] is refused: it names no
/// terms, and a chain cannot run on terms nobody wrote down.
pub fn by_id(v: u8) -> Result<Profile> {
    match Which::from_u8(v) {
        Ok(Which::StrictPq) => Ok(strict_pq()),
        Ok(Which::Permissive) => Ok(permissive()),
        Ok(Which::Fips) => Ok(fips()),
        Ok(Which::None) | Err(_) => Err(Error::UnknownSecurityProfile(v)),
    }
}

/// Who may still sign classically on a chain that otherwise refuses it.
///
/// Read-only by design: the write path is a governance transaction, so nothing
/// reachable from admission can widen the list while it is being consulted.
/// Implementations must be safe to read from several threads.
pub trait Exempt: Send + Sync {
    fn holds(&self, addr: &ShortId) -> bool;
}

/// A list fixed at construction — what a genesis hard-codes before a
/// governance path exists.
#[derive(Debug, Default)]
pub struct Listed(BTreeSet<ShortId>);

impl Listed {
    pub fn of(addrs: &[ShortId]) -> Listed {
        Listed(addrs.iter().copied().collect())
    }

    pub fn len(&self) -> usize {
        self.0.len()
    }

    pub fn is_empty(&self) -> bool {
        self.0.is_empty()
    }
}

impl Exempt for Listed {
    fn holds(&self, addr: &ShortId) -> bool {
        self.0.contains(addr)
    }
}

/// Whether a credential is one an attacker with a quantum computer forges.
///
/// The three fx families of this chain all spend with a secp256k1 signature,
/// so all three are classical. This is a function of the family rather than a
/// flag on the credential, because the family is what the wire carries.
pub fn classical(cred: &Cred) -> bool {
    matches!(
        cred.family,
        Family::Secp256k1 | Family::Nft | Family::Property
    )
}

/// The gate. `Ok(())` exactly when this credential set may enter the mempool.
///
/// `originator` is the address the chain binds the transaction to. Go passes
/// the empty address on the X-Chain — a UTXO transaction has no single
/// originator until its inputs are resolved against state, and admission
/// happens before that — so a list that means to carry an X-Chain operator
/// over has to say so by holding [`crate::ids::SHORT_EMPTY`], deliberately,
/// rather than by accident.
pub fn admits(
    creds: &[Cred],
    profile: Option<&Profile>,
    exempt: Option<&dyn Exempt>,
    originator: &ShortId,
) -> Result<()> {
    let Some(profile) = profile else {
        return Err(Error::NoSecurityProfile);
    };
    if !profile.typed_auth {
        return Ok(());
    }
    for cred in creds {
        if !classical(cred) {
            continue;
        }
        match exempt {
            Some(list) if list.holds(originator) => continue,
            _ => return Err(Error::ClassicalCredentialRefused),
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fx::secp256k1::Credential;
    use crate::ids::SHORT_EMPTY;

    fn cred(family: Family) -> Cred {
        Cred::new(family, Credential { sigs: Vec::new() })
    }

    fn addr(n: u8) -> ShortId {
        ShortId::prefixed_bytes(&[n])
    }

    #[test]
    fn the_profile_numbers_and_names_are_the_ones_go_writes() {
        assert_eq!(Which::None as u8, 0x00);
        assert_eq!(Which::StrictPq as u8, 0x01);
        assert_eq!(Which::Permissive as u8, 0x02);
        assert_eq!(Which::Fips as u8, 0x03);
        assert_eq!(Which::StrictPq.name(), "STRICT");
        assert_eq!(Which::Permissive.name(), "PERMISSIVE");
        assert_eq!(Which::Fips.name(), "FIPS");
        assert_eq!(Which::from_u8(0x02), Ok(Which::Permissive));
        assert_eq!(Which::from_u8(0x7f), Err(0x7f));
    }

    #[test]
    fn a_profile_is_looked_up_by_the_byte_that_names_it() {
        assert_eq!(by_id(0x01).unwrap(), strict_pq());
        assert_eq!(by_id(0x02).unwrap(), permissive());
        assert_eq!(by_id(0x03).unwrap(), fips());
        // Naming no profile is not the same as naming a permissive one.
        assert_eq!(by_id(0x00), Err(Error::UnknownSecurityProfile(0x00)));
        assert_eq!(by_id(0x99), Err(Error::UnknownSecurityProfile(0x99)));
    }

    #[test]
    fn strict_and_fips_require_typed_authorisation_and_permissive_does_not() {
        assert!(strict_pq().typed_auth);
        assert!(fips().typed_auth);
        assert!(!permissive().typed_auth);
    }

    #[test]
    fn no_profile_is_a_wiring_mistake_and_fails_loud() {
        assert_eq!(
            admits(&[cred(Family::Secp256k1)], None, None, &SHORT_EMPTY),
            Err(Error::NoSecurityProfile)
        );
        // Even with nothing to check.
        assert_eq!(
            admits(&[], None, None, &SHORT_EMPTY),
            Err(Error::NoSecurityProfile)
        );
    }

    #[test]
    fn a_permissive_chain_admits_a_classical_credential() {
        assert_eq!(
            admits(
                &[cred(Family::Secp256k1)],
                Some(&permissive()),
                None,
                &SHORT_EMPTY
            ),
            Ok(())
        );
    }

    #[test]
    fn a_strict_chain_with_no_list_refuses_a_classical_credential() {
        for family in [Family::Secp256k1, Family::Nft, Family::Property] {
            assert_eq!(
                admits(&[cred(family)], Some(&strict_pq()), None, &SHORT_EMPTY),
                Err(Error::ClassicalCredentialRefused),
                "{family:?} spends with secp256k1 and is classical"
            );
        }
    }

    #[test]
    fn one_classical_credential_among_many_is_enough_to_refuse() {
        let creds = vec![cred(Family::Nft), cred(Family::Secp256k1)];
        assert_eq!(
            admits(&creds, Some(&strict_pq()), None, &SHORT_EMPTY),
            Err(Error::ClassicalCredentialRefused)
        );
    }

    #[test]
    fn a_transaction_with_no_credentials_is_admitted_by_any_profile() {
        // There is nothing classical about it. Whether it is a valid spend is
        // the executor's question, not the gate's.
        assert_eq!(admits(&[], Some(&strict_pq()), None, &SHORT_EMPTY), Ok(()));
    }

    #[test]
    fn only_a_named_originator_is_carried_over() {
        let list = Listed::of(&[addr(9)]);
        assert_eq!(list.len(), 1);
        assert!(!list.is_empty());
        assert_eq!(
            admits(
                &[cred(Family::Secp256k1)],
                Some(&strict_pq()),
                Some(&list),
                &addr(9)
            ),
            Ok(())
        );
        assert_eq!(
            admits(
                &[cred(Family::Secp256k1)],
                Some(&strict_pq()),
                Some(&list),
                &addr(8)
            ),
            Err(Error::ClassicalCredentialRefused)
        );
    }

    #[test]
    fn an_empty_list_carries_nobody_over() {
        let list = Listed::default();
        assert!(list.is_empty());
        assert_eq!(
            admits(
                &[cred(Family::Secp256k1)],
                Some(&strict_pq()),
                Some(&list),
                &SHORT_EMPTY
            ),
            Err(Error::ClassicalCredentialRefused)
        );
    }

    #[test]
    fn the_empty_address_is_carried_over_only_when_it_is_named() {
        // The X-Chain asks about the empty address, so a list meaning to carry
        // an X-Chain operator over has to hold it on purpose.
        let list = Listed::of(&[SHORT_EMPTY]);
        assert_eq!(
            admits(
                &[cred(Family::Secp256k1)],
                Some(&strict_pq()),
                Some(&list),
                &SHORT_EMPTY
            ),
            Ok(())
        );
    }
}
