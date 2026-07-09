// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// LP-023 native-ZAP P-chain tx wire.
//
// This is the replacement for the reflection codec stack
// (pcodecs -> proto/zap_codec -> zapcodec, and the historical
// codec/linearcodec/reflectcodec it descended from). There is NO marshal
// step: each Go tx struct is bridged to its zap_native buffer via fixed-
// offset writes (build) and offset reads (parse). No serialize-tag walk,
// no reflect, no version dispatch — LP-023 mandates a single wire (native
// ZAP) from genesis, so the codec-version argument carried by the legacy
// codec.Manager interface is accepted for source-compatibility with the
// ~70 existing call sites and then ignored.
//
// Signed-tx envelope: a ZAP message is self-delimiting (its total size is
// stored in the 16-byte header at bytes[12:16]). The signed wire is
// therefore the concatenation
//
//	unsigned_zap_buffer ‖ creds_zap_buffer
//
// which makes the unsigned bytes a genuine byte-prefix of the signed bytes
// — exactly the invariant tx.go's Initialize / Sign / Parse rely on
// (`unsignedBytes = signedBytes[:unsignedLen]`), so the tx envelope logic
// needs no change. TxID = hash(signedBytes) with no re-encoding.
//
// nativeManager implements pcodecs.Manager. It is wired incrementally: as
// each tx type's bridge lands (marshalUnsigned + unmarshalUnsigned arms)
// its round-trip test goes green; the package-level Codec is flipped from
// the reflection manager to nativeManager only once every registered type
// is covered.

import (
	"encoding/binary"
	"fmt"

	"github.com/luxfi/node/vms/pcodecs"
	"github.com/luxfi/proto/zap_codec"
	"github.com/luxfi/zap"
)

// nativeManager is the native-ZAP implementation of the pcodecs.Manager
// (codec.Manager) surface. It holds no reflection codec, no registry, and
// no per-version slot map — the tx kind travels in the wire (TxKind byte
// at object offset 0) and dispatch is a type switch, not a reflect walk.
type nativeManager struct{}

// compile-time assertion that the native manager satisfies the interface
// every txs.Codec call site depends on.
var _ pcodecs.Manager = nativeManager{}

// RegisterCodec is a no-op: there are no inner per-version codecs to
// register. Retained to satisfy the pcodecs.Manager interface during the
// migration; callers stop invoking it once codec.go is fully cut over.
func (nativeManager) RegisterCodec(uint16, zap_codec.Codec) error { return nil }

// Marshal encodes source into native-ZAP wire bytes. The version argument
// is ignored (LP-023: one wire). source is either a *Tx (signed: unsigned
// ‖ creds), a *UnsignedTx (pointer to the interface, as produced by
// `&tx.Unsigned`), or a concrete UnsignedTx.
func (nativeManager) Marshal(_ uint16, source interface{}) ([]byte, error) {
	switch v := source.(type) {
	case *Tx:
		return marshalSignedNative(v)
	case *UnsignedTx:
		return marshalUnsignedNative(*v)
	case UnsignedTx:
		return marshalUnsignedNative(v)
	default:
		return nil, fmt.Errorf("zap_native: cannot marshal %T (not a *Tx or UnsignedTx)", source)
	}
}

// Unmarshal decodes native-ZAP wire bytes into dest, returning the wire
// version. dest is either a *Tx or a *UnsignedTx. The returned version is
// always CodecVersion — there is one wire.
func (nativeManager) Unmarshal(b []byte, dest interface{}) (uint16, error) {
	switch d := dest.(type) {
	case *Tx:
		return CodecVersion, unmarshalSignedNative(b, d)
	case *UnsignedTx:
		u, _, err := unmarshalUnsignedNative(b)
		if err != nil {
			return 0, err
		}
		*d = u
		return CodecVersion, nil
	default:
		return 0, fmt.Errorf("zap_native: cannot unmarshal into %T (not a *Tx or *UnsignedTx)", dest)
	}
}

// Size returns the on-wire length of value. It is len(Marshal(value)) —
// native ZAP has no separate sizing pass, and the buffers are small and
// built once, so a marshal-and-measure is both correct and cheap relative
// to the per-block finality budget.
func (m nativeManager) Size(version uint16, value interface{}) (int, error) {
	b, err := m.Marshal(version, value)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

// zapBufferLen returns the total byte length of the leading self-delimiting
// ZAP message in b (its header size field, bytes[12:16] little-endian). This
// is the split point between the unsigned prefix and the credentials tail of
// a signed tx. Returns an error if b is too short to hold a ZAP header or the
// declared size overruns the buffer.
func zapBufferLen(b []byte) (int, error) {
	if len(b) < zap.HeaderSize {
		return 0, zap.ErrBufferTooSmall
	}
	n := int(binary.LittleEndian.Uint32(b[12:16]))
	if n < zap.HeaderSize || n > len(b) {
		return 0, zap.ErrBufferTooSmall
	}
	return n, nil
}

// marshalSignedNative encodes a signed *Tx as unsigned ‖ creds. With no
// credentials (proposal txs: AdvanceTime, RewardValidator) the signed bytes
// equal the unsigned buffer, so the prefix invariant is trivially satisfied.
func marshalSignedNative(tx *Tx) ([]byte, error) {
	unsigned, err := marshalUnsignedNative(tx.Unsigned)
	if err != nil {
		return nil, err
	}
	if len(tx.Creds) == 0 {
		return unsigned, nil
	}
	creds, err := marshalCredsNative(tx.Creds)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(unsigned)+len(creds))
	out = append(out, unsigned...)
	out = append(out, creds...)
	return out, nil
}

// unmarshalSignedNative splits b at the unsigned ZAP buffer boundary,
// decodes the unsigned body, decodes any credential tail, and binds the
// exact bytes onto tx (so TxID = hash(signedBytes) with no re-encoding).
func unmarshalSignedNative(b []byte, tx *Tx) error {
	unsigned, n, err := unmarshalUnsignedNative(b)
	if err != nil {
		return err
	}
	tx.Unsigned = unsigned
	if len(b) > n {
		creds, err := unmarshalCredsNative(b[n:])
		if err != nil {
			return err
		}
		tx.Creds = creds
	} else {
		tx.Creds = nil
	}
	tx.SetBytes(b[:n], b)
	return nil
}
