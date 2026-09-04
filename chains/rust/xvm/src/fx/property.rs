// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The property feature extension.
//!
//! A property is ownership with no quantity and no payload: a mint authority
//! that can hand out owned outputs, and an owned output that can only be
//! burned. Everything is bare owner sets; the shape byte is what tells the
//! three of them apart.

use crate::error::Result;
use crate::wire::{self, shapes, TypeKind};

use super::secp256k1::{Credential as SecpCredential, Input, Owners};

/// The family byte every propertyfx primitive travels under.
pub const TYPE_KIND: TypeKind = TypeKind::Property;

/// Who may mint this property.
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
        let v = shapes::wrap_mint_output(b)?;
        if v.type_kind() != TYPE_KIND {
            return Err(wire::Error::WrongTypeKind.into());
        }
        Ok(MintOutput {
            owners: Owners {
                locktime: v.locktime(),
                threshold: v.threshold(),
                addrs: v.addresses().all(),
            },
        })
    }
}

/// One held property.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct OwnedOutput {
    pub owners: Owners,
}

impl OwnedOutput {
    pub fn verify(&self) -> Result<()> {
        self.owners.verify()
    }

    pub fn bytes(&self) -> Vec<u8> {
        shapes::new_owned_output(
            TYPE_KIND,
            self.owners.locktime,
            self.owners.threshold,
            &self.owners.addrs,
        )
    }

    pub fn from_envelope(b: &[u8]) -> Result<OwnedOutput> {
        let v = shapes::wrap_owned_output(b)?;
        if v.type_kind() != TYPE_KIND {
            return Err(wire::Error::WrongTypeKind.into());
        }
        Ok(OwnedOutput {
            owners: Owners {
                locktime: v.locktime(),
                threshold: v.threshold(),
                addrs: v.addresses().all(),
            },
        })
    }
}

/// Minting a property: the authority signs, and out come the continued
/// authority and one owned output.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct MintOperation {
    pub mint_input: Input,
    pub mint_output: MintOutput,
    pub owned_output: OwnedOutput,
}

impl MintOperation {
    pub fn cost(&self) -> Result<u64> {
        self.mint_input.cost()
    }

    pub fn verify(&self) -> Result<()> {
        self.mint_input.verify()?;
        self.mint_output.verify()?;
        self.owned_output.verify()
    }

    pub fn bytes(&self) -> Vec<u8> {
        shapes::new_mint_operation(
            TYPE_KIND,
            &self.mint_input.sig_indices,
            &self.mint_output.bytes(),
            &self.owned_output.bytes(),
        )
    }

    pub fn from_envelope(b: &[u8]) -> Result<MintOperation> {
        let v = shapes::wrap_mint_operation(b)?;
        if v.type_kind() != TYPE_KIND {
            return Err(wire::Error::WrongTypeKind.into());
        }
        Ok(MintOperation {
            mint_input: Input {
                sig_indices: v.sig_indices(),
            },
            mint_output: MintOutput::from_envelope(v.mint_output_bytes())?,
            owned_output: OwnedOutput::from_envelope(v.transfer_output_bytes())?,
        })
    }
}

/// Burning a property. It produces nothing — that is what burning is.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct BurnOperation {
    pub input: Input,
}

impl BurnOperation {
    pub fn cost(&self) -> Result<u64> {
        self.input.cost()
    }

    pub fn verify(&self) -> Result<()> {
        self.input.verify()
    }

    pub fn bytes(&self) -> Vec<u8> {
        shapes::new_burn_operation(TYPE_KIND, &self.input.sig_indices)
    }

    pub fn from_envelope(b: &[u8]) -> Result<BurnOperation> {
        let v = shapes::wrap_burn_operation(b)?;
        if v.type_kind() != TYPE_KIND {
            return Err(wire::Error::WrongTypeKind.into());
        }
        Ok(BurnOperation {
            input: Input {
                sig_indices: v.sig_indices(),
            },
        })
    }
}

/// A propertyfx credential: secp256k1 signatures under the property family
/// byte.
pub fn credential_bytes(c: &SecpCredential) -> Vec<u8> {
    c.bytes_for(TYPE_KIND)
}

pub fn credential_from_envelope(b: &[u8]) -> Result<SecpCredential> {
    SecpCredential::from_envelope_for(b, TYPE_KIND)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ids::ShortId;

    fn owners() -> Owners {
        Owners::new(1, vec![ShortId::prefixed_bytes(&[1])])
    }

    #[test]
    fn every_property_primitive_round_trips_through_its_envelope() {
        let m = MintOutput { owners: owners() };
        assert_eq!(MintOutput::from_envelope(&m.bytes()).unwrap(), m);

        let o = OwnedOutput { owners: owners() };
        assert_eq!(OwnedOutput::from_envelope(&o.bytes()).unwrap(), o);

        let op = MintOperation {
            mint_input: Input {
                sig_indices: vec![0],
            },
            mint_output: m,
            owned_output: o,
        };
        assert_eq!(MintOperation::from_envelope(&op.bytes()).unwrap(), op);

        let burn = BurnOperation {
            input: Input {
                sig_indices: vec![0, 1],
            },
        };
        assert_eq!(BurnOperation::from_envelope(&burn.bytes()).unwrap(), burn);
    }

    #[test]
    fn a_mint_output_and_an_owned_output_are_told_apart_by_the_shape_byte_alone() {
        let m = MintOutput { owners: owners() };
        let o = OwnedOutput { owners: owners() };
        // Same owner payload, different shape byte, and neither reads as the
        // other.
        assert_eq!(&m.bytes()[2..], &o.bytes()[2..]);
        assert_ne!(m.bytes()[1], o.bytes()[1]);
        assert!(MintOutput::from_envelope(&o.bytes()).is_err());
        assert!(OwnedOutput::from_envelope(&m.bytes()).is_err());
    }

    #[test]
    fn burning_produces_nothing() {
        let burn = BurnOperation {
            input: Input {
                sig_indices: vec![0],
            },
        };
        assert!(burn.verify().is_ok());
        assert_eq!(burn.cost().unwrap(), 1000);
    }
}
