// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The secp256k1 feature extension — the one that moves value.
//!
//! An output names a set of addresses and how many of them must sign. An input
//! names WHICH of them are signing, by index. A credential carries the
//! signatures. Verification recovers a public key from each signature, derives
//! its address, and checks it against the address the input pointed at — so an
//! output is spendable by exactly the people it names and no one else.

use crate::error::{Error, Result};
use crate::hash::{pubkey_bytes_to_address, sha256};
use crate::ids::{is_sorted_and_unique, ShortId};
use crate::wire::{self, shapes, TypeKind};

/// A secp256k1 signature: 64 bytes of (r,s) plus the recovery byte.
pub const SIGNATURE_LEN: usize = 65;

/// What one signature costs a transaction.
pub const COST_PER_SIGNATURE: u64 = 1000;

/// The family byte every secp256k1fx primitive travels under.
pub const TYPE_KIND: TypeKind = TypeKind::Secp256k1;

/// Who may spend an output, and how many of them it takes.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Owners {
    pub locktime: u64,
    pub threshold: u32,
    pub addrs: Vec<ShortId>,
}

impl Owners {
    pub fn new(threshold: u32, addrs: Vec<ShortId>) -> Self {
        Owners {
            locktime: 0,
            threshold,
            addrs,
        }
    }

    /// The addresses, as raw bytes — what an atomic element is indexed by.
    pub fn addresses(&self) -> Vec<Vec<u8>> {
        self.addrs.iter().map(|a| a.0.to_vec()).collect()
    }

    /// Whether two owner sets state the same condition.
    pub fn equals(&self, other: &Owners) -> bool {
        self.locktime == other.locktime
            && self.threshold == other.threshold
            && self.addrs == other.addrs
    }

    /// The three ways an owner set is malformed.
    ///
    /// A threshold above the address count can never be met. A threshold of
    /// zero with addresses listed is an unspendable-looking output that anyone
    /// could spend, so it is written the one canonical way instead. Unsorted or
    /// repeated addresses would give one owner set two encodings.
    pub fn verify(&self) -> Result<()> {
        if self.threshold > self.addrs.len() as u32 {
            return Err(Error::OutputUnspendable);
        }
        if self.threshold == 0 && !self.addrs.is_empty() {
            return Err(Error::OutputUnoptimized);
        }
        if !is_sorted_and_unique(&self.addrs) {
            return Err(Error::AddrsNotSortedUnique);
        }
        Ok(())
    }

    pub fn sort(&mut self) {
        self.addrs.sort();
    }

    pub fn bytes(&self) -> Vec<u8> {
        shapes::new_output_owners(self.locktime, self.threshold, &self.addrs)
    }

    pub fn from_envelope(b: &[u8]) -> Result<Owners> {
        let v = shapes::wrap_output_owners(b)?;
        Ok(Owners {
            locktime: v.locktime(),
            threshold: v.threshold(),
            addrs: shapes::addrs(v.addresses()),
        })
    }
}

/// An output holding value.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TransferOutput {
    pub amt: u64,
    pub owners: Owners,
}

impl TransferOutput {
    pub fn amount(&self) -> u64 {
        self.amt
    }

    pub fn verify(&self) -> Result<()> {
        if self.amt == 0 {
            return Err(Error::NoValueOutput);
        }
        self.owners.verify()
    }

    pub fn bytes(&self) -> Vec<u8> {
        shapes::new_transfer_output(
            TYPE_KIND,
            self.amt,
            self.owners.locktime,
            self.owners.threshold,
            &self.owners.addrs,
        )
    }

    pub fn from_envelope(b: &[u8]) -> Result<TransferOutput> {
        let (tk, v) = shapes::wrap_transfer_output(b)?;
        if tk != TYPE_KIND {
            return Err(wire::Error::WrongTypeKind.into());
        }
        Ok(TransferOutput {
            amt: v.amount(),
            owners: Owners {
                locktime: v.locktime(),
                threshold: v.threshold(),
                addrs: shapes::addrs(v.addresses()),
            },
        })
    }
}

/// An output that names who may MINT more of an asset, not who holds value.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct MintOutput {
    pub owners: Owners,
}

impl MintOutput {
    pub fn verify(&self) -> Result<()> {
        self.owners.verify()
    }

    pub fn bytes(&self) -> Vec<u8> {
        shapes::new_mint_output(
            TYPE_KIND,
            self.owners.locktime,
            self.owners.threshold,
            &self.owners.addrs,
        )
    }

