// SPDX-License-Identifier: BSD-3-Clause-Eco

//! On what terms a network's validator set is admitted.
//!
//! Go states this as `vms/platformvm/security`. It is two independent axes and
//! nothing else:
//!
//! - whether the network restakes its parent's set, and
//! - whether it runs a set of its own — and if so, who admits to it and who
//!   manages it.
//!
//! Keeping them apart is what lets one transaction shape describe every level
//! of the hierarchy. A network that restakes its parent and runs no set of its
//! own is an L2; one that runs its own permissionless set is an L1; a network
//! can do both. The one thing it cannot do is neither, because a network whose
//! blocks nothing secures is not a network — that is [`Error::NoSecurity`].
//!
//! This is the sibling of [`crate::stakingparams`]: this module says how a set
//! is ADMITTED, that one says on what TERMS.

/// Who may join a network's own validator set.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u8)]
pub enum Admission {
    /// No set of its own: security is entirely restaked from the parent.
    NoOwnSet = 0,
    /// Permissionless. Anyone meeting [`Mode::threshold`] joins.
    Open = 1,
    /// Membership is admitted by the manager.
    Gated = 2,
}

/// Who manages the set.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[repr(u8)]
pub enum Manager {
    /// P-Chain transactions, authorised by the network's owner.
    PChain = 0,
    /// A staking contract on some chain.
    Contract = 1,
}

/// The two axes together.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Mode {
    pub restake_parent: bool,
    pub admission: Admission,
    /// The minimum stake for [`Admission::Open`]; zero otherwise.
    pub threshold: u64,
    pub manager: Manager,
}

/// Why a mode is not one.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    UnknownAdmission(u8),
    UnknownManager(u8),
    /// Neither axis is set: nothing would secure the network's blocks.
    NoSecurity,
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::UnknownAdmission(v) => write!(f, "security: unknown admission {v}"),
            Error::UnknownManager(v) => write!(f, "security: unknown manager {v}"),
            Error::NoSecurity => write!(
                f,
                "security: network must restake parent or run its own set"
            ),
        }
    }
}

impl std::error::Error for Error {}

impl Admission {
    /// The wire value, or the value itself when it names nothing.
    ///
    /// An unknown byte is carried rather than rejected here so the check lives
    /// in exactly one place — [`Mode::valid`] — as it does in Go, where the
    /// field is a bare `uint8` the constructor never validates.
    pub fn from_u8(v: u8) -> Result<Admission, u8> {
        Ok(match v {
            0 => Admission::NoOwnSet,
            1 => Admission::Open,
            2 => Admission::Gated,
            other => return Err(other),
        })
    }
}

impl Manager {
    pub fn from_u8(v: u8) -> Result<Manager, u8> {
        Ok(match v {
            0 => Manager::PChain,
            1 => Manager::Contract,
            other => return Err(other),
        })
    }
}

impl Mode {
    /// Whether the network runs a validator set of its own.
    pub fn sovereign(&self) -> bool {
        self.admission != Admission::NoOwnSet
    }

    /// Whether the two axes are a coherent pair.
    pub fn valid(&self) -> Result<(), Error> {
        if !self.restake_parent && self.admission == Admission::NoOwnSet {
            return Err(Error::NoSecurity);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn mode(restake: bool, admission: Admission) -> Mode {
        Mode {
            restake_parent: restake,
            admission,
            threshold: 0,
            manager: Manager::PChain,
        }
    }

    #[test]
    fn a_network_secured_by_nothing_is_refused() {
        assert_eq!(
            mode(false, Admission::NoOwnSet).valid(),
            Err(Error::NoSecurity)
        );
    }

    #[test]
    fn either_axis_alone_is_enough() {
        assert_eq!(mode(true, Admission::NoOwnSet).valid(), Ok(()));
        assert_eq!(mode(false, Admission::Open).valid(), Ok(()));
        assert_eq!(mode(true, Admission::Gated).valid(), Ok(()));
    }

    #[test]
    fn running_a_set_of_its_own_is_what_sovereign_means() {
        assert!(!mode(true, Admission::NoOwnSet).sovereign());
        assert!(mode(false, Admission::Open).sovereign());
        assert!(mode(false, Admission::Gated).sovereign());
    }

    #[test]
    fn the_wire_numbers_are_the_ones_go_writes() {
        assert_eq!(Admission::NoOwnSet as u8, 0);
        assert_eq!(Admission::Open as u8, 1);
        assert_eq!(Admission::Gated as u8, 2);
        assert_eq!(Manager::PChain as u8, 0);
        assert_eq!(Manager::Contract as u8, 1);
        assert_eq!(Admission::from_u8(3), Err(3));
        assert_eq!(Manager::from_u8(2), Err(2));
    }
}
