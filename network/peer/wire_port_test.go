// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWirePort pins the narrowing of a wire uint32 port. The Handshake path used
// to test the untruncated value for zero and then narrow it, so any multiple of
// 65536 was accepted and became port 0 — while the PeerList path, which tested
// the narrowed value, closed the connection for the same claim.
func TestWirePort(t *testing.T) {
	tests := []struct {
		name     string
		wire     uint32
		wantPort uint16
		wantOK   bool
	}{
		{name: "zero", wire: 0},
		{name: "truncates to zero", wire: 65536},
		{name: "truncates to zero, larger", wire: 4294901760},
		{name: "truncates to a nonzero port", wire: 65536 + 9651},
		{name: "max uint32", wire: math.MaxUint32},
		{name: "lowest valid", wire: 1, wantPort: 1, wantOK: true},
		{name: "staking port", wire: 9651, wantPort: 9651, wantOK: true},
		{name: "highest valid", wire: math.MaxUint16, wantPort: math.MaxUint16, wantOK: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			port, ok := wirePort(test.wire)
			require.Equal(t, test.wantOK, ok)
			require.Equal(t, test.wantPort, port)
		})
	}
}
