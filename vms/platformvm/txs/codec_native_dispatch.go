// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Per-type native-ZAP bridges. Each arm maps a Go tx struct to its
// zap_native constructor (build) and accessor wrapper (parse). New arms are
// added type-by-type; when every type registered by codec.go is covered
// here, the package-level Codec flips to nativeManager and the reflection
// stack is deleted.
//
// The bridge is intentionally mechanical: build reads struct fields and
// writes them through the zap_native New* constructor; parse reads them
// back through the Wrap* accessor. There is no reflection on either side.

import (
	"fmt"

	"github.com/luxfi/node/vms/components/verify"
	zn "github.com/luxfi/proto/zap_native"
	"github.com/luxfi/zap"
)

// marshalUnsignedNative encodes a concrete UnsignedTx to its native-ZAP
// buffer. The returned bytes are a complete, self-delimiting ZAP message.
func marshalUnsignedNative(u UnsignedTx) ([]byte, error) {
	switch t := u.(type) {
	case *AdvanceTimeTx:
		return zn.NewAdvanceTimeTx(t.Time).Bytes(), nil
	case *RewardValidatorTx:
		return zn.NewRewardValidatorTx(t.TxID).Bytes(), nil
	default:
		return nil, fmt.Errorf("zap_native: unsigned tx type %T not yet bridged", u)
	}
}

// unmarshalUnsignedNative decodes the leading self-delimiting ZAP message in
// b into a concrete UnsignedTx, returning the tx and the number of bytes it
// consumed (its ZAP buffer length). Dispatch is on the TxKind discriminator
// byte at object offset 0 — no version, no slot map.
func unmarshalUnsignedNative(b []byte) (UnsignedTx, int, error) {
	n, err := zapBufferLen(b)
	if err != nil {
		return nil, 0, err
	}
	buf := b[:n]

	kind, err := txKindOf(buf)
	if err != nil {
		return nil, 0, err
	}

	switch kind {
	case zn.TxKindAdvanceTime:
		w, err := zn.WrapAdvanceTimeTx(buf)
		if err != nil {
			return nil, 0, err
		}
		tx := &AdvanceTimeTx{Time: w.Time()}
		tx.SetBytes(buf)
		return tx, n, nil

	case zn.TxKindRewardValidator:
		w, err := zn.WrapRewardValidatorTx(buf)
		if err != nil {
			return nil, 0, err
		}
		tx := &RewardValidatorTx{TxID: w.TxID()}
		tx.SetBytes(buf)
		return tx, n, nil

	default:
		return nil, 0, fmt.Errorf("zap_native: tx kind %d not yet bridged", kind)
	}
}

// txKindOf reads the TxKind discriminator from a native-ZAP tx buffer. It
// parses the ZAP header (magic/version/size checked) and returns the byte at
// object offset 0. Wrap*Tx re-checks the kind against the expected value;
// this is the pre-dispatch read.
func txKindOf(buf []byte) (zn.TxKind, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return 0, err
	}
	return zn.TxKind(msg.Root().Uint8(zn.OffsetTxKind)), nil
}

// marshalCredsNative encodes a signed tx's credential list as a standalone
// native-ZAP buffer appended after the unsigned prefix. Only reached for
// txs that actually carry credentials; proposal txs (empty Creds) never hit
// this path.
func marshalCredsNative(creds []verify.Verifiable) ([]byte, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	return nil, fmt.Errorf("zap_native: credential encoding not yet bridged (%d creds)", len(creds))
}

// unmarshalCredsNative decodes a credential-list buffer produced by
// marshalCredsNative. Symmetric with the marshal side; not yet reached for
// proposal txs.
func unmarshalCredsNative(b []byte) ([]verify.Verifiable, error) {
	return nil, fmt.Errorf("zap_native: credential decoding not yet bridged (%d bytes)", len(b))
}