    pub fn from_envelope(b: &[u8]) -> Result<MintOutput> {
        let (tk, v) = shapes::wrap_mint_output(b)?;
        if tk != TYPE_KIND {
            return Err(wire::Error::WrongTypeKind.into());
        }
        Ok(MintOutput {
            owners: Owners {
                locktime: v.locktime(),
                threshold: v.threshold(),
                addrs: shapes::addrs(v.addresses()),
            },
        })
    }
}

/// Which of an output's owners are signing, by position in its address list.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Input {
    pub sig_indices: Vec<u32>,
}

impl Input {
    pub fn cost(&self) -> Result<u64> {
        (self.sig_indices.len() as u64)
            .checked_mul(COST_PER_SIGNATURE)
            .ok_or(Error::Overflow)
    }

    pub fn verify(&self) -> Result<()> {
        if !is_sorted_and_unique(&self.sig_indices) {
            return Err(Error::InputIndicesNotSortedUnique);
        }
        Ok(())
    }
}

/// An input spending a value output.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TransferInput {
    pub amt: u64,
    pub input: Input,
}

impl TransferInput {
    pub fn amount(&self) -> u64 {
        self.amt
    }

    pub fn cost(&self) -> Result<u64> {
        self.input.cost()
    }

    pub fn verify(&self) -> Result<()> {
        if self.amt == 0 {
            return Err(Error::NoValueInput);
        }
        self.input.verify()
    }

    pub fn bytes(&self) -> Vec<u8> {
        shapes::new_transfer_input(TYPE_KIND, self.amt, &self.input.sig_indices)
    }

    pub fn from_envelope(b: &[u8]) -> Result<TransferInput> {
        let (tk, v) = shapes::wrap_transfer_input(b)?;
        if tk != TYPE_KIND {
            return Err(wire::Error::WrongTypeKind.into());
        }
        Ok(TransferInput {
            amt: v.amount(),
            input: Input {
                sig_indices: shapes::indices(v.sig_indices()),
            },
        })
    }
}

/// The signatures over a transaction's unsigned bytes.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Credential {
    pub sigs: Vec<[u8; SIGNATURE_LEN]>,
}

impl Credential {
    pub fn verify(&self) -> Result<()> {
        Ok(())
    }

    /// The signature run, concatenated — what the wire carries.
    pub fn sig_blob(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(self.sigs.len() * SIGNATURE_LEN);
        for s in &self.sigs {
            out.extend_from_slice(s);
        }
        out
    }

    pub fn bytes_for(&self, tk: TypeKind) -> Vec<u8> {
        shapes::new_credential(tk, 0, &self.sig_blob(), &[])
    }

    pub fn bytes(&self) -> Vec<u8> {
        self.bytes_for(TYPE_KIND)
    }

    pub fn from_envelope_for(b: &[u8], tk: TypeKind) -> Result<Credential> {
        let (got, v) = shapes::wrap_credential(b)?;
        if got != tk {
            return Err(wire::Error::WrongTypeKind.into());
        }
        let n = shapes::signature_count(&v, SIGNATURE_LEN);
        let mut sigs = Vec::with_capacity(n.min(4096));
        for i in 0..n {
            let raw = shapes::signature_at(&v, i, SIGNATURE_LEN).unwrap_or_default();
            let mut s = [0u8; SIGNATURE_LEN];
            s[..raw.len().min(SIGNATURE_LEN)].copy_from_slice(&raw[..raw.len().min(SIGNATURE_LEN)]);
            sigs.push(s);
        }
        Ok(Credential { sigs })
    }

    pub fn from_envelope(b: &[u8]) -> Result<Credential> {
        Credential::from_envelope_for(b, TYPE_KIND)
    }
}

/// Minting: an authority signs, and out come a fresh value output and the
/// continued authority to mint again.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct MintOperation {
    pub mint_input: Input,
    pub mint_output: MintOutput,
    pub transfer_output: TransferOutput,
}

impl MintOperation {
    pub fn cost(&self) -> Result<u64> {
        self.mint_input.cost()
    }

    pub fn verify(&self) -> Result<()> {
        self.mint_input.verify()?;
        self.mint_output.verify()?;
        self.transfer_output.verify()
    }

