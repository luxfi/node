// SPDX-License-Identifier: BSD-3-Clause-Eco

//! The check that value is not created.
//!
//! Everything a transaction makes must be backed by something it spends. That
//! is one sentence and three rules, because a stakeable lock makes value
//! non-fungible with itself:
//!
//! 1. Unlocked value produced may not exceed unlocked value consumed.
//! 2. Value produced locked until some time, for some owner, may not exceed
//!    value consumed locked until that same time for that same owner.
//! 3. A shortfall in rule 2 may be covered by spending unlocked value — you
//!    may always lock more of your own money — but never the other way round.
//!
//! The owner is part of the key in rule 2 on purpose: without it, one person's
//! locked tokens could pay for another person's locked output, and a lock
//! would only bind the amount rather than the holder. The owner's identity is
//! the hash of its one canonical encoding, so two spellings of the same owner
//! cannot become two owners.
//!
//! A lock that has already expired is not a lock: an output locked until a
//! time in the past is counted as unlocked, which is what lets staked tokens
//! come back into circulation without a special transaction.

use std::collections::HashMap;

use crate::components::{Credential, Input, Output, Owners, Utxo};
use crate::ids::Id;

/// Why a transaction does not add up.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    /// One credential per input, no more and no fewer.
    WrongNumberOfCredentials {
        inputs: usize,
        credentials: usize,
    },
    WrongNumberOfUtxos {
        inputs: usize,
        utxos: usize,
    },
    /// The input claims an asset the output it names is not.
    AssetMismatch,
    /// A locked output spent by an input that does not admit the lock.
    LockedFundsNotMarkedAsLocked,
    /// An input that admits a lock, and names the wrong time.
    LocktimeMismatch {
        claimed: u64,
        actual: u64,
    },
    /// More locked value produced for an owner and time than was consumed for
    /// that owner and time, with no unlocked value left to cover it.
    InsufficientLockedFunds,
    /// More unlocked value produced than consumed.
    InsufficientUnlockedFunds {
        needed: u64,
        asset: Id,
    },
    /// A sum that does not fit.
    Overflow,
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::WrongNumberOfCredentials {
                inputs,
                credentials,
            } => write!(f, "{inputs} inputs != {credentials} credentials"),
            Error::WrongNumberOfUtxos { inputs, utxos } => {
                write!(f, "{inputs} inputs != {utxos} utxos")
            }
            Error::AssetMismatch => write!(f, "input asset does not match the utxo it names"),
            Error::LockedFundsNotMarkedAsLocked => {
                write!(f, "locked funds not marked as locked")
            }
            Error::LocktimeMismatch { claimed, actual } => {
                write!(f, "locktime {claimed} != {actual}")
            }
            Error::InsufficientLockedFunds => write!(f, "insufficient locked funds"),
            Error::InsufficientUnlockedFunds { needed, .. } => {
                write!(f, "needs {needed} more of the asset")
            }
            Error::Overflow => write!(f, "overflow"),
        }
    }
}

impl std::error::Error for Error {}

