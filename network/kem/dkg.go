// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kem

import (
	"errors"
	"fmt"
)

// DKGChannelScheme is the only KeyExchangeID admissible on a validator-to-
// validator DKG channel. Pinned at KeyExchangeMLKEM1024 (NIST PQ Cat 5) so
// the Pulsar / Pulsar-M Pedersen DKG over R_q runs under the strongest
// FIPS 203 parameter set available.
//
// Anyone constructing a DKG session in pulsar / pulsar-m MUST go through
// NewDKGKEMSession (or assert this constant explicitly) so a future
// refactor that silently flips the byte to ML-KEM-768 fails the strict-PQ
// profile gate rather than silently downgrading.
const DKGChannelScheme = KeyExchangeMLKEM1024

// ErrDKGSchemeMismatch is returned by NewDKGKEMSession when the caller
// supplies a scheme other than DKGChannelScheme. A strict-PQ profile that
// declares HighValueKEM=ML-KEM-1024 but observes a DKG attempt under
// ML-KEM-768 MUST surface this error to the operator — the scheme
// mismatch is a configuration drift, not a tactical preference.
var ErrDKGSchemeMismatch = errors.New("kem: DKG channels MUST use ML-KEM-1024")

// InitiateDKGKEMSession is the DKG-channel-flavoured wrapper around
// InitiateKEMSession. The scheme axis is fixed at ML-KEM-1024; supplying
// any other scheme returns ErrDKGSchemeMismatch before any KEM work
// runs.
//
// The returned KEMSession is otherwise byte-identical to what
// InitiateKEMSession(MLKEM1024, ...) returns; downstream code may call
// DeriveDKGAEADKey on it to bind the AEAD key under the "NODE_DKG_V1"
// customisation.
func InitiateDKGKEMSession(
	scheme KeyExchangeID,
	peerKEMPub []byte,
	transcript []byte,
) (*KEMSession, []byte, error) {
	if scheme != DKGChannelScheme {
		return nil, nil, fmt.Errorf("%w: got=%s", ErrDKGSchemeMismatch, scheme)
	}
	return InitiateKEMSession(scheme, peerKEMPub, transcript)
}

// RespondDKGKEMSession is the responder-side analogue of
// InitiateDKGKEMSession. Same scheme-pin rule applies.
func RespondDKGKEMSession(
	scheme KeyExchangeID,
	ourKEMSec []byte,
	peerCiphertext []byte,
	transcript []byte,
) (*KEMSession, error) {
	if scheme != DKGChannelScheme {
		return nil, fmt.Errorf("%w: got=%s", ErrDKGSchemeMismatch, scheme)
	}
	return RespondKEMSession(scheme, ourKEMSec, peerCiphertext, transcript)
}

// AssertDKGCompliance reports nil iff sessionScheme and profileHighValueKEM
// are both ML-KEM-1024. The strict-PQ profile cross-axis check: a profile
// that declares HighValueKEM=ML-KEM-1024 MUST observe ML-KEM-1024 on the
// wire; anything else is a configuration drift that audit tooling rejects
// at boot.
//
// This is the single dispatch point for the "DKG channel scheme matches
// profile high-value KEM" predicate; pulsar / pulsar-m callers route
// through it instead of comparing the bytes locally.
func AssertDKGCompliance(sessionScheme, profileHighValueKEM KeyExchangeID) error {
	if profileHighValueKEM != DKGChannelScheme {
		return fmt.Errorf("%w: profile HighValueKEM=%s, expected %s",
			ErrDKGSchemeMismatch, profileHighValueKEM, DKGChannelScheme)
	}
	if sessionScheme != DKGChannelScheme {
		return fmt.Errorf("%w: session scheme=%s, expected %s",
			ErrDKGSchemeMismatch, sessionScheme, DKGChannelScheme)
	}
	return nil
}