    pub fn bytes(&self) -> Vec<u8> {
        shapes::new_mint_operation(
            TYPE_KIND,
            &self.mint_input.sig_indices,
            &self.mint_output.bytes(),
            &self.transfer_output.bytes(),
        )
    }

    pub fn from_envelope(b: &[u8]) -> Result<MintOperation> {
        let (tk, v) = shapes::wrap_mint_operation(b)?;
        if tk != TYPE_KIND {
            return Err(wire::Error::WrongTypeKind.into());
        }
        Ok(MintOperation {
            mint_input: Input {
                sig_indices: shapes::indices(v.sig_indices()),
            },
            mint_output: MintOutput::from_envelope(v.mint_output())?,
            transfer_output: TransferOutput::from_envelope(v.transfer_output())?,
        })
    }
}

/// What an fx needs to know at verification time: what time it is, and whether
/// the node has caught up.
#[derive(Clone, Copy, Debug, Default)]
pub struct FxContext {
    /// Seconds since the epoch.
    pub now: u64,
    /// Signature checking is off while bootstrapping — those blocks were
    /// already decided by the network, and re-checking every signature in the
    /// chain's history would make a new node take forever to join.
    pub bootstrapped: bool,
}

/// The whole spending rule: an output can be spent by an input with a
/// credential, or it cannot, and this says which.
///
/// The order of the checks is the order they are cheapest in. Locktime first —
/// no signature work for an output that is not spendable yet. Then the two
/// threshold checks, stated separately so "too many" and "too few" are
/// different answers. Then the count against the credential. Only then does
/// anything touch a curve.
pub fn verify_credentials(
    ctx: &FxContext,
    unsigned_bytes: &[u8],
    input: &Input,
    cred: &Credential,
    out: &Owners,
) -> Result<()> {
    let num_sigs = input.sig_indices.len();
    if out.locktime > ctx.now {
        return Err(Error::Timelocked);
    }
    if out.threshold < num_sigs as u32 {
        return Err(Error::TooManySigners);
    }
    if out.threshold > num_sigs as u32 {
        return Err(Error::TooFewSigners);
    }
    if num_sigs != cred.sigs.len() {
        return Err(Error::InputCredentialSignersMismatch);
    }
    if !ctx.bootstrapped {
        return Ok(());
    }

    let tx_hash = sha256(unsigned_bytes);
    for (i, index) in input.sig_indices.iter().enumerate() {
        if *index as usize >= out.addrs.len() {
            return Err(Error::InputOutputIndexOutOfBounds);
        }
        let pk = recover_public_key(&tx_hash, &cred.sigs[i])?;
        let addr = ShortId(pubkey_bytes_to_address(&pk));
        let expected = out.addrs[*index as usize];
        if expected != addr {
            return Err(Error::WrongSig);
        }
    }
    Ok(())
}

/// Recover the compressed public key that made a signature over `hash`.
///
/// This is what makes an output spendable at all: the address in the output is
/// derived from a key nobody transmitted, so the signature has to produce it.
pub fn recover_public_key(hash: &[u8; 32], sig: &[u8; SIGNATURE_LEN]) -> Result<Vec<u8>> {
    use k256::ecdsa::{RecoveryId, Signature, VerifyingKey};

    let recid = RecoveryId::from_byte(sig[64]).ok_or(Error::UnrecoverableSignature)?;
    let signature = Signature::from_slice(&sig[..64]).map_err(|_| Error::UnrecoverableSignature)?;
    let vk = VerifyingKey::recover_from_prehash(hash, &signature, recid)
        .map_err(|_| Error::UnrecoverableSignature)?;
    Ok(vk.to_encoded_point(true).as_bytes().to_vec())
}

/// Sign a 32-byte hash, producing the 65-byte recoverable form.
///
/// Deterministic (RFC 6979), like the Go signer — so the same key over the same
/// transaction produces the same bytes in both languages, and a golden vector
/// means something.
pub fn sign_hash(secret: &[u8; 32], hash: &[u8; 32]) -> Result<[u8; SIGNATURE_LEN]> {
    use k256::ecdsa::SigningKey;

    let sk = SigningKey::from_slice(secret).map_err(|_| Error::UnrecoverableSignature)?;
    let (sig, recid) = sk
        .sign_prehash_recoverable(hash)
        .map_err(|_| Error::UnrecoverableSignature)?;
    let mut out = [0u8; SIGNATURE_LEN];
    out[..64].copy_from_slice(&sig.to_bytes());
    out[64] = recid.to_byte();
    Ok(out)
}

