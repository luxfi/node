// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/net/endpoints"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/util"
)

// endpointSigners returns a staker cert with its TLS signer plus a BLS signer,
// which is exactly the key material a validator signs an endpoint claim with.
func endpointSigners(t *testing.T) (*staking.Certificate, crypto.Signer, bls.Signer) {
	t.Helper()
	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)
	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(t, err)
	blsKey, err := localsigner.New()
	require.NoError(t, err)
	return cert, tlsCert.PrivateKey.(crypto.Signer), blsKey
}

func testIPEndpoint() endpoints.Endpoint {
	return endpoints.NewIPEndpoint(netip.MustParseAddrPort("203.0.113.7:9651"))
}

func testHostEndpoint(t *testing.T, host string) endpoints.Endpoint {
	t.Helper()
	e, err := endpoints.NewHostnameEndpoint(host, 9651)
	require.NoError(t, err)
	return e
}

// TestSignedEndpoint_VerifiesWhatWasSigned is the base case for both endpoint
// kinds. A claim minted by a validator must verify under that validator's
// certificate, and the hostname path must work as well as the IP path — the
// whole reason the endpoint type exists is the node that has no stable IP.
func TestSignedEndpoint_VerifiesWhatWasSigned(t *testing.T) {
	for name, endpoint := range map[string]endpoints.Endpoint{
		"ip":       testIPEndpoint(),
		"hostname": testHostEndpoint(t, "validator-07.lux.cloud"),
	} {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			cert, tlsSigner, blsSigner := endpointSigners(t)

			now := time.Now()
			unsigned := &UnsignedEndpoint{Endpoint: endpoint, Timestamp: uint64(now.Unix())}
			signed, err := unsigned.Sign(tlsSigner, blsSigner)
			require.NoError(err)
			require.NotEmpty(signed.TLSSignature)
			require.NotNil(signed.BLSSignature)
			require.Equal(bls.SignatureToBytes(signed.BLSSignature), signed.BLSSignatureBytes,
				"the cached signature bytes must be the signature")

			require.NoError(signed.Verify(cert, now.Add(time.Minute)))
		})
	}
}

// TestSignedEndpoint_RefusesAnotherValidatorsCertificate is the point of
// signing at all: a claim is a statement by one key holder, and it must not
// verify under anybody else's certificate. Without this, an endpoint claim is
// a suggestion.
func TestSignedEndpoint_RefusesAnotherValidatorsCertificate(t *testing.T) {
	require := require.New(t)

	_, tlsSigner, blsSigner := endpointSigners(t)
	otherCert, _, _ := endpointSigners(t)

	now := time.Now()
	unsigned := &UnsignedEndpoint{Endpoint: testIPEndpoint(), Timestamp: uint64(now.Unix())}
	signed, err := unsigned.Sign(tlsSigner, blsSigner)
	require.NoError(err)

	require.ErrorIs(signed.Verify(otherCert, now.Add(time.Minute)), errInvalidEndpointSignature,
		"a claim must not verify under a certificate that did not sign it")
}

// TestSignedEndpoint_AnyEditBreaksTheSignature walks each field a peer could
// rewrite in flight. Every one must break verification — a signature that
// covers only some of the claim lets a relay redirect a validator's traffic
// while the claim still checks out.
func TestSignedEndpoint_AnyEditBreaksTheSignature(t *testing.T) {
	require := require.New(t)
	cert, tlsSigner, blsSigner := endpointSigners(t)

	now := time.Now()
	unsigned := &UnsignedEndpoint{Endpoint: testIPEndpoint(), Timestamp: uint64(now.Unix())}
	signed, err := unsigned.Sign(tlsSigner, blsSigner)
	require.NoError(err)
	require.NoError(signed.Verify(cert, now.Add(time.Minute)))

	edits := map[string]func(*SignedEndpoint){
		"address": func(s *SignedEndpoint) {
			s.Endpoint = endpoints.NewIPEndpoint(netip.MustParseAddrPort("198.51.100.9:9651"))
		},
		"port": func(s *SignedEndpoint) {
			s.Endpoint = endpoints.NewIPEndpoint(netip.MustParseAddrPort("203.0.113.7:9999"))
		},
		"timestamp": func(s *SignedEndpoint) { s.Timestamp++ },
		"endpoint kind": func(s *SignedEndpoint) {
			s.Endpoint = testHostEndpoint(t, "validator-07.lux.cloud")
		},
		"signature byte": func(s *SignedEndpoint) {
			s.TLSSignature[len(s.TLSSignature)/2] ^= 0x01
		},
		"signature truncated": func(s *SignedEndpoint) {
			s.TLSSignature = s.TLSSignature[:len(s.TLSSignature)-1]
		},
		"signature absent": func(s *SignedEndpoint) { s.TLSSignature = nil },
	}

	for name, edit := range edits {
		tampered := *signed
		tampered.TLSSignature = append([]byte(nil), signed.TLSSignature...)
		edit(&tampered)
		require.Error(tampered.Verify(cert, now.Add(time.Minute)),
			"editing the %s must invalidate the claim", name)
	}
}

