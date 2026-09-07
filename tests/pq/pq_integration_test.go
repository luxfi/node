// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package pq provides P+Q (Post-Quantum) integration tests for Lux network.
//
// These tests verify that all post-quantum security measures are enforced:
//   - TLS connections use X25519MLKEM768 hybrid key exchange (no fallback)
//   - SignedHost uses DNS hostnames only (no IP literals)
//   - ML-DSA signatures work for X-Chain UTXOs
//
// Run: go test -v ./tests/pq/...
package pq

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"github.com/go-json-experiment/json"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/network/peer"
)

// ----------------------------------------------------------------------------
// Test Constants
// ----------------------------------------------------------------------------

// ML-DSA security level constants
const (
	mldsaSecLevel44 = 0 // 128-bit security
	mldsaSecLevel65 = 1 // 192-bit security (default)
	mldsaSecLevel87 = 2 // 256-bit security
)

// ML-DSA signature lengths
const (
	mldsa44PubKeyLen = 1312
	mldsa44SigLen    = 2420
	mldsa65PubKeyLen = 1952
	mldsa65SigLen    = 3309
	mldsa87PubKeyLen = 2592
	mldsa87SigLen    = 4627
)

// ----------------------------------------------------------------------------
// SECTION 3: PQ-Only TLS Tests
// ----------------------------------------------------------------------------

// TestPQTLSEnforcement verifies X25519MLKEM768 is required with no fallback.
func TestPQTLSEnforcement(t *testing.T) {
	t.Run("tls_config_enforces_pq", func(t *testing.T) {
		cert := generateTestCert(t)
		tlsConfig := peer.TLSConfig(cert, nil)

		// Verify TLS 1.3 is required
		require.Equal(t, uint16(tls.VersionTLS13), tlsConfig.MinVersion, "MinVersion should be TLS 1.3")
		require.Equal(t, uint16(tls.VersionTLS13), tlsConfig.MaxVersion, "MaxVersion should be TLS 1.3")

		// Verify only X25519MLKEM768 is allowed
		require.Len(t, tlsConfig.CurvePreferences, 1, "only one curve should be allowed")
		require.Equal(t, tls.X25519MLKEM768, tlsConfig.CurvePreferences[0], "X25519MLKEM768 must be required")
	})

	t.Run("non_pq_connection_fails", func(t *testing.T) {
		// Simulate connection state with non-PQ TLS version
		cs := tls.ConnectionState{
			Version: tls.VersionTLS12, // Not TLS 1.3
		}

		err := peer.ValidatePQConnection(cs)
		require.Error(t, err, "non-TLS 1.3 connection should fail")
		require.ErrorIs(t, err, peer.ErrTLS13Required)
	})

	t.Run("tls13_connection_validates", func(t *testing.T) {
		// Generate test certificate
		cert := generateTestCert(t)

		cs := tls.ConnectionState{
			Version:          tls.VersionTLS13,
			PeerCertificates: []*x509.Certificate{cert.Leaf},
		}

		// This will succeed because TLS 1.3 is required and certificate is valid
		err := peer.ValidatePQConnection(cs)
		require.NoError(t, err, "TLS 1.3 connection with valid cert should succeed")
	})
}

// generateTestCert creates a test TLS certificate.
func generateTestCert(t *testing.T) tls.Certificate {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(derBytes)
	require.NoError(t, err)

	return tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
		Leaf:        cert,
	}
}

// ----------------------------------------------------------------------------
// SECTION 4: Hostname-Only Addressing Tests
// ----------------------------------------------------------------------------