/// Check that `outs` plus `fees` is covered by `utxos`.
///
/// `now` is the time locks are measured against. `fees` is what the
/// transaction must burn on top of what it makes — the caller supplies it
/// because how much a transaction costs is the fee policy's business and not
/// this function's.
///
/// Signature verification is the caller's too: this decides whether the
/// arithmetic holds, and [`verify_credentials`] decides whether the signatures
/// authorise it. Keeping them apart means neither can be accidentally skipped
/// by satisfying the other.
pub fn verify_spend(
    utxos: &[Utxo],
    ins: &[Input],
    outs: &[Output],
    creds: &[Credential],
    fees: &HashMap<Id, u64>,
    now: u64,
) -> Result<(), Error> {
    if ins.len() != creds.len() {
        return Err(Error::WrongNumberOfCredentials {
            inputs: ins.len(),
            credentials: creds.len(),
        });
    }
    if ins.len() != utxos.len() {
        return Err(Error::WrongNumberOfUtxos {
            inputs: ins.len(),
            utxos: utxos.len(),
        });
    }

    // asset -> amount
    let mut unlocked_consumed: HashMap<Id, u64> = HashMap::new();
    // asset -> locktime -> owner -> amount
    let mut locked_consumed: HashMap<Id, HashMap<u64, HashMap<Id, u64>>> = HashMap::new();
    let mut locked_produced: HashMap<Id, HashMap<u64, HashMap<Id, u64>>> = HashMap::new();
    let mut unlocked_produced: HashMap<Id, u64> = fees.clone();

    for (input, utxo) in ins.iter().zip(utxos.iter()) {
        if utxo.output.asset != input.asset {
            return Err(Error::AssetMismatch);
        }

        let locktime = utxo.output.stake_lock;

        // An input spending a still-locked output must say so. Without this an
        // input could simply omit the lock and spend it as ordinary money.
        if now < locktime && input.stake_lock == 0 {
            return Err(Error::LockedFundsNotMarkedAsLocked);
        }
        if input.stake_lock != 0 && input.stake_lock != locktime {
            return Err(Error::LocktimeMismatch {
                claimed: input.stake_lock,
                actual: locktime,
            });
        }

        if now >= locktime {
            let e = unlocked_consumed.entry(utxo.output.asset).or_insert(0);
            *e = e.checked_add(input.amount).ok_or(Error::Overflow)?;
            continue;
        }

        let owner = utxo.output.owners.id();
        let e = locked_consumed
            .entry(utxo.output.asset)
            .or_default()
            .entry(locktime)
            .or_default()
            .entry(owner)
            .or_insert(0);
        *e = e.checked_add(input.amount).ok_or(Error::Overflow)?;
    }

    for out in outs {
        if out.stake_lock == 0 {
            let e = unlocked_produced.entry(out.asset).or_insert(0);
            *e = e.checked_add(out.amount).ok_or(Error::Overflow)?;
            continue;
        }
        let owner = out.owners.id();
        let e = locked_produced
            .entry(out.asset)
            .or_default()
            .entry(out.stake_lock)
            .or_default()
            .entry(owner)
            .or_insert(0);
        *e = e.checked_add(out.amount).ok_or(Error::Overflow)?;
    }

    // Rule 2, and rule 3 where it falls short.
    for (asset, by_time) in &locked_produced {
        let consumed_asset = locked_consumed.get(asset);
        for (locktime, by_owner) in by_time {
            let consumed_time = consumed_asset.and_then(|m| m.get(locktime));
            for (owner, produced) in by_owner {
                let consumed = consumed_time
                    .and_then(|m| m.get(owner))
                    .copied()
                    .unwrap_or(0);
                if *produced > consumed {
                    let increase = produced - consumed;
                    let available = unlocked_consumed.get(asset).copied().unwrap_or(0);
                    if increase > available {
                        return Err(Error::InsufficientLockedFunds);
                    }
                    unlocked_consumed.insert(*asset, available - increase);
                }
            }
        }
    }

    // Rule 1.
    for (asset, produced) in &unlocked_produced {
        let consumed = unlocked_consumed.get(asset).copied().unwrap_or(0);
        if *produced > consumed {
            return Err(Error::InsufficientUnlockedFunds {
                needed: produced - consumed,
                asset: *asset,
            });
        }
    }

    Ok(())
}

/// Whether the credentials authorise the inputs.
///
/// Each input names which of its owner's addresses signed, in order; each
/// named address must have recovered from the signature at the matching
/// position. Go reaches this through its fx indirection; the rule is the same
/// one and it is stated here where the spend is checked.
///
/// The address a signature names comes from [`crate::sign::recover`] — the
/// one implementation in this crate. It is called here rather than passed in,
/// because a check that can be handed a different answer is a check that can
/// be handed one that always agrees.
pub fn verify_credentials(
    utxos: &[Utxo],
    ins: &[Input],
    creds: &[Credential],
    sighash: &Id,
    now: u64,
) -> Result<(), CredentialError> {
    if ins.len() != creds.len() {
        return Err(CredentialError::WrongNumberOfCredentials);
    }
    for ((input, cred), utxo) in ins.iter().zip(creds.iter()).zip(utxos.iter()) {
        verify_permission(&utxo.output.owners, &input.sig_indices, cred, sighash, now)?;
    }
    Ok(())
}

