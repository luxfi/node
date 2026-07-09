// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	zn "github.com/luxfi/proto/zap_native"
	"github.com/luxfi/zap"
)

// nativeCodec is the LP-023 native-ZAP manager under test. Once every tx
// type is bridged this becomes the package-level txs.Codec and the
// reflection stack is deleted.
var nativeCodec = nativeManager{}

// TestNative_AdvanceTime_RoundTrip proves the full native path for the
// simplest proposal tx: struct -> Marshal -> Unmarshal -> struct with field
// equality, no reflection anywhere. The wire bytes are a native ZAP buffer
// (magic "ZAP\x00", Version2) with the AdvanceTime TxKind discriminator.
func TestNative_AdvanceTime_RoundTrip(t *testing.T) {
	require := require.New(t)

	in := &AdvanceTimeTx{Time: 1_766_708_400}

	// Marshal the signed envelope (empty creds => signed == unsigned buffer).
	signed := &Tx{Unsigned: in}
	b, err := nativeCodec.Marshal(CodecVersion, signed)
	require.NoError(err)

	// Wire sanity: it is a real native ZAP Version2 message, not codec bytes.
	msg, perr := zap.Parse(b)
	require.NoError(perr)
	require.Equal(zap.Version2, msg.Version())
	require.Equal(uint8(zn.TxKindAdvanceTime), msg.Root().Uint8(zn.OffsetTxKind))

	// Unmarshal back into a fresh *Tx and assert field equality.
	out := &Tx{}
	version, err := nativeCodec.Unmarshal(b, out)
	require.NoError(err)
	require.Equal(CodecVersion, version)

	gotAT, ok := out.Unsigned.(*AdvanceTimeTx)
	require.True(ok, "decoded unsigned must be *AdvanceTimeTx")
	require.Equal(in.Time, gotAT.Time)
	require.Empty(out.Creds)

	// Bytes are stable: re-marshal is byte-identical (no re-encode drift).
	b2, err := nativeCodec.Marshal(CodecVersion, out)
	require.NoError(err)
	require.Equal(b, b2, "re-marshal must be byte-identical")

	// Size == len(Marshal).
	sz, err := nativeCodec.Size(CodecVersion, signed)
	require.NoError(err)
	require.Equal(len(b), sz)
}

// TestNative_RewardValidator_RoundTrip proves the 32-byte-ID proposal tx.
func TestNative_RewardValidator_RoundTrip(t *testing.T) {
	require := require.New(t)

	id := ids.GenerateTestID()
	in := &RewardValidatorTx{TxID: id}

	b, err := nativeCodec.Marshal(CodecVersion, &Tx{Unsigned: in})
	require.NoError(err)

	msg, perr := zap.Parse(b)
	require.NoError(perr)
	require.Equal(uint8(zn.TxKindRewardValidator), msg.Root().Uint8(zn.OffsetTxKind))

	out := &Tx{}
	_, err = nativeCodec.Unmarshal(b, out)
	require.NoError(err)

	gotRV, ok := out.Unsigned.(*RewardValidatorTx)
	require.True(ok)
	require.Equal(id, gotRV.TxID)
}

// TestNative_UnsignedIsPrefixOfSigned locks the LP-023 envelope invariant
// the tx.go Initialize path depends on: the unsigned bytes are a genuine
// byte-prefix of the signed bytes. For an empty-cred proposal tx they are
// equal; the assertion still exercises the Marshal(&tx.Unsigned) vs
// Marshal(tx) relationship the reflection codec previously guaranteed.
func TestNative_UnsignedIsPrefixOfSigned(t *testing.T) {
	require := require.New(t)

	tx := &Tx{Unsigned: &AdvanceTimeTx{Time: 42}}

	var unsigned UnsignedTx = tx.Unsigned
	ub, err := nativeCodec.Marshal(CodecVersion, &unsigned)
	require.NoError(err)

	sb, err := nativeCodec.Marshal(CodecVersion, tx)
	require.NoError(err)

	require.LessOrEqual(len(ub), len(sb))
	require.Equal(ub, sb[:len(ub)], "unsigned bytes must be a prefix of signed bytes")
}

// TestNative_RejectsNonZAP confirms non-ZAP input is refused at the wire
// boundary (magic check) rather than silently mis-parsed.
func TestNative_RejectsNonZAP(t *testing.T) {
	require := require.New(t)

	out := &Tx{}
	_, err := nativeCodec.Unmarshal([]byte("not a zap buffer at all!!"), out)
	require.Error(err)
}