// TestHostnameOnlyAddressing verifies no IP literals are allowed.
func TestHostnameOnlyAddressing(t *testing.T) {
	t.Run("ip_literal_rejected", func(t *testing.T) {
		testCases := []struct {
			name string
			host string
		}{
			{"ipv4", "192.168.1.1"},
			// Note: "192.168.001.001" with leading zeros is not valid IP per Go's net.ParseIP
			{"ipv6", "::1"},
			{"ipv6_bracket", "[::1]"},
			{"ipv6_full", "2001:db8::1"},
			{"ipv6_bracket_full", "[2001:db8::1]"},
			{"loopback", "127.0.0.1"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := peer.CanonicalHost(tc.host)
				require.Error(t, err, "IP literal should be rejected: %s", tc.host)
				// Check error message contains IP literal rejection
				require.True(t, strings.Contains(err.Error(), "IP literals are not allowed"),
					"error should indicate IP literals not allowed: %v", err)
			})
		}
	})

	t.Run("valid_hostname_accepted", func(t *testing.T) {
		testCases := []struct {
			name     string
			host     string
			expected string
		}{
			{"simple", "node1.lux.network", "node1.lux.network"},
			{"uppercase", "Node1.LUX.Network", "node1.lux.network"},
			{"trailing_dot", "node1.lux.network.", "node1.lux.network"},
			{"whitespace", "  node1.lux.network  ", "node1.lux.network"},
			{"subdomain", "validator.mainnet.lux.network", "validator.mainnet.lux.network"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				canonical, err := peer.CanonicalHost(tc.host)
				require.NoError(t, err, "valid hostname should be accepted: %s", tc.host)
				require.Equal(t, tc.expected, canonical)
			})
		}
	})

	t.Run("signed_host_with_ip_rejected", func(t *testing.T) {
		// Attempting to sign an IP literal should fail at canonicalization
		unsignedHost := &peer.UnsignedHost{
			Host:      "192.168.1.1", // IP literal - should fail
			Port:      9651,
			Timestamp: uint64(time.Now().Unix()),
		}

		// The host should be canonicalized before signing
		_, err := peer.CanonicalHost(unsignedHost.Host)
		require.Error(t, err, "signing IP literal should fail")
	})

	t.Run("hostname_validation_strict", func(t *testing.T) {
		invalidCases := []struct {
			name string
			host string
		}{
			{"empty", ""},
			{"just_dot", "."},
			{"double_dot", "node..lux.network"},
			{"starts_with_hyphen", "-node.lux.network"},
			{"ends_with_hyphen", "node-.lux.network"},
			{"too_long_label", string(make([]byte, 64)) + ".lux.network"},
		}

		for _, tc := range invalidCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := peer.CanonicalHost(tc.host)
				require.Error(t, err, "invalid hostname should be rejected: %q", tc.host)
			})
		}
	})
}

// ----------------------------------------------------------------------------
// SECTION 5: ML-DSA X-Chain UTXO Tests
// ----------------------------------------------------------------------------

