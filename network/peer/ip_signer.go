// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"net/netip"
	"sync"
	"time"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/timer/mockable"
	"github.com/luxfi/utils"
)

const (
	// ipResignInterval controls how often we re-sign our IP to keep the
	// signature fresh. Must be less than the reasonableClockSkewWindow in
	// ip.go (10 minutes) to ensure peers always accept our signature.
	ipResignInterval = 5 * time.Minute
)

// IPSigner will return a signedIP for the current value of our dynamic IP.
//
// Carries up to three signing legs: TLS (classical, staker cert),
// BLS12-381 (classical, proof-of-possession), and ML-DSA-65 (FIPS 204
// post-quantum). The ML-DSA leg is optional at construction time —
// passing mldsaSigner == nil to NewIPSigner produces classical-only
// gossip that strict-PQ verifiers refuse. Strict-PQ chains MUST be wired
// with a real ML-DSA-65 private key.
type IPSigner struct {
	ip          *utils.Atomic[netip.AddrPort]
	clock       mockable.Clock
	tlsSigner   crypto.Signer
	blsSigner   bls.Signer
	mldsaSigner *mldsa.PrivateKey

	// Must be held while accessing [signedIP]
	signedIPLock sync.RWMutex
	// Note that the values in [*signedIP] are constants and can be inspected
	// without holding [signedIPLock].
	signedIP *SignedIP
}

// NewIPSigner constructs a classical-only IPSigner (TLS + BLS legs). Use
// NewIPSignerPQ when the node has an ML-DSA-65 identity key and the chain
// runs under a strict-PQ profile.
func NewIPSigner(
	ip *utils.Atomic[netip.AddrPort],
	tlsSigner crypto.Signer,
	blsSigner bls.Signer,
) *IPSigner {
	return NewIPSignerPQ(ip, tlsSigner, blsSigner, nil)
}

// NewIPSignerPQ constructs an IPSigner that signs every refresh with all
// three legs the caller supplies. mldsaSigner may be nil to keep the
// classical-only behaviour (for permissive chains or for tests that have
// not yet generated a long-term ML-DSA key).
func NewIPSignerPQ(
	ip *utils.Atomic[netip.AddrPort],
	tlsSigner crypto.Signer,
	blsSigner bls.Signer,
	mldsaSigner *mldsa.PrivateKey,
) *IPSigner {
	return &IPSigner{
		ip:          ip,
		tlsSigner:   tlsSigner,
		blsSigner:   blsSigner,
		mldsaSigner: mldsaSigner,
	}
}

// GetSignedIP returns the signedIP of the current value of the provided
// dynamicIP. If the dynamicIP hasn't changed and the signature is still fresh,
// then the same [SignedIP] will be returned. The signature is refreshed
// periodically to ensure it stays within the verification window.
//
// It's safe for multiple goroutines to concurrently call GetSignedIP.
func (s *IPSigner) GetSignedIP() (*SignedIP, error) {
	// Optimistically, the IP should already be signed. By grabbing a read lock
	// here we enable full concurrency of new connections.
	s.signedIPLock.RLock()
	signedIP := s.signedIP
	s.signedIPLock.RUnlock()
	ip := s.ip.Get()
	if signedIP != nil && signedIP.AddrPort == ip && s.isFresh(signedIP) {
		return signedIP, nil
	}

	// If our current IP hasn't been signed yet or the signature is stale,
	// we should (re-)sign it.
	s.signedIPLock.Lock()
	defer s.signedIPLock.Unlock()

	// It's possible that multiple threads read [n.signedIP] as incorrect at the
	// same time, we should verify that we are the first thread to attempt to
	// update it.
	signedIP = s.signedIP
	if signedIP != nil && signedIP.AddrPort == ip && s.isFresh(signedIP) {
		return signedIP, nil
	}

	// We should now sign our new IP at the current timestamp.
	unsignedIP := UnsignedIP{
		AddrPort:  ip,
		Timestamp: s.clock.Unix(),
	}
	signedIP, err := unsignedIP.SignPQ(s.tlsSigner, s.blsSigner, s.mldsaSigner)
	if err != nil {
		return nil, err
	}

	s.signedIP = signedIP
	return s.signedIP, nil
}

// isFresh returns true if the signed IP's timestamp is recent enough that
// peers will accept it within the reasonableClockSkewWindow.
func (s *IPSigner) isFresh(ip *SignedIP) bool {
	now := s.clock.Unix()
	return now-ip.Timestamp < uint64(ipResignInterval.Seconds())
}

// PublicKey returns the BLS public key for this IPSigner
func (s *IPSigner) PublicKey() *bls.PublicKey {
	if s.blsSigner == nil {
		return nil
	}
	return s.blsSigner.PublicKey()
}
