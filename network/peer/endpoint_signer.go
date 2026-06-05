// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"crypto/rand"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/net/endpoints"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/utils/wrappers"
	"github.com/luxfi/utils"
)

var (
	errInvalidEndpointSignature = errors.New("invalid endpoint signature")
)

// UnsignedEndpoint is used for a validator to claim an endpoint (IP or hostname).
// The [Timestamp] ensures peers track the most updated endpoint claim.
type UnsignedEndpoint struct {
	Endpoint  endpoints.Endpoint
	Timestamp uint64
}

// Sign this endpoint with the provided signers and return the signed endpoint.
func (e *UnsignedEndpoint) Sign(tlsSigner crypto.Signer, blsSigner bls.Signer) (*SignedEndpoint, error) {
	endpointBytes := e.bytes()
	tlsSignature, err := tlsSigner.Sign(
		rand.Reader,
		hash.ComputeHash256(endpointBytes),
		crypto.SHA256,
	)
	if err != nil {
		return nil, err
	}

	blsSignature, err := blsSigner.SignProofOfPossession(endpointBytes)
	if err != nil {
		return nil, err
	}

	return &SignedEndpoint{
		UnsignedEndpoint:  *e,
		TLSSignature:      tlsSignature,
		BLSSignature:      blsSignature,
		BLSSignatureBytes: bls.SignatureToBytes(blsSignature),
	}, nil
}

// bytes returns the canonical byte representation for signing.
func (e *UnsignedEndpoint) bytes() []byte {
	endpointBytes := e.Endpoint.Bytes()
	p := wrappers.Packer{
		Bytes: make([]byte, len(endpointBytes)+wrappers.LongLen),
	}
	p.PackFixedBytes(endpointBytes)
	p.PackLong(e.Timestamp)
	return p.Bytes
}

// SignedEndpoint is a wrapper of UnsignedEndpoint with signatures.
type SignedEndpoint struct {
	UnsignedEndpoint
	TLSSignature      []byte
	BLSSignature      *bls.Signature
	BLSSignatureBytes []byte
}

// Verify verifies that:
// - [e.Timestamp] is within the allowed clock skew range.
// - [e.TLSSignature] is a valid signature over [e.UnsignedEndpoint] from [cert].
func (e *SignedEndpoint) Verify(
	cert *staking.Certificate,
	maxTimestamp time.Time,
) error {
	maxUnixTimestamp := uint64(maxTimestamp.Unix())
	if e.Timestamp > maxUnixTimestamp {
		return fmt.Errorf("%w: timestamp %d > maxTimestamp %d",
			errTimestampTooFarInFuture, e.Timestamp, maxUnixTimestamp)
	}

	// Prevent replay attacks
	const reasonableClockSkewWindow = 10 * time.Minute
	minTimestamp := maxTimestamp.Add(-reasonableClockSkewWindow)
	minUnixTimestamp := uint64(minTimestamp.Unix())
	if e.Timestamp < minUnixTimestamp {
		return fmt.Errorf("%w: timestamp %d < minTimestamp %d",
			errTimestampTooFarInPast, e.Timestamp, minUnixTimestamp)
	}

	if err := staking.CheckSignature(
		cert,
		e.UnsignedEndpoint.bytes(),
		e.TLSSignature,
	); err != nil {
		return fmt.Errorf("%w: %w", errInvalidEndpointSignature, err)
	}
	return nil
}

// ToSignedIP converts to legacy SignedIP if this is an IP endpoint.
// Returns nil if this is a hostname endpoint.
func (e *SignedEndpoint) ToSignedIP() *SignedIP {
	if !e.Endpoint.IsIP() {
		return nil
	}
	return &SignedIP{
		UnsignedIP: UnsignedIP{
			AddrPort:  e.Endpoint.AddrPort,
			Timestamp: e.Timestamp,
		},
		TLSSignature:      e.TLSSignature,
		BLSSignature:      e.BLSSignature,
		BLSSignatureBytes: e.BLSSignatureBytes,
	}
}

// EndpointSigner periodically signs the node's endpoint claim.
// Supports both IP and hostname endpoints.
type EndpointSigner struct {
	endpoint  *utils.Atomic[endpoints.Endpoint]
	tlsSigner crypto.Signer
	blsSigner bls.Signer

	// For backward compatibility with IP-only peers
	legacyIPSigner *IPSigner
}

// NewEndpointSigner returns an EndpointSigner that can sign endpoint claims.
// If the endpoint is IP-based, it also maintains a legacy IPSigner for compatibility.
func NewEndpointSigner(
	endpoint *utils.Atomic[endpoints.Endpoint],
	tlsSigner crypto.Signer,
	blsSigner bls.Signer,
) *EndpointSigner {
	es := &EndpointSigner{
		endpoint:  endpoint,
		tlsSigner: tlsSigner,
		blsSigner: blsSigner,
	}

	// If the endpoint is IP-based, create legacy signer for backward compatibility
	currentEndpoint := endpoint.Get()
	if currentEndpoint.IsIP() {
		ipAtomic := utils.NewAtomic(currentEndpoint.AddrPort)
		es.legacyIPSigner = NewIPSigner(ipAtomic, tlsSigner, blsSigner)
	}

	return es
}

// GetSignedEndpoint returns a signed endpoint claim with the current timestamp.
func (s *EndpointSigner) GetSignedEndpoint() (*SignedEndpoint, error) {
	unsigned := &UnsignedEndpoint{
		Endpoint:  s.endpoint.Get(),
		Timestamp: uint64(time.Now().Unix()),
	}
	return unsigned.Sign(s.tlsSigner, s.blsSigner)
}

// GetSignedIP returns a legacy signed IP for backward compatibility.
// Returns nil if the endpoint is hostname-based.
func (s *EndpointSigner) GetSignedIP() (*SignedIP, error) {
	if s.legacyIPSigner == nil {
		return nil, nil
	}
	return s.legacyIPSigner.GetSignedIP()
}

// SupportsHostname returns true if this signer is using a hostname endpoint.
func (s *EndpointSigner) SupportsHostname() bool {
	return s.endpoint.Get().IsHostname()
}

// IPEndpointFromAddrPort creates an IP endpoint from netip.AddrPort for convenience.
func IPEndpointFromAddrPort(addr netip.AddrPort) endpoints.Endpoint {
	return endpoints.NewIPEndpoint(addr)
}