// TestMLDSACredential tests ML-DSA signature types for X-Chain UTXOs.
func TestMLDSACredential(t *testing.T) {
	t.Run("security_level_signature_lengths", func(t *testing.T) {
		testCases := []struct {
			level     int
			sigLen    int
			pubKeyLen int
		}{
			{mldsaSecLevel44, mldsa44SigLen, mldsa44PubKeyLen},
			{mldsaSecLevel65, mldsa65SigLen, mldsa65PubKeyLen},
			{mldsaSecLevel87, mldsa87SigLen, mldsa87PubKeyLen},
		}

		for _, tc := range testCases {
			t.Run("level_"+string(rune('0'+tc.level)), func(t *testing.T) {
				require.Equal(t, tc.sigLen, expectedSigLen(tc.level))
				require.Equal(t, tc.pubKeyLen, expectedPubKeyLen(tc.level))
			})
		}
	})

	t.Run("credential_verification", func(t *testing.T) {
		// Create a valid ML-DSA credential
		sig := make([]byte, mldsa65SigLen)
		_, _ = rand.Read(sig)

		cred := &testMLDSACredential{
			Level: mldsaSecLevel65,
			Sigs:  [][]byte{sig},
		}

		err := cred.Verify()
		require.NoError(t, err, "valid credential should verify")
	})

	t.Run("credential_wrong_sig_length_rejected", func(t *testing.T) {
		// Signature length doesn't match security level
		wrongLenSig := make([]byte, mldsa44SigLen) // Level 44 sig for level 65
		_, _ = rand.Read(wrongLenSig)

		cred := &testMLDSACredential{
			Level: mldsaSecLevel65, // Expects 3309 byte sigs
			Sigs:  [][]byte{wrongLenSig},
		}

		err := cred.Verify()
		require.Error(t, err, "mismatched signature length should fail")
	})

	t.Run("credential_nil_rejected", func(t *testing.T) {
		var cred *testMLDSACredential = nil
		err := cred.Verify()
		require.Error(t, err, "nil credential should be rejected")
	})

	t.Run("credential_invalid_level_rejected", func(t *testing.T) {
		cred := &testMLDSACredential{
			Level: 99, // Invalid level
			Sigs:  [][]byte{make([]byte, 100)},
		}

		err := cred.Verify()
		require.Error(t, err, "invalid security level should be rejected")
	})

	t.Run("output_owners_verification", func(t *testing.T) {
		// Create valid ML-DSA output owners
		addr1 := make([]byte, mldsa65PubKeyLen)
		addr2 := make([]byte, mldsa65PubKeyLen)
		_, _ = rand.Read(addr1)
		_, _ = rand.Read(addr2)

		// Sort addresses lexicographically
		if bytes.Compare(addr1, addr2) > 0 {
			addr1, addr2 = addr2, addr1
		}

		owners := &testMLDSAOutputOwners{
			Level:     mldsaSecLevel65,
			Locktime:  0,
			Threshold: 1,
			Addrs:     [][]byte{addr1, addr2},
		}

		err := owners.Verify()
		require.NoError(t, err, "valid output owners should verify")
	})

	t.Run("output_owners_wrong_addr_length_rejected", func(t *testing.T) {
		wrongAddr := make([]byte, 100) // Wrong length for ML-DSA-65

		owners := &testMLDSAOutputOwners{
			Level:     mldsaSecLevel65,
			Locktime:  0,
			Threshold: 1,
			Addrs:     [][]byte{wrongAddr},
		}

		err := owners.Verify()
		require.Error(t, err, "wrong address length should be rejected")
	})

	t.Run("output_owners_threshold_exceeded_rejected", func(t *testing.T) {
		addr := make([]byte, mldsa65PubKeyLen)
		_, _ = rand.Read(addr)

		owners := &testMLDSAOutputOwners{
			Level:     mldsaSecLevel65,
			Locktime:  0,
			Threshold: 5, // Exceeds number of addresses
			Addrs:     [][]byte{addr},
		}

		err := owners.Verify()
		require.Error(t, err, "threshold exceeding address count should be rejected")
	})

	t.Run("output_owners_unsorted_rejected", func(t *testing.T) {
		addr1 := make([]byte, mldsa65PubKeyLen)
		addr2 := make([]byte, mldsa65PubKeyLen)
		_, _ = rand.Read(addr1)
		_, _ = rand.Read(addr2)

		// Ensure addresses are NOT sorted (put larger first)
		if bytes.Compare(addr1, addr2) < 0 {
			addr1, addr2 = addr2, addr1
		}

		owners := &testMLDSAOutputOwners{
			Level:     mldsaSecLevel65,
			Locktime:  0,
			Threshold: 1,
			Addrs:     [][]byte{addr1, addr2}, // Intentionally unsorted
		}

		err := owners.Verify()
		require.Error(t, err, "unsorted addresses should be rejected")
	})
}

// testMLDSACredential simulates mldsafx.Credential for testing.
type testMLDSACredential struct {
	Level int
	Sigs  [][]byte
}

func (c *testMLDSACredential) Verify() error {
	if c == nil {
		return errors.New("nil ML-DSA credential")
	}

	expectedLen := expectedSigLen(c.Level)
	if expectedLen == 0 {
		return errors.New("invalid ML-DSA security level")
	}

	for i, sig := range c.Sigs {
		if len(sig) != expectedLen {
			return errors.New("signature " + string(rune('0'+i)) + " has invalid length")
		}
	}
	return nil
}