// TestSignedEndpoint_TimestampWindowIsClosedAtBothEnds is the replay bound. A
// claim from the future is a peer with a broken clock or a peer trying to
// outrank the real one forever; a claim from the deep past is a recording. Both
// are refused, and by name.
func TestSignedEndpoint_TimestampWindowIsClosedAtBothEnds(t *testing.T) {
	require := require.New(t)
	cert, tlsSigner, blsSigner := endpointSigners(t)

	now := time.Now()
	maxTimestamp := now.Add(time.Minute)

	sign := func(at time.Time) *SignedEndpoint {
		unsigned := &UnsignedEndpoint{Endpoint: testIPEndpoint(), Timestamp: uint64(at.Unix())}
		s, err := unsigned.Sign(tlsSigner, blsSigner)
		require.NoError(err)
		return s
	}

	require.NoError(sign(now).Verify(cert, maxTimestamp))
	require.NoError(sign(maxTimestamp).Verify(cert, maxTimestamp),
		"the window is inclusive at its upper edge")
	require.ErrorIs(sign(maxTimestamp.Add(time.Second)).Verify(cert, maxTimestamp),
		errTimestampTooFarInFuture)

	// The lower edge sits ten minutes below maxTimestamp.
	floor := maxTimestamp.Add(-10 * time.Minute)
	require.NoError(sign(floor).Verify(cert, maxTimestamp),
		"the window is inclusive at its lower edge")
	require.ErrorIs(sign(floor.Add(-time.Second)).Verify(cert, maxTimestamp),
		errTimestampTooFarInPast)
	require.ErrorIs(sign(now.Add(-time.Hour)).Verify(cert, maxTimestamp),
		errTimestampTooFarInPast)
}

// TestSignedEndpoint_ToSignedIPOnlyForAddresses guards the legacy bridge. An
// IP claim converts to the old SignedIP shape carrying the same signatures; a
// hostname claim has no IP to hand over and must return nothing rather than a
// SignedIP for the zero address, which every legacy peer would read as a real
// claim on 0.0.0.0.
func TestSignedEndpoint_ToSignedIPOnlyForAddresses(t *testing.T) {
	require := require.New(t)
	cert, tlsSigner, blsSigner := endpointSigners(t)

	now := time.Now()
	addrPort := netip.MustParseAddrPort("203.0.113.7:9651")

	ipClaim, err := (&UnsignedEndpoint{
		Endpoint:  endpoints.NewIPEndpoint(addrPort),
		Timestamp: uint64(now.Unix()),
	}).Sign(tlsSigner, blsSigner)
	require.NoError(err)

	legacy := ipClaim.ToSignedIP()
	require.NotNil(legacy)
	require.Equal(addrPort, legacy.AddrPort)
	require.Equal(ipClaim.Timestamp, legacy.Timestamp)
	require.Equal(ipClaim.TLSSignature, legacy.TLSSignature)
	require.Equal(ipClaim.BLSSignatureBytes, legacy.BLSSignatureBytes)

	hostClaim, err := (&UnsignedEndpoint{
		Endpoint:  testHostEndpoint(t, "validator-07.lux.cloud"),
		Timestamp: uint64(now.Unix()),
	}).Sign(tlsSigner, blsSigner)
	require.NoError(err)
	require.Nil(hostClaim.ToSignedIP(),
		"a hostname claim has no address to hand a legacy peer")

	// The conversion carries the endpoint signature, which is over the
	// endpoint's own byte form — NOT over the legacy UnsignedIP bytes. A
	// legacy verifier must therefore reject it, and knowing that is the
	// difference between a bridge and a footgun.
	require.Error(legacy.Verify(cert, now.Add(time.Minute)),
		"the two wire forms sign different bytes; the conversion does not re-sign")
}

