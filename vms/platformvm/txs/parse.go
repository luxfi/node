// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Parse dispatch + credential wire. There is no codec: Parse wraps the buffer
// zero-copy and dispatches on the 1-byte kind. A signed tx is
// unsigned_buffer ‖ creds_buffer (both self-delimiting zap messages), so the
// unsigned bytes are a genuine byte-prefix of the signed bytes.

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/zap"
)

// errCredSigsOutOfRange is returned when a credential entry names a signature
// range the buffer's signature array does not contain.
var errCredSigsOutOfRange = errors.New("txs: credential signature range is outside the signature array")

// zapLen returns the total length of the leading self-delimiting zap message
// in b (its header size field). This is the split point between the unsigned
// prefix and the credential suffix of a signed tx.
func zapLen(b []byte) (int, error) {
	if len(b) < zap.HeaderSize {
		return 0, zap.ErrBufferTooSmall
	}
	n := int(binary.LittleEndian.Uint32(b[12:16]))
	if n < zap.HeaderSize || n > len(b) {
		return 0, zap.ErrBufferTooSmall
	}
	return n, nil
}

// parseUnsigned wraps the leading zap buffer as the typed UnsignedTx (zero
// copy) by dispatching on its kind discriminator.
func parseUnsigned(buf []byte) (UnsignedTx, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return nil, err
	}
	switch k := kindOf(msg); k {
	case kindRewardValidator:
		return &RewardValidatorTx{msg: msg}, nil
	case kindBase:
		return &BaseTx{spendingTx{msg: msg}}, nil
	case kindImport:
		return &ImportTx{spendingTx{msg: msg}}, nil
	case kindExport:
		return &ExportTx{spendingTx{msg: msg}}, nil
	case kindCreateNetwork:
		return &CreateNetworkTx{spendingTx{msg: msg}}, nil
	case kindCreateChain:
		return &CreateChainTx{spendingTx{msg: msg}}, nil
	case kindTransferChainOwnership:
		return &TransferChainOwnershipTx{spendingTx{msg: msg}}, nil
	case kindRemoveChainValidator:
		return &RemoveChainValidatorTx{spendingTx{msg: msg}}, nil
	case kindTransformChain:
		return &TransformChainTx{spendingTx{msg: msg}}, nil
	case kindAddValidator:
		return &AddValidatorTx{spendingTx{msg: msg}}, nil
	case kindAddChainValidator:
		return &AddChainValidatorTx{spendingTx{msg: msg}}, nil
	case kindAddDelegator:
		return &AddDelegatorTx{spendingTx{msg: msg}}, nil
	case kindAddPermissionlessValidator:
		return &AddPermissionlessValidatorTx{spendingTx{msg: msg}}, nil
	case kindAddPermissionlessDelegator:
		return &AddPermissionlessDelegatorTx{spendingTx{msg: msg}}, nil
	case kindRegisterL1Validator:
		return &RegisterL1ValidatorTx{spendingTx{msg: msg}}, nil
	case kindSetL1ValidatorWeight:
		return &SetL1ValidatorWeightTx{spendingTx{msg: msg}}, nil
	case kindIncreaseL1ValidatorBalance:
		return &IncreaseL1ValidatorBalanceTx{spendingTx{msg: msg}}, nil
	case kindDisableL1Validator:
		return &DisableL1ValidatorTx{spendingTx{msg: msg}}, nil
	case kindConvertNetwork:
		return &ConvertNetworkTx{spendingTx{msg: msg}}, nil
	default:
		return nil, fmt.Errorf("zap: unknown tx kind %d", k)
	}
}

// credential wire: object{ credsList ptr @0, sigArray ptr @8 } (size 16).
// A cred entry (8-byte stride) is {sigStart u32, sigCount u32}, slicing into
// the shared 65-byte-blob signature array.
const (
	offCredsList = 0
	offSigArray  = 8
	credsObjSize = 16
	credEntry    = 8
)

// writeCredsBuf encodes a signed tx's credentials as a standalone zap buffer,
// appended after the unsigned prefix. Only called when Creds is non-empty.
func writeCredsBuf(creds []verify.Verifiable) ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + 128 + len(creds)*credEntry)
	var blobs [][sigLen]byte
	clb := b.StartList(credEntry)
	cursor := uint32(0)
	for i, c := range creds {
		cred, ok := c.(*secp256k1fx.Credential)
		if !ok {
			return nil, fmt.Errorf("credential %d: unsupported type %T", i, c)
		}
		var e [credEntry]byte
		putU32(e[0:], cursor)
		putU32(e[4:], uint32(len(cred.Sigs)))
		clb.AddBytes(e[:])
		blobs = append(blobs, cred.Sigs...)
		cursor += uint32(len(cred.Sigs))
	}
	// AddBytes counts bytes, not elements — use real element counts.
	credsOff, _ := clb.Finish()
	credsCount := len(creds)

	var sigOff, sigCount int
	if len(blobs) > 0 {
		slb := b.StartList(sigLen)
		for _, s := range blobs {
			slb.AddBytes(s[:])
		}
		sigOff, _ = slb.Finish()
		sigCount = len(blobs)
	}

	ob := b.StartObject(credsObjSize)
	ob.SetList(offCredsList, credsOff, credsCount)
	ob.SetList(offSigArray, sigOff, sigCount)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

// parseCredsBuf decodes a credential buffer produced by writeCredsBuf.
func parseCredsBuf(b []byte) ([]verify.Verifiable, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	credsList := obj.ListStride(offCredsList, credEntry)
	sigArr := obj.ListStride(offSigArray, sigLen)
	n := credsList.Len()
	total := uint32(sigArr.Len())
	creds := make([]verify.Verifiable, n)
	for i := 0; i < n; i++ {
		e := credsList.Object(i, credEntry)
		start, count := e.Uint32(0), e.Uint32(4)
		// The claimed range must lie inside the signature array. Both bounds
		// come off the wire: an out-of-range index yields a zero zap.Object
		// whose Uint8 dereferences a nil message, and count on its own sizes
		// the allocation below. Matches the clamp the input/output slicers in
		// spending.go apply. start <= total makes total-start safe.
		if start > total || count > total-start {
			return nil, fmt.Errorf("%w: credential %d claims signatures [%d, %d) of %d",
				errCredSigsOutOfRange, i, start, uint64(start)+uint64(count), total)
		}
		sigs := make([][sigLen]byte, count)
		for j := uint32(0); j < count; j++ {
			blob := sigArr.Object(int(start+j), sigLen)
			for k := 0; k < sigLen; k++ {
				sigs[j][k] = blob.Uint8(k)
			}
		}
		creds[i] = &secp256k1fx.Credential{Sigs: sigs}
	}
	return creds, nil
}