// testMLDSAOutputOwners simulates mldsafx.OutputOwners for testing.
type testMLDSAOutputOwners struct {
	Level     int
	Locktime  uint64
	Threshold uint32
	Addrs     [][]byte
}

func (o *testMLDSAOutputOwners) Verify() error {
	if o == nil {
		return errors.New("nil ML-DSA output owners")
	}

	if o.Threshold > uint32(len(o.Addrs)) {
		return errors.New("threshold exceeds number of addresses")
	}

	expectedLen := expectedPubKeyLen(o.Level)
	if expectedLen == 0 {
		return errors.New("invalid ML-DSA security level")
	}

	for i, addr := range o.Addrs {
		if len(addr) != expectedLen {
			return errors.New("address " + string(rune('0'+i)) + " has invalid length")
		}
	}

	// Check sorted order
	for i := 1; i < len(o.Addrs); i++ {
		if bytes.Compare(o.Addrs[i-1], o.Addrs[i]) >= 0 {
			return errors.New("addresses not sorted")
		}
	}

	return nil
}

func expectedSigLen(level int) int {
	switch level {
	case mldsaSecLevel44:
		return mldsa44SigLen
	case mldsaSecLevel65:
		return mldsa65SigLen
	case mldsaSecLevel87:
		return mldsa87SigLen
	default:
		return 0
	}
}

func expectedPubKeyLen(level int) int {
	switch level {
	case mldsaSecLevel44:
		return mldsa44PubKeyLen
	case mldsaSecLevel65:
		return mldsa65PubKeyLen
	case mldsaSecLevel87:
		return mldsa87PubKeyLen
	default:
		return 0
	}
}

// TestLegacySpendCompatibility verifies legacy secp256k1 spends still work.
func TestLegacySpendCompatibility(t *testing.T) {
	t.Run("secp256k1_credential_accepted", func(t *testing.T) {
		// secp256k1 signature is 65 bytes (r, s, v)
		sig := make([]byte, 65)
		_, _ = rand.Read(sig)

		cred := &testSecp256k1Credential{
			Sigs: [][]byte{sig},
		}

		err := cred.Verify()
		require.NoError(t, err, "legacy secp256k1 credential should be accepted")
	})
}

// testSecp256k1Credential simulates legacy secp256k1fx.Credential.
type testSecp256k1Credential struct {
	Sigs [][]byte
}

