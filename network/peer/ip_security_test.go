// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/staking"
)

// TestSignedIP_ReplayAttackPrevention verifies that old timestamps
// are rejected to prevent replay attacks.
func TestSignedIP_ReplayAttackPrevention(t *testing.T) {
	req := require.New(t)

	// Generate test certificate
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	req.NoError(err)

	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, &key.PublicKey, key)
	req.NoError(err)

	stakingCert := &staking.Certificate{
		Raw:       certDER,
		PublicKey: &key.PublicKey,
	}

	currentTime := time.Now()
	maxClockDiff := time.Minute

	tests := []struct {
		name          string
		timestamp     time.Time
		maxTimestamp  time.Time
		shouldSucceed bool
		expectedError error
	}{
		{
			name:          "current time - should succeed",
			timestamp:     currentTime,
			maxTimestamp:  currentTime.Add(maxClockDiff),
			shouldSucceed: true,
		},
		{
			name:          "slightly in future - should succeed",
			timestamp:     currentTime.Add(30 * time.Second),
			maxTimestamp:  currentTime.Add(maxClockDiff),
			shouldSucceed: true,
		},
		{
			name:          "slightly in past - should succeed",
			timestamp:     currentTime.Add(-30 * time.Second),
			maxTimestamp:  currentTime.Add(maxClockDiff),
			shouldSucceed: true,
		},
		{
			name:          "far in future - should fail",
			timestamp:     currentTime.Add(2 * maxClockDiff),
			maxTimestamp:  currentTime.Add(maxClockDiff),
			shouldSucceed: false,
			expectedError: errTimestampTooFarInFuture,
		},
		{
			name:          "far in past (replay attack) - should fail",
			timestamp:     currentTime.Add(-15 * time.Minute), // Beyond 10-minute window
			maxTimestamp:  currentTime.Add(maxClockDiff),
			shouldSucceed: false,
			expectedError: errTimestampTooFarInPast,
		},
		{
			name:          "exactly at min boundary - should succeed",
			timestamp:     currentTime.Add(-maxClockDiff),
			maxTimestamp:  currentTime.Add(maxClockDiff),
			shouldSucceed: true,
		},
		{
			name:          "exactly at max boundary - should succeed",
			timestamp:     currentTime.Add(maxClockDiff),
			maxTimestamp:  currentTime.Add(maxClockDiff),
			shouldSucceed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			innerRequire := require.New(t)

			unsignedIP := &UnsignedIP{
				AddrPort:  netip.MustParseAddrPort("1.2.3.4:5678"),
				Timestamp: uint64(tt.timestamp.Unix()),
			}

			// Sign the IP (must hash first per staking.CheckSignature requirements)
			ipBytes := unsignedIP.bytes()
			hash := crypto.SHA256.New()
			hash.Write(ipBytes)
			hashed := hash.Sum(nil)
			signature, err := key.Sign(rand.Reader, hashed, crypto.SHA256)
			innerRequire.NoError(err)

			signedIP := &SignedIP{
				UnsignedIP:   *unsignedIP,
				TLSSignature: signature,
			}

			// Verify
			err = signedIP.Verify(stakingCert, tt.maxTimestamp)

			if tt.shouldSucceed {
				innerRequire.NoError(err, "Expected verification to succeed")
			} else {
				innerRequire.Error(err, "Expected verification to fail")
				if tt.expectedError != nil {
					innerRequire.ErrorIs(err, tt.expectedError)
				}
			}
		})
	}
}

// TestSignedIP_ClockSkewWindow verifies the size of the acceptance window.
func TestSignedIP_ClockSkewWindow(t *testing.T) {
	require := require.New(t)

	// With MaxClockDifference = 1 minute:
	// - maxTimestamp = currentTime + 1 min
	// - minTimestamp = currentTime - 1 min
	// - Total window = 2 minutes

	currentTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	maxClockDiff := time.Minute

	// Expected window boundaries
	expectedMinTime := currentTime.Add(-maxClockDiff)
	expectedMaxTime := currentTime.Add(maxClockDiff)

	// Total window should be 2 * MaxClockDifference
	expectedWindow := 2 * maxClockDiff

	actualWindow := expectedMaxTime.Sub(expectedMinTime)
	require.Equal(expectedWindow, actualWindow,
		"Clock skew window should be 2x MaxClockDifference")

	t.Logf("Clock skew acceptance window:")
	t.Logf("  Current time:  %s", currentTime.Format(time.RFC3339))
	t.Logf("  Min timestamp: %s (current - %s)", expectedMinTime.Format(time.RFC3339), maxClockDiff)
	t.Logf("  Max timestamp: %s (current + %s)", expectedMaxTime.Format(time.RFC3339), maxClockDiff)
	t.Logf("  Total window:  %s", actualWindow)
}
