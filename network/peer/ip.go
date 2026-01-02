// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids/utils/wrappers"
	"github.com/luxfi/node/staking"
)

var (
	errTimestampTooFarInFuture = errors.New("timestamp too far in the future")
	errTimestampTooFarInPast   = errors.New("timestamp too far in the past")
	errInvalidTLSSignature     = errors.New("invalid TLS signature")
)

// UnsignedIP is used for a validator to claim an IP. The [Timestamp] is used to
// ensure that the most updated IP claim is tracked by peers for a given
// validator.
type UnsignedIP struct {
	AddrPort  netip.AddrPort
	Timestamp uint64
}

// Sign this IP with the provided signer and return the signed IP.
func (ip *UnsignedIP) Sign(tlsSigner crypto.Signer, blsSigner bls.Signer) (*SignedIP, error) {
	ipBytes := ip.bytes()
	tlsSignature, err := tlsSigner.Sign(
		rand.Reader,
		hash.ComputeHash256(ipBytes),
		crypto.SHA256,
	)
	if err != nil {
		return nil, err
	}

	blsSignature, err := blsSigner.SignProofOfPossession(ipBytes)
	if err != nil {
		return nil, err
	}

	return &SignedIP{
		UnsignedIP:        *ip,
		TLSSignature:      tlsSignature,
		BLSSignature:      blsSignature,
		BLSSignatureBytes: bls.SignatureToBytes(blsSignature),
	}, nil
}

func (ip *UnsignedIP) bytes() []byte {
	p := wrappers.Packer{
		Bytes: make([]byte, net.IPv6len+wrappers.ShortLen+wrappers.LongLen),
	}
	addrBytes := ip.AddrPort.Addr().As16()
	p.PackFixedBytes(addrBytes[:])
	p.PackShort(ip.AddrPort.Port())
	p.PackLong(ip.Timestamp)
	return p.Bytes
}

// SignedIP is a wrapper of an UnsignedIP with the signature from a signer.
type SignedIP struct {
	UnsignedIP
	TLSSignature      []byte
	BLSSignature      *bls.Signature
	BLSSignatureBytes []byte
}

// Returns nil if:
// * [ip.Timestamp] is within the allowed clock skew range (not too far in past or future).
// * [ip.TLSSignature] is a valid signature over [ip.UnsignedIP] from [cert].
//
// [maxTimestamp] defines the maximum allowed timestamp (current time + MaxClockDifference).
// The minimum allowed timestamp is inferred as (maxTimestamp - ReasonableClockSkewWindow) to
// prevent replay attacks. We use a conservative 10-minute window to account for various
// MaxClockDifference configurations while still protecting against replay attacks.
func (ip *SignedIP) Verify(
	cert *staking.Certificate,
	maxTimestamp time.Time,
) error {
	maxUnixTimestamp := uint64(maxTimestamp.Unix())
	if ip.Timestamp > maxUnixTimestamp {
		return fmt.Errorf("%w: timestamp %d > maxTimestamp %d", errTimestampTooFarInFuture, ip.Timestamp, maxUnixTimestamp)
	}

	// Prevent replay attacks by rejecting timestamps too far in the past.
	// We use a conservative 10-minute total window. This accommodates:
	// - Default MaxClockDifference of 1 minute (2-minute total window)
	// - Larger MaxClockDifference configurations (up to 5 minutes)
	// - Edge cases where maxTimestamp might be slightly in the past due to test timing
	//
	// For example, with MaxClockDifference = 1 minute:
	// - Current time: 12:00
	// - maxTimestamp: 12:01
	// - minTimestamp: 11:51 (maxTimestamp - 10 min)
	// This prevents replay of IPs older than 10 minutes.
	const reasonableClockSkewWindow = 10 * time.Minute
	minTimestamp := maxTimestamp.Add(-reasonableClockSkewWindow)
	minUnixTimestamp := uint64(minTimestamp.Unix())
	if ip.Timestamp < minUnixTimestamp {
		return fmt.Errorf("%w: timestamp %d < minTimestamp %d", errTimestampTooFarInPast, ip.Timestamp, minUnixTimestamp)
	}

	if err := staking.CheckSignature(
		cert,
		ip.UnsignedIP.bytes(),
		ip.TLSSignature,
	); err != nil {
		return fmt.Errorf("%w: %w", errInvalidTLSSignature, err)
	}
	return nil
}