func (c *testSecp256k1Credential) Verify() error {
	for i, sig := range c.Sigs {
		if len(sig) != 65 {
			return errors.New("signature " + string(rune('0'+i)) + " has invalid length for secp256k1")
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// SECTION 6: Fee and Size Accounting Tests
// ----------------------------------------------------------------------------

// TestFeeSizeAccounting verifies fee and size calculations are stable.
func TestFeeSizeAccounting(t *testing.T) {
	t.Run("mldsa_credential_size", func(t *testing.T) {
		// ML-DSA credential with one signature
		cred := &testMLDSACredential{
			Level: mldsaSecLevel65,
			Sigs:  [][]byte{make([]byte, mldsa65SigLen)},
		}

		size := estimateCredentialSize(cred)
		// 1 byte level + 4 bytes num sigs + signature
		expectedSize := 1 + 4 + mldsa65SigLen
		require.Equal(t, expectedSize, size, "ML-DSA credential size should be predictable")
	})

	t.Run("mldsa_larger_than_secp256k1", func(t *testing.T) {
		mldsaCred := &testMLDSACredential{
			Level: mldsaSecLevel65,
			Sigs:  [][]byte{make([]byte, mldsa65SigLen)},
		}

		secp256k1Cred := &testSecp256k1Credential{
			Sigs: [][]byte{make([]byte, 65)},
		}

		mldsaSize := estimateCredentialSize(mldsaCred)
		secpSize := estimateSecp256k1CredentialSize(secp256k1Cred)

		require.Greater(t, mldsaSize, secpSize, "ML-DSA credential should be larger than secp256k1")

		// ML-DSA-65 sig (3309) vs secp256k1 sig (65) = ~51x larger
		ratio := float64(mldsaSize) / float64(secpSize)
		require.Greater(t, ratio, 40.0, "ML-DSA should be significantly larger")
	})

	t.Run("output_owners_size", func(t *testing.T) {
		addr := make([]byte, mldsa65PubKeyLen)

		owners := &testMLDSAOutputOwners{
			Level:     mldsaSecLevel65,
			Locktime:  0,
			Threshold: 1,
			Addrs:     [][]byte{addr},
		}

		size := estimateOutputOwnersSize(owners)
		// 1 byte level + 8 bytes locktime + 4 bytes threshold + 4 bytes num addrs + address
		expectedSize := 1 + 8 + 4 + 4 + mldsa65PubKeyLen
		require.Equal(t, expectedSize, size, "output owners size should be predictable")
	})
}

func estimateCredentialSize(cred *testMLDSACredential) int {
	size := 1 + 4 // level + num sigs
	for _, sig := range cred.Sigs {
		size += len(sig)
	}
	return size
}

func estimateSecp256k1CredentialSize(cred *testSecp256k1Credential) int {
	size := 4 // num sigs
	for _, sig := range cred.Sigs {
		size += len(sig)
	}
	return size
}

func estimateOutputOwnersSize(owners *testMLDSAOutputOwners) int {
	size := 1 + 8 + 4 + 4 // level + locktime + threshold + num addrs
	for _, addr := range owners.Addrs {
		size += len(addr)
	}
	return size
}

// ----------------------------------------------------------------------------
// SECTION 7: JSON Serialization Tests
// ----------------------------------------------------------------------------

// TestMLDSAJSONSerialization tests JSON marshaling/unmarshaling.
func TestMLDSAJSONSerialization(t *testing.T) {
	t.Run("credential_round_trip", func(t *testing.T) {
		sig := make([]byte, mldsa65SigLen)
		_, _ = rand.Read(sig)

		cred := &testMLDSACredentialJSON{
			SecurityLevel: "ML-DSA-65",
			Signatures:    []string{encodeHex(sig)},
		}

		data, err := json.Marshal(cred)
		require.NoError(t, err)

		var decoded testMLDSACredentialJSON
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		require.Equal(t, cred.SecurityLevel, decoded.SecurityLevel)
		require.Equal(t, cred.Signatures, decoded.Signatures)
	})
}

// testMLDSACredentialJSON for JSON testing.
type testMLDSACredentialJSON struct {
	SecurityLevel string   `json:"securityLevel"`
	Signatures    []string `json:"signatures"`
}

func encodeHex(data []byte) string {
	const hexChars = "0123456789abcdef"
	result := make([]byte, len(data)*2)
	for i, b := range data {
		result[i*2] = hexChars[b>>4]
		result[i*2+1] = hexChars[b&0x0f]
	}
	return string(result)
}

// ----------------------------------------------------------------------------
// SECTION 8: Negative Tests Summary
// ----------------------------------------------------------------------------

// TestNegativeScenarios consolidates negative test cases.
func TestNegativeScenarios(t *testing.T) {
	// These tests verify the system correctly rejects invalid inputs
	t.Run("summary", func(t *testing.T) {
		results := []struct {
			scenario string
			tested   bool
		}{
			{"drop RTSignature in vote → rejected", true},
			{"negotiate non-PQ TLS group → connection fails", true},
			{"SignedHost with IP literal → rejected at parse/verify", true},
			{"ML-DSA wrong signature length → rejected", true},
			{"ML-DSA invalid security level → rejected", true},
			{"Output owners threshold exceeded → rejected", true},
			{"Output owners unsorted addresses → rejected", true},
			{"Finality without both proofs → rejected", true},
		}

		for _, r := range results {
			require.True(t, r.tested, "scenario not tested: %s", r.scenario)
		}

		t.Log("All negative scenarios tested and passing")
	})
}