// TestUnsignedEndpoint_DistinctClaimsSignDistinctBytes is the collision
// property. Two claims that differ in anything a verifier cares about must
// produce different signed bytes, or one signature authorises both. The
// interesting pair is an IP endpoint against a hostname endpoint: their byte
// forms are different lengths and different shapes, and only the leading type
// tag keeps them from being read as each other.
func TestUnsignedEndpoint_DistinctClaimsSignDistinctBytes(t *testing.T) {
	require := require.New(t)

	claims := map[string]*UnsignedEndpoint{
		"ip":             {Endpoint: testIPEndpoint(), Timestamp: 1000},
		"ip, other port": {Endpoint: endpoints.NewIPEndpoint(netip.MustParseAddrPort("203.0.113.7:9652")), Timestamp: 1000},
		"ip, other addr": {Endpoint: endpoints.NewIPEndpoint(netip.MustParseAddrPort("203.0.113.8:9651")), Timestamp: 1000},
		"ip, later":      {Endpoint: testIPEndpoint(), Timestamp: 1001},
		"host":           {Endpoint: testHostEndpoint(t, "a.example"), Timestamp: 1000},
		"host, longer":   {Endpoint: testHostEndpoint(t, "aa.example"), Timestamp: 1000},
		"host, other":    {Endpoint: testHostEndpoint(t, "b.example"), Timestamp: 1000},
		"host, later":    {Endpoint: testHostEndpoint(t, "a.example"), Timestamp: 1001},
	}

	seen := make(map[string]string, len(claims))
	for name, claim := range claims {
		encoded := string(claim.bytes())
		if other, dup := seen[name]; dup {
			t.Fatalf("%q and %q sign identical bytes", name, other)
		}
		for otherName, otherEncoded := range seen {
			require.NotEqual(otherEncoded, encoded,
				"%q and %q sign identical bytes: one signature would authorise both", name, otherName)
		}
		seen[name] = encoded
	}

	// And the encoding is deterministic — the same claim signed twice must
	// produce the same bytes, or the freshness cache never hits.
	repeat := &UnsignedEndpoint{Endpoint: testIPEndpoint(), Timestamp: 1000}
	require.Equal(claims["ip"].bytes(), repeat.bytes())
}

// TestEndpointSigner_IPEndpointAlsoSpeaksLegacy pins the compatibility rule the
// signer's construction encodes: a node on an IP endpoint must be able to
// answer a legacy peer with a SignedIP, and a node on a hostname must not
// invent one.
func TestEndpointSigner_IPEndpointAlsoSpeaksLegacy(t *testing.T) {
	require := require.New(t)
	cert, tlsSigner, blsSigner := endpointSigners(t)

	addrPort := netip.MustParseAddrPort("203.0.113.7:9651")
	ipSigner := NewEndpointSigner(
		utils.NewAtomic(endpoints.NewIPEndpoint(addrPort)), tlsSigner, blsSigner)

	require.False(ipSigner.SupportsHostname())

	claim, err := ipSigner.GetSignedEndpoint()
	require.NoError(err)
	require.NoError(claim.Verify(cert, time.Now().Add(time.Minute)))
	require.True(claim.Endpoint.IsIP())

	legacy, err := ipSigner.GetSignedIP()
	require.NoError(err)
	require.NotNil(legacy, "an IP endpoint must still answer legacy peers")
	require.Equal(addrPort, legacy.AddrPort)
	require.NoError(legacy.Verify(cert, time.Now().Add(time.Minute)),
		"the legacy leg is signed over the legacy bytes and must verify as one")

	hostSigner := NewEndpointSigner(
		utils.NewAtomic(testHostEndpoint(t, "validator-07.lux.cloud")), tlsSigner, blsSigner)
	require.True(hostSigner.SupportsHostname())

	legacy, err = hostSigner.GetSignedIP()
	require.NoError(err)
	require.Nil(legacy, "a hostname node has no address to claim; it must not invent one")

	claim, err = hostSigner.GetSignedEndpoint()
	require.NoError(err)
	require.NoError(claim.Verify(cert, time.Now().Add(time.Minute)))
	require.True(claim.Endpoint.IsHostname())
}

