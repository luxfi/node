// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/zap"
)

// craftCredsBuf writes a credential buffer whose single entry claims
// declaredCount signatures while the signature array holds actualSigs. It is
// writeCredsBuf with the two counts decoupled, which is exactly what a remote
// sender controls.
func craftCredsBuf(declaredCount uint32, actualSigs int) []byte {
	b := zap.NewBuilder(zap.HeaderSize + 128 + credEntry + actualSigs*sigLen)

	clb := b.StartList(credEntry)
	var e [credEntry]byte
	putU32(e[0:], 0)
	putU32(e[4:], declaredCount)
	clb.AddBytes(e[:])
	credsOff, _ := clb.Finish()

	sigOff, sigCount := 0, 0
	if actualSigs > 0 {
		slb := b.StartList(sigLen)
		for i := 0; i < actualSigs; i++ {
			var s [sigLen]byte
			s[0] = byte(i + 1)
			slb.AddBytes(s[:])
		}
		sigOff, _ = slb.Finish()
		sigCount = actualSigs
	}

	ob := b.StartObject(credsObjSize)
	ob.SetList(offCredsList, credsOff, 1)
	ob.SetList(offSigArray, sigOff, sigCount)
	ob.FinishAsRoot()
	return b.Finish()
}

// TestParseCredsBufRejectsOutOfRangeSigs pins the bounds check on the signature
// range a credential entry names. Both numbers come off the wire: an index past
// the array yields a zero zap.Object whose Uint8 dereferences a nil message
// (SIGSEGV), and the count alone sizes a [65]byte allocation.
func TestParseCredsBufRejectsOutOfRangeSigs(t *testing.T) {
	t.Run("count past end", func(t *testing.T) {
		_, err := parseCredsBuf(craftCredsBuf(2, 1))
		require.ErrorIs(t, err, errCredSigsOutOfRange)
	})

	t.Run("no sig array at all", func(t *testing.T) {
		_, err := parseCredsBuf(craftCredsBuf(1, 0))
		require.ErrorIs(t, err, errCredSigsOutOfRange)
	})

	t.Run("count is unrelated to the payload", func(t *testing.T) {
		_, err := parseCredsBuf(craftCredsBuf(math.MaxUint32, 1))
		require.ErrorIs(t, err, errCredSigsOutOfRange)
	})
}

// TestParseCredsBufRoundTrip pins that the bounds check leaves well-formed
// credentials alone, including a credential that carries no signatures.
func TestParseCredsBufRoundTrip(t *testing.T) {
	require := require.New(t)

	first := [sigLen]byte{1, 2, 3}
	second := [sigLen]byte{4, 5, 6}
	in := []verify.Verifiable{
		&secp256k1fx.Credential{Sigs: [][sigLen]byte{first, second}},
		&secp256k1fx.Credential{},
		&secp256k1fx.Credential{Sigs: [][sigLen]byte{second}},
	}

	buf, err := writeCredsBuf(in)
	require.NoError(err)

	out, err := parseCredsBuf(buf)
	require.NoError(err)
	require.Len(out, 3)
	require.Equal([][sigLen]byte{first, second}, out[0].(*secp256k1fx.Credential).Sigs)
	require.Empty(out[1].(*secp256k1fx.Credential).Sigs)
	require.Equal([][sigLen]byte{second}, out[2].(*secp256k1fx.Credential).Sigs)
}