/// The address of a secret key: the address its public key derives to.
pub fn address_of(secret: &[u8; 32]) -> Result<ShortId> {
    use k256::ecdsa::SigningKey;
    let sk = SigningKey::from_slice(secret).map_err(|_| Error::UnrecoverableSignature)?;
    let pk = sk
        .verifying_key()
        .to_encoded_point(true)
        .as_bytes()
        .to_vec();
    Ok(ShortId(pubkey_bytes_to_address(&pk)))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn addr(n: u8) -> ShortId {
        ShortId::prefixed_bytes(&[n])
    }

    #[test]
    fn an_owner_set_is_refused_for_each_of_the_three_reasons() {
        let mut o = Owners::new(2, vec![addr(1)]);
        assert_eq!(o.verify().unwrap_err(), Error::OutputUnspendable);

        o = Owners::new(0, vec![addr(1)]);
        assert_eq!(o.verify().unwrap_err(), Error::OutputUnoptimized);

        o = Owners::new(2, vec![addr(2), addr(1)]);
        assert_eq!(o.verify().unwrap_err(), Error::AddrsNotSortedUnique);

        o = Owners::new(2, vec![addr(1), addr(1)]);
        assert_eq!(o.verify().unwrap_err(), Error::AddrsNotSortedUnique);

        // Threshold zero with NO addresses is the canonical unspendable form.
        assert!(Owners::new(0, vec![]).verify().is_ok());
        assert!(Owners::new(1, vec![addr(1)]).verify().is_ok());
    }

    #[test]
    fn an_output_with_no_value_is_not_an_output() {
        let out = TransferOutput {
            amt: 0,
            owners: Owners::new(1, vec![addr(1)]),
        };
        assert_eq!(out.verify().unwrap_err(), Error::NoValueOutput);
    }

    #[test]
    fn an_input_with_no_value_is_not_an_input() {
        let inp = TransferInput {
            amt: 0,
            input: Input {
                sig_indices: vec![0],
            },
        };
        assert_eq!(inp.verify().unwrap_err(), Error::NoValueInput);
    }

    #[test]
    fn signature_indices_must_be_sorted_and_unique() {
        let inp = Input {
            sig_indices: vec![1, 0],
        };
        assert_eq!(
            inp.verify().unwrap_err(),
            Error::InputIndicesNotSortedUnique
        );
        let inp = Input {
            sig_indices: vec![0, 0],
        };
        assert_eq!(
            inp.verify().unwrap_err(),
            Error::InputIndicesNotSortedUnique
        );
        assert!(Input {
            sig_indices: vec![0, 1]
        }
        .verify()
        .is_ok());
    }

    #[test]
    fn one_signature_costs_a_thousand() {
        assert_eq!(
            Input {
                sig_indices: vec![0, 1, 2]
            }
            .cost()
            .unwrap(),
            3000
        );
    }

    #[test]
    fn every_primitive_round_trips_through_its_envelope() {
        let owners = Owners {
            locktime: 7,
            threshold: 2,
            addrs: vec![addr(1), addr(2)],
        };
        assert_eq!(Owners::from_envelope(&owners.bytes()).unwrap(), owners);

        let out = TransferOutput {
            amt: 99,
            owners: owners.clone(),
        };
        assert_eq!(TransferOutput::from_envelope(&out.bytes()).unwrap(), out);

        let mint = MintOutput {
            owners: owners.clone(),
        };
        assert_eq!(MintOutput::from_envelope(&mint.bytes()).unwrap(), mint);

        let inp = TransferInput {
            amt: 5,
            input: Input {
                sig_indices: vec![0, 3],
            },
        };
        assert_eq!(TransferInput::from_envelope(&inp.bytes()).unwrap(), inp);

        let cred = Credential {
            sigs: vec![[1u8; 65], [2u8; 65]],
        };
        assert_eq!(Credential::from_envelope(&cred.bytes()).unwrap(), cred);

        let op = MintOperation {
            mint_input: Input {
                sig_indices: vec![0],
            },
            mint_output: mint,
            transfer_output: out,
        };
        assert_eq!(MintOperation::from_envelope(&op.bytes()).unwrap(), op);
    }

    #[test]
    fn a_signature_recovers_the_address_that_made_it() {
        let secret = [7u8; 32];
        let hash = sha256(b"the bytes that were signed");
        let sig = sign_hash(&secret, &hash).unwrap();
        let pk = recover_public_key(&hash, &sig).unwrap();
        assert_eq!(
            ShortId(pubkey_bytes_to_address(&pk)),
            address_of(&secret).unwrap()
        );
    }

    #[test]
    fn signing_is_deterministic_so_a_golden_vector_means_something() {
        let secret = [7u8; 32];
        let hash = sha256(b"x");
        assert_eq!(
            sign_hash(&secret, &hash).unwrap(),
            sign_hash(&secret, &hash).unwrap()
        );
    }

    fn ctx() -> FxContext {
        FxContext {
            now: 1000,
            bootstrapped: true,
        }
    }

    #[test]
    fn a_locked_output_cannot_be_spent_before_its_time() {
        let owners = Owners {
            locktime: 2000,
            threshold: 1,
            addrs: vec![addr(1)],
        };
        let err = verify_credentials(
            &ctx(),
            b"tx",
            &Input {
                sig_indices: vec![0],
            },
            &Credential {
                sigs: vec![[0u8; 65]],
            },
            &owners,
        )
        .unwrap_err();
        assert_eq!(err, Error::Timelocked);
    }

    #[test]
    fn too_many_and_too_few_signers_are_different_answers() {
        let owners = Owners::new(1, vec![addr(1)]);
        assert_eq!(
            verify_credentials(
                &ctx(),
                b"tx",
                &Input {
                    sig_indices: vec![0, 1]
                },
                &Credential {
                    sigs: vec![[0u8; 65]; 2]
                },
                &owners,
            )
            .unwrap_err(),
            Error::TooManySigners
        );
        assert_eq!(
            verify_credentials(
                &ctx(),
                b"tx",
                &Input {
                    sig_indices: vec![]
                },
                &Credential { sigs: vec![] },
                &owners,
            )
            .unwrap_err(),
            Error::TooFewSigners
        );
    }

    #[test]
    fn the_credential_must_carry_exactly_as_many_signatures_as_the_input_claims() {
        let owners = Owners::new(1, vec![addr(1)]);
        assert_eq!(
            verify_credentials(
                &ctx(),
                b"tx",
                &Input {
                    sig_indices: vec![0]
                },
                &Credential { sigs: vec![] },
                &owners,
            )
            .unwrap_err(),
            Error::InputCredentialSignersMismatch
        );
    }

    #[test]
    fn an_index_past_the_address_list_is_refused() {
        let owners = Owners::new(1, vec![addr(1)]);
        let secret = [3u8; 32];
        let sig = sign_hash(&secret, &sha256(b"tx")).unwrap();
        assert_eq!(
            verify_credentials(
                &ctx(),
                b"tx",
                &Input {
                    sig_indices: vec![5]
                },
                &Credential { sigs: vec![sig] },
                &owners,
            )
            .unwrap_err(),
            Error::InputOutputIndexOutOfBounds
        );
    }

    #[test]
    fn a_signature_from_the_wrong_key_is_refused_and_from_the_right_one_is_not() {
        let secret = [3u8; 32];
        let me = address_of(&secret).unwrap();
        let owners = Owners::new(1, vec![me]);
        let sig = sign_hash(&secret, &sha256(b"tx")).unwrap();
        assert!(verify_credentials(
            &ctx(),
            b"tx",
            &Input {
                sig_indices: vec![0]
            },
            &Credential { sigs: vec![sig] },
            &owners,
        )
        .is_ok());

        // The same signature over different bytes recovers a different key.
        assert_eq!(
            verify_credentials(
                &ctx(),
                b"other",
                &Input {
                    sig_indices: vec![0]
                },
                &Credential { sigs: vec![sig] },
                &owners,
            )
            .unwrap_err(),
            Error::WrongSig
        );
    }

    #[test]
    fn signature_checking_is_off_until_the_node_has_caught_up() {
        let owners = Owners::new(1, vec![addr(1)]);
        let ctx = FxContext {
            now: 1000,
            bootstrapped: false,
        };
        // A garbage signature passes while bootstrapping — those blocks were
        // already decided.
        assert!(verify_credentials(
            &ctx,
            b"tx",
            &Input {
                sig_indices: vec![0]
            },
            &Credential {
                sigs: vec![[0u8; 65]]
            },
            &owners,
        )
        .is_ok());
    }
}