// TestEndpointSigner_TracksTheEndpointItIsPointedAt is the dynamic-address
// property: the signer holds a live reference, so a node whose address changes
// starts claiming the new one without being rebuilt. A signer that cached the
// endpoint would keep advertising an address the node no longer answers on.
func TestEndpointSigner_TracksTheEndpointItIsPointedAt(t *testing.T) {
	require := require.New(t)
	cert, tlsSigner, blsSigner := endpointSigners(t)

	current := utils.NewAtomic(endpoints.NewIPEndpoint(netip.MustParseAddrPort("203.0.113.7:9651")))
	signer := NewEndpointSigner(current, tlsSigner, blsSigner)

	before, err := signer.GetSignedEndpoint()
	require.NoError(err)
	require.Equal("203.0.113.7", before.Endpoint.AddrPort.Addr().String())

	moved := netip.MustParseAddrPort("198.51.100.9:9651")
	current.Set(endpoints.NewIPEndpoint(moved))

	after, err := signer.GetSignedEndpoint()
	require.NoError(err)
	require.Equal(moved, after.Endpoint.AddrPort, "the claim must follow the node")
	require.NoError(after.Verify(cert, time.Now().Add(time.Minute)))
	require.NotEqual(before.TLSSignature, after.TLSSignature,
		"a new address must be signed afresh, never re-labelled")
}

// TestEndpointSigner_LegacyLegMustTrackTheSameAddress is where the signer's
// two views come apart.
//
// One signer answers two audiences: modern peers get GetSignedEndpoint, legacy
// peers get GetSignedIP. Both are answering the same question — where is this
// node — so they must never name different addresses.
//
// They do. The endpoint is held by reference and follows the node, but the
// legacy leg is built once at construction from a COPY of the address that was
// current then. After the node moves, legacy peers keep being handed a signed,
// perfectly valid claim on an address it no longer answers on, and it never
// expires: the legacy signer re-signs the stale address every five minutes, so
// the claim stays fresh forever and always outranks the truth.
func TestEndpointSigner_LegacyLegMustTrackTheSameAddress(t *testing.T) {
	require := require.New(t)
	_, tlsSigner, blsSigner := endpointSigners(t)

	before := netip.MustParseAddrPort("203.0.113.7:9651")
	after := netip.MustParseAddrPort("198.51.100.9:9651")

	current := utils.NewAtomic(endpoints.NewIPEndpoint(before))
	signer := NewEndpointSigner(current, tlsSigner, blsSigner)

	current.Set(endpoints.NewIPEndpoint(after))

	modern, err := signer.GetSignedEndpoint()
	require.NoError(err)
	require.Equal(after, modern.Endpoint.AddrPort, "precondition: the modern claim follows the node")

	legacy, err := signer.GetSignedIP()
	require.NoError(err)
	require.NotNil(legacy)
	require.Equal(after, legacy.AddrPort,
		"both audiences must be told the same address; the legacy leg is claiming where the node used to be")
}

// TestEndpointSigner_LegacyLegAppearsWhenAnAddressDoes is the same defect seen
// from the other side: a node that starts on a hostname and later moves to an
// address can never answer a legacy peer, because whether the legacy leg exists
// at all was decided once, at construction.
func TestEndpointSigner_LegacyLegAppearsWhenAnAddressDoes(t *testing.T) {
	require := require.New(t)
	_, tlsSigner, blsSigner := endpointSigners(t)

	current := utils.NewAtomic(testHostEndpoint(t, "validator-07.lux.cloud"))
	signer := NewEndpointSigner(current, tlsSigner, blsSigner)

	addrPort := netip.MustParseAddrPort("203.0.113.7:9651")
	current.Set(endpoints.NewIPEndpoint(addrPort))

	require.False(signer.SupportsHostname(), "precondition: the node now has an address")

	legacy, err := signer.GetSignedIP()
	require.NoError(err)
	require.NotNil(legacy, "a node that now has an address must be able to claim it to legacy peers")
	require.Equal(addrPort, legacy.AddrPort)
}

// TestIPEndpointFromAddrPort keeps the convenience constructor honest: it must
// produce an IP endpoint carrying exactly the address handed to it.
func TestIPEndpointFromAddrPort(t *testing.T) {
	require := require.New(t)
	addrPort := netip.MustParseAddrPort("[2001:db8::1]:9651")

	e := IPEndpointFromAddrPort(addrPort)
	require.True(e.IsIP())
	require.False(e.IsHostname())
	require.Equal(addrPort, e.AddrPort)
	require.Equal(addrPort.Port(), e.Port)
}