/// Whether one credential satisfies one owner.
///
/// Go: `secp256k1fx.Fx.VerifyPermission`. This is the whole signature rule, in
/// one place, used by both things that need it: a spend, where the owner is the
/// output being consumed, and an authorisation, where the owner is a network's
/// or a validator's. Two copies of this rule would be two answers to who
/// signed.
pub fn verify_permission(
    owners: &Owners,
    sig_indices: &[u32],
    cred: &Credential,
    sighash: &Id,
    now: u64,
) -> Result<(), CredentialError> {
    // The owner's own locktime — distinct from the stakeable lock, and checked
    // here because an output whose owner-time has not arrived is not spendable
    // by anyone.
    if now < owners.locktime {
        return Err(CredentialError::TimelockNotExpired);
    }
    if sig_indices.len() != owners.threshold as usize {
        return Err(CredentialError::WrongNumberOfSignatures {
            given: sig_indices.len(),
            needed: owners.threshold as usize,
        });
    }
    if cred.sigs.len() != sig_indices.len() {
        return Err(CredentialError::WrongNumberOfSignatures {
            given: cred.sigs.len(),
            needed: sig_indices.len(),
        });
    }
    for (position, index) in sig_indices.iter().enumerate() {
        let expected = owners
            .addrs
            .get(*index as usize)
            .ok_or(CredentialError::AddressIndexOutOfBounds)?;
        // The address a signature names comes from [`crate::sign::recover`] —
        // the one implementation in this crate. It is called here rather than
        // passed in, because a check that can be handed a different answer is a
        // check that can be handed one that always agrees.
        let got = crate::sign::recover(sighash, &cred.sigs[position])
            .ok_or(CredentialError::MalformedSignature)?;
        if got != *expected {
            return Err(CredentialError::WrongSigner);
        }
    }
    Ok(())
}

/// Why the signatures do not authorise the spend.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CredentialError {
    WrongNumberOfCredentials,
    WrongNumberOfSignatures {
        given: usize,
        needed: usize,
    },
    /// An index naming an address the owner does not have.
    AddressIndexOutOfBounds,
    MalformedSignature,
    /// A valid signature by someone who is not the named owner.
    WrongSigner,
    /// The owner's own locktime has not passed.
    TimelockNotExpired,
}

impl std::fmt::Display for CredentialError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            CredentialError::WrongNumberOfCredentials => write!(f, "wrong number of credentials"),
            CredentialError::WrongNumberOfSignatures { given, needed } => {
                write!(f, "{given} signatures, {needed} needed")
            }
            CredentialError::AddressIndexOutOfBounds => write!(f, "address index out of bounds"),
            CredentialError::MalformedSignature => write!(f, "malformed signature"),
            CredentialError::WrongSigner => write!(f, "signature is not the owner's"),
            CredentialError::TimelockNotExpired => write!(f, "timelock has not expired"),
        }
    }
}

impl std::error::Error for CredentialError {}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::components::{Owners, UtxoId};
    use crate::ids::ShortId;

    const ASSET: Id = [9u8; 32];

    fn owner(n: u8) -> Owners {
        Owners {
            locktime: 0,
            threshold: 1,
            addrs: vec![ShortId([n; 20])],
        }
    }

    fn utxo(n: u8, amount: u64, lock: u64, who: u8) -> Utxo {
        Utxo {
            id: UtxoId {
                tx_id: [n; 32],
                output_index: 0,
            },
            output: Output {
                asset: ASSET,
                stake_lock: lock,
                amount,
                owners: owner(who),
            },
        }
    }

    fn spend(u: &Utxo, lock: u64) -> Input {
        Input {
            utxo: u.id,
            asset: u.output.asset,
            stake_lock: lock,
            amount: u.output.amount,
            sig_indices: vec![0],
        }
    }

    fn out(amount: u64, lock: u64, who: u8) -> Output {
        Output {
            asset: ASSET,
            stake_lock: lock,
            amount,
            owners: owner(who),
        }
    }

    /// A key, and the address it spends under. Signatures in these tests are
    /// real: a credential check that accepted a made-up answer would be a
    /// check of the bookkeeping around signatures rather than of signatures.
    fn key(seed: u8) -> k256::ecdsa::SigningKey {
        k256::ecdsa::SigningKey::from_bytes(&[seed; 32].into()).expect("a key")
    }

    fn key_address(seed: u8) -> ShortId {
        crate::sign::address(key(seed).verifying_key().to_encoded_point(true).as_bytes())
    }

    /// An unspent output owned by the holder of `seed`.
    fn utxo_of(n: u8, amount: u64, seed: u8) -> Utxo {
        let mut u = utxo(n, amount, 0, 0);
        u.output.owners = Owners {
            locktime: 0,
            threshold: 1,
            addrs: vec![key_address(seed)],
        };
        u
    }

    fn credential(seed: u8, sighash: &Id) -> Credential {
        Credential {
            sigs: vec![crate::sign::sign(&key(seed), sighash)],
        }
    }

    fn no_fees() -> HashMap<Id, u64> {
        HashMap::new()
    }

    fn fee(n: u64) -> HashMap<Id, u64> {
        let mut m = HashMap::new();
        m.insert(ASSET, n);
        m
    }

    fn creds(n: usize) -> Vec<Credential> {
        vec![Credential::default(); n]
    }

    #[test]
    fn spending_exactly_what_is_made_holds() {
        let u = utxo(1, 100, 0, 1);
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[out(100, 0, 1)],
                &creds(1),
                &no_fees(),
                0
            ),
            Ok(())
        );
    }

    #[test]
    fn making_more_than_was_spent_is_refused() {
        let u = utxo(1, 100, 0, 1);
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[out(101, 0, 1)],
                &creds(1),
                &no_fees(),
                0
            ),
            Err(Error::InsufficientUnlockedFunds {
                needed: 1,
                asset: ASSET
            })
        );
    }

    #[test]
    fn the_fee_must_be_covered_too() {
        let u = utxo(1, 100, 0, 1);
        // 100 spent, 100 made, 1 fee: one short.
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[out(100, 0, 1)],
                &creds(1),
                &fee(1),
                0
            ),
            Err(Error::InsufficientUnlockedFunds {
                needed: 1,
                asset: ASSET
            })
        );
        // Make one less and it holds.
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[out(99, 0, 1)],
                &creds(1),
                &fee(1),
                0
            ),
            Ok(())
        );
    }

    #[test]
    fn burning_more_than_needed_is_allowed() {
        // Consuming more than is produced is not an error — the surplus is
        // burned. Only the other direction creates value.
        let u = utxo(1, 100, 0, 1);
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[out(1, 0, 1)],
                &creds(1),
                &no_fees(),
                0
            ),
            Ok(())
        );
    }

    #[test]
    fn one_credential_per_input() {
        let u = utxo(1, 100, 0, 1);
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[],
                &creds(0),
                &no_fees(),
                0
            ),
            Err(Error::WrongNumberOfCredentials {
                inputs: 1,
                credentials: 0
            })
        );
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[],
                &creds(2),
                &no_fees(),
                0
            ),
            Err(Error::WrongNumberOfCredentials {
                inputs: 1,
                credentials: 2
            })
        );
    }

    #[test]
    fn an_input_must_claim_the_asset_the_output_actually_is() {
        let u = utxo(1, 100, 0, 1);
        let mut i = spend(&u, 0);
        i.asset = [1; 32];
        assert_eq!(
            verify_spend(&[u], &[i], &[], &creds(1), &no_fees(), 0),
            Err(Error::AssetMismatch)
        );
    }

    #[test]
    fn a_locked_output_may_not_be_spent_as_ordinary_money() {
        // The whole point of the lock. Before the time, an input that does not
        // admit the lock is refused outright.
        let u = utxo(1, 100, 500, 1);
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[out(100, 500, 1)],
                &creds(1),
                &no_fees(),
                100
            ),
            Err(Error::LockedFundsNotMarkedAsLocked)
        );
    }

    #[test]
    fn an_input_may_not_claim_a_lock_time_the_output_does_not_have() {
        let u = utxo(1, 100, 500, 1);
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 999)],
                &[out(100, 500, 1)],
                &creds(1),
                &no_fees(),
                100
            ),
            Err(Error::LocktimeMismatch {
                claimed: 999,
                actual: 500
            })
        );
    }

    #[test]
    fn locked_value_stays_with_its_owner_and_its_time() {
        let u = utxo(1, 100, 500, 1);
        // Same owner, same time: fine.
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 500)],
                &[out(100, 500, 1)],
                &creds(1),
                &no_fees(),
                100
            ),
            Ok(())
        );
        // A different owner: the lock would be worthless if this passed.
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 500)],
                &[out(100, 500, 2)],
                &creds(1),
                &no_fees(),
                100
            ),
            Err(Error::InsufficientLockedFunds)
        );
        // A different time: an earlier unlock is value the lock did not grant.
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 500)],
                &[out(100, 400, 1)],
                &creds(1),
                &no_fees(),
                100
            ),
            Err(Error::InsufficientLockedFunds)
        );
    }

    #[test]
    fn locked_value_may_not_become_unlocked_value_before_its_time() {
        let u = utxo(1, 100, 500, 1);
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 500)],
                &[out(100, 0, 1)],
                &creds(1),
                &no_fees(),
                100
            ),
            Err(Error::InsufficientUnlockedFunds {
                needed: 100,
                asset: ASSET
            })
        );
    }

    #[test]
    fn unlocked_value_may_become_locked_value() {
        // Rule 3: you may always lock more of your own money.
        let unlocked = utxo(1, 100, 0, 1);
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&unlocked),
                &[spend(&unlocked, 0)],
                &[out(100, 500, 1)],
                &creds(1),
                &no_fees(),
                100
            ),
            Ok(())
        );
    }

    #[test]
    fn an_expired_lock_is_not_a_lock() {
        // This is how staked tokens return to circulation.
        let u = utxo(1, 100, 500, 1);
        assert_eq!(
            verify_spend(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[out(100, 0, 1)],
                &creds(1),
                &no_fees(),
                500
            ),
            Ok(())
        );
    }

    #[test]
    fn one_persons_lock_cannot_pay_for_anothers() {
        // Two locked inputs, two locked outputs, totals equal — but the owners
        // are crossed. A check that only compared totals would pass this.
        let a = utxo(1, 100, 500, 1);
        let b = utxo(2, 100, 500, 2);
        assert_eq!(
            verify_spend(
                &[a.clone(), b.clone()],
                &[spend(&a, 500), spend(&b, 500)],
                &[out(200, 500, 1)],
                &creds(2),
                &no_fees(),
                100
            ),
            Err(Error::InsufficientLockedFunds)
        );
    }

    #[test]
    fn a_signature_by_the_named_owner_authorises_the_spend() {
        let u = utxo_of(1, 100, 7);
        let sighash = [1u8; 32];
        assert_eq!(
            verify_credentials(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[credential(7, &sighash)],
                &sighash,
                0,
            ),
            Ok(())
        );
    }

    #[test]
    fn a_signature_by_someone_else_does_not() {
        let u = utxo_of(1, 100, 7);
        let sighash = [1u8; 32];
        assert_eq!(
            verify_credentials(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[credential(8, &sighash)],
                &sighash,
                0,
            ),
            Err(CredentialError::WrongSigner)
        );
    }

    /// A signature over a different transaction is not a signature over this
    /// one. Without this, one authorisation would authorise everything the
    /// same key ever signed.
    #[test]
    fn a_signature_over_another_transaction_does_not_authorise_this_one() {
        let u = utxo_of(1, 100, 7);
        assert_eq!(
            verify_credentials(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[credential(7, &[2u8; 32])],
                &[1u8; 32],
                0,
            ),
            Err(CredentialError::WrongSigner)
        );
    }

    #[test]
    fn bytes_that_are_not_a_signature_authorise_nothing() {
        let u = utxo_of(1, 100, 7);
        assert_eq!(
            verify_credentials(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[Credential {
                    sigs: vec![[0u8; 65]]
                }],
                &[1u8; 32],
                0,
            ),
            Err(CredentialError::MalformedSignature)
        );
    }

    #[test]
    fn the_threshold_must_be_met_exactly() {
        let sighash = [1u8; 32];
        let mut u = utxo_of(1, 100, 7);
        u.output.owners = Owners {
            locktime: 0,
            threshold: 2,
            addrs: vec![key_address(7), key_address(8)],
        };
        // One index for a threshold of two.
        assert_eq!(
            verify_credentials(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[credential(7, &sighash)],
                &sighash,
                0,
            ),
            Err(CredentialError::WrongNumberOfSignatures {
                given: 1,
                needed: 2
            })
        );

        // Both, in the order the indices name them.
        let mut both = spend(&u, 0);
        both.sig_indices = vec![0, 1];
        assert_eq!(
            verify_credentials(
                std::slice::from_ref(&u),
                &[both.clone()],
                &[Credential {
                    sigs: vec![
                        crate::sign::sign(&key(7), &sighash),
                        crate::sign::sign(&key(8), &sighash),
                    ]
                }],
                &sighash,
                0,
            ),
            Ok(())
        );

        // The same signature twice does not meet a threshold of two.
        assert_eq!(
            verify_credentials(
                std::slice::from_ref(&u),
                &[both],
                &[Credential {
                    sigs: vec![
                        crate::sign::sign(&key(7), &sighash),
                        crate::sign::sign(&key(7), &sighash),
                    ]
                }],
                &sighash,
                0,
            ),
            Err(CredentialError::WrongSigner)
        );
    }

    #[test]
    fn an_index_naming_no_address_is_refused() {
        let sighash = [1u8; 32];
        let u = utxo_of(1, 100, 7);
        let mut i = spend(&u, 0);
        i.sig_indices = vec![5];
        assert_eq!(
            verify_credentials(&[u], &[i], &[credential(7, &sighash)], &sighash, 0),
            Err(CredentialError::AddressIndexOutOfBounds)
        );
    }

    #[test]
    fn an_owners_own_locktime_blocks_the_spend_until_it_passes() {
        let sighash = [1u8; 32];
        let mut u = utxo_of(1, 100, 7);
        u.output.owners.locktime = 500;
        let cred = credential(7, &sighash);
        assert_eq!(
            verify_credentials(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                std::slice::from_ref(&cred),
                &sighash,
                499,
            ),
            Err(CredentialError::TimelockNotExpired)
        );
        assert_eq!(
            verify_credentials(
                std::slice::from_ref(&u),
                &[spend(&u, 0)],
                &[cred],
                &sighash,
                500,
            ),
            Ok(())
        );
    }

    // ---- Go's `TestVerifySpendUTXOs`, case for case ----
    //
    // Go checks the arithmetic and the signatures in one call
    // (`VerifySpendUTXOs`); here they are two, so the two cases in that table
    // that are about a credential rather than about value —"invalid
    // credential" and "invalid signature" — are the credential tests above,
    // where they live. Every other case is here, with the same inputs and the
    // same verdict.

    /// The instant Go's table is evaluated at.
    const GO_NOW: u64 = 1_607_133_207;

    /// An owner with nothing in it — the one every value in Go's table has,
    /// so every locked entry is tallied under the same owner.
    fn nobody() -> Owners {
        Owners {
            locktime: 0,
            threshold: 0,
            addrs: Vec::new(),
        }
    }

    fn go_utxo(n: u8, asset: Id, amount: u64, lock: u64) -> Utxo {
        Utxo {
            id: UtxoId {
                tx_id: [n; 32],
                output_index: 0,
            },
            output: Output {
                asset,
                stake_lock: lock,
                amount,
                owners: nobody(),
            },
        }
    }

    fn go_in(n: u8, asset: Id, amount: u64, lock: u64) -> Input {
        Input {
            utxo: UtxoId {
                tx_id: [n; 32],
                output_index: 0,
            },
            asset,
            stake_lock: lock,
            amount,
            sig_indices: Vec::new(),
        }
    }

    fn go_out(asset: Id, amount: u64, lock: u64) -> Output {
        Output {
            asset,
            stake_lock: lock,
            amount,
            owners: nobody(),
        }
    }

    fn owed(asset: Id, amount: u64) -> HashMap<Id, u64> {
        let mut m = HashMap::new();
        m.insert(asset, amount);
        m
    }

    /// The asset the chain's own fees are denominated in, and one that is not.
    const NATIVE: Id = [0xaa; 32];
    const CUSTOM: Id = [0xbb; 32];

    fn go_case(
        utxos: Vec<Utxo>,
        ins: Vec<Input>,
        outs: Vec<Output>,
        creds: usize,
        fees: HashMap<Id, u64>,
    ) -> Result<(), Error> {
        verify_spend(&utxos, &ins, &outs, &creds_n(creds), &fees, GO_NOW)
    }

    fn creds_n(n: usize) -> Vec<Credential> {
        vec![Credential::default(); n]
    }

    #[test]
    fn go_nothing_in_nothing_out_no_fee() {
        assert_eq!(go_case(vec![], vec![], vec![], 0, HashMap::new()), Ok(()));
    }

    #[test]
    fn go_nothing_in_nothing_out_positive_fee() {
        assert_eq!(
            go_case(vec![], vec![], vec![], 0, owed(NATIVE, 1)),
            Err(Error::InsufficientUnlockedFunds {
                needed: 1,
                asset: NATIVE
            })
        );
    }

    #[test]
    fn go_the_output_named_is_not_the_asset_claimed() {
        // The unspent output is one asset and the input says another.
        assert_eq!(
            go_case(
                vec![go_utxo(1, CUSTOM, 1, 0)],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![],
                1,
                HashMap::new()
            ),
            Err(Error::AssetMismatch)
        );
        // And the other way around.
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, 0)],
                vec![go_in(1, CUSTOM, 1, 0)],
                vec![],
                1,
                HashMap::new()
            ),
            Err(Error::AssetMismatch)
        );
    }

    #[test]
    fn go_one_asset_in_another_out() {
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, 0)],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![go_out(CUSTOM, 1, 0)],
                1,
                HashMap::new()
            ),
            Err(Error::InsufficientUnlockedFunds {
                needed: 1,
                asset: CUSTOM
            })
        );
    }

    #[test]
    fn go_a_locked_output_may_not_be_spent_as_unlocked() {
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, GO_NOW + 1)],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![],
                1,
                HashMap::new()
            ),
            Err(Error::LockedFundsNotMarkedAsLocked)
        );
    }

    #[test]
    fn go_an_input_may_not_restate_the_locktime() {
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, GO_NOW + 1)],
                vec![go_in(1, NATIVE, 1, GO_NOW)],
                vec![],
                1,
                HashMap::new()
            ),
            Err(Error::LocktimeMismatch {
                claimed: GO_NOW,
                actual: GO_NOW + 1
            })
        );
    }

    #[test]
    fn go_one_input_no_outputs_positive_fee() {
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, 0)],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![],
                1,
                owed(NATIVE, 1)
            ),
            Ok(())
        );
    }

    #[test]
    fn go_one_credential_per_input_and_one_output_per_input() {
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, 0)],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![],
                0,
                owed(NATIVE, 1)
            ),
            Err(Error::WrongNumberOfCredentials {
                inputs: 1,
                credentials: 0
            })
        );
        assert_eq!(
            go_case(
                vec![],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![],
                1,
                owed(NATIVE, 1)
            ),
            Err(Error::WrongNumberOfUtxos {
                inputs: 1,
                utxos: 0
            })
        );
    }

    #[test]
    fn go_locked_in_nothing_out_no_fee() {
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, GO_NOW + 1)],
                vec![go_in(1, NATIVE, 1, GO_NOW + 1)],
                vec![],
                1,
                HashMap::new()
            ),
            Ok(())
        );
    }

    #[test]
    fn go_locked_value_does_not_pay_a_fee() {
        // The fee is unlocked value, and this transaction consumes none.
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, GO_NOW + 1)],
                vec![go_in(1, NATIVE, 1, GO_NOW + 1)],
                vec![],
                1,
                owed(NATIVE, 1)
            ),
            Err(Error::InsufficientUnlockedFunds {
                needed: 1,
                asset: NATIVE
            })
        );
    }

    #[test]
    fn go_one_locked_and_one_unlocked_in_one_locked_out_with_a_fee() {
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, GO_NOW + 1), go_utxo(2, NATIVE, 1, 0)],
                vec![go_in(1, NATIVE, 1, GO_NOW + 1), go_in(2, NATIVE, 1, 0)],
                vec![go_out(NATIVE, 1, GO_NOW + 1)],
                2,
                owed(NATIVE, 1)
            ),
            Ok(())
        );
    }

    #[test]
    fn go_unlocked_value_may_be_locked_alongside_a_fee() {
        // Two consumed, one locked; the output locks two — the extra one comes
        // out of the unlocked input, and the fee out of what remains.
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, GO_NOW + 1), go_utxo(2, NATIVE, 2, 0)],
                vec![go_in(1, NATIVE, 1, GO_NOW + 1), go_in(2, NATIVE, 2, 0)],
                vec![go_out(NATIVE, 2, GO_NOW + 1)],
                2,
                owed(NATIVE, 1)
            ),
            Ok(())
        );
    }

    #[test]
    fn go_an_expired_lock_spends_as_unlocked() {
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, GO_NOW - 1)],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![go_out(NATIVE, 1, 0)],
                1,
                HashMap::new()
            ),
            Ok(())
        );
        // The same, for an asset that is not the chain's own.
        assert_eq!(
            go_case(
                vec![go_utxo(1, CUSTOM, 1, GO_NOW - 1)],
                vec![go_in(1, CUSTOM, 1, 0)],
                vec![go_out(CUSTOM, 1, 0)],
                1,
                HashMap::new()
            ),
            Ok(())
        );
    }

    #[test]
    fn go_a_sum_that_does_not_fit_is_not_a_sum() {
        // Unlocked.
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, 0)],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![go_out(NATIVE, 2, 0), go_out(NATIVE, u64::MAX, 0)],
                1,
                HashMap::new()
            ),
            Err(Error::Overflow)
        );
        // And locked, under one owner at one time.
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, 0)],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![go_out(NATIVE, 2, 1), go_out(NATIVE, u64::MAX, 1)],
                1,
                HashMap::new()
            ),
            Err(Error::Overflow)
        );
    }

    #[test]
    fn go_locking_is_not_a_way_to_mint() {
        // One consumed, two locked out.
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, 0)],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![go_out(NATIVE, 2, 1)],
                1,
                HashMap::new()
            ),
            Err(Error::InsufficientLockedFunds)
        );
        // Nor by mixing, in either order.
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, 0)],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![go_out(NATIVE, 2, 0), go_out(NATIVE, u64::MAX, 1)],
                1,
                HashMap::new()
            ),
            Err(Error::InsufficientLockedFunds)
        );
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, 0)],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![go_out(NATIVE, u64::MAX, 0), go_out(NATIVE, 2, 1)],
                1,
                HashMap::new()
            ),
            Err(Error::InsufficientLockedFunds)
        );
    }

    #[test]
    fn go_an_asset_that_is_not_the_chains_own_moves_and_locks() {
        assert_eq!(
            go_case(
                vec![go_utxo(1, CUSTOM, 1, 0)],
                vec![go_in(1, CUSTOM, 1, 0)],
                vec![go_out(CUSTOM, 1, 0)],
                1,
                HashMap::new()
            ),
            Ok(())
        );
        assert_eq!(
            go_case(
                vec![go_utxo(1, CUSTOM, 1, 0)],
                vec![go_in(1, CUSTOM, 1, 0)],
                vec![go_out(CUSTOM, 1, GO_NOW + 1)],
                1,
                HashMap::new()
            ),
            Ok(())
        );
    }

    #[test]
    fn go_one_asset_is_not_another() {
        // Consuming one asset does not pay for producing another.
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, 0)],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![go_out(CUSTOM, 1, 0)],
                1,
                HashMap::new()
            ),
            Err(Error::InsufficientUnlockedFunds {
                needed: 1,
                asset: CUSTOM
            })
        );
        // Nor does burning one pay a fee denominated in another.
        assert_eq!(
            go_case(
                vec![go_utxo(1, CUSTOM, 1, 0)],
                vec![go_in(1, CUSTOM, 1, 0)],
                vec![],
                1,
                owed(NATIVE, 1)
            ),
            Err(Error::InsufficientUnlockedFunds {
                needed: 1,
                asset: NATIVE
            })
        );
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, 0)],
                vec![go_in(1, NATIVE, 1, 0)],
                vec![],
                1,
                owed(CUSTOM, 1)
            ),
            Err(Error::InsufficientUnlockedFunds {
                needed: 1,
                asset: CUSTOM
            })
        );
    }

    #[test]
    fn go_two_assets_are_tallied_apart() {
        // One asset pays the fee, the other passes through.
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, 0), go_utxo(2, CUSTOM, 1, 0)],
                vec![go_in(1, NATIVE, 1, 0), go_in(2, CUSTOM, 1, 0)],
                vec![go_out(CUSTOM, 1, 0)],
                2,
                owed(NATIVE, 1)
            ),
            Ok(())
        );
        // A fee in each.
        let mut both = HashMap::new();
        both.insert(NATIVE, 1);
        both.insert(CUSTOM, 1);
        assert_eq!(
            go_case(
                vec![go_utxo(1, NATIVE, 1, 0), go_utxo(2, CUSTOM, 1, 0)],
                vec![go_in(1, NATIVE, 1, 0), go_in(2, CUSTOM, 1, 0)],
                vec![],
                2,
                both
            ),
            Ok(())
        );
        // A fee paid in the asset it is denominated in.
        assert_eq!(
            go_case(
                vec![go_utxo(1, CUSTOM, 1, 0)],
                vec![go_in(1, CUSTOM, 1, 0)],
                vec![],
                1,
                owed(CUSTOM, 1)
            ),
            Ok(())
        );
    }
}
