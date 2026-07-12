// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"errors"
	"fmt"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/vm/types"
	"github.com/luxfi/zap"
)

var (
	ErrInvalidChainID = errors.New("invalid chain ID")
	ErrInvalidWeight  = errors.New("invalid weight")
	ErrInvalidNodeID  = errors.New("invalid node ID")
	ErrInvalidOwner   = errors.New("invalid owner")
)

type PChainOwner struct {
	// The threshold number of `Addresses` that must provide a signature in
	// order for the `PChainOwner` to be considered valid.
	Threshold uint32 `json:"threshold"`
	// The addresses that are allowed to sign to authenticate a `PChainOwner`.
	Addresses []ids.ShortID `json:"addresses"`
}

// RegisterL1Validator adds a validator to the chain.
//
// Wire: mkind u8 @0, ChainID 32B @1, BLSPublicKey 48B @33, Expiry u64 @81,
// Weight u64 @89, NodeID bytes @97, RemainingBalanceOwner{threshold u32 @105,
// addrs 20B-stride list @109}, DisableOwner{threshold u32 @117, addrs @121}
// = 129-byte object header.
type RegisterL1Validator struct {
	payload

	ChainID               ids.ID                 `json:"chainID"`
	NodeID                types.JSONByteSlice    `json:"nodeID"`
	BLSPublicKey          [bls.PublicKeyLen]byte `json:"blsPublicKey"`
	Expiry                uint64                 `json:"expiry"`
	RemainingBalanceOwner PChainOwner            `json:"remainingBalanceOwner"`
	DisableOwner          PChainOwner            `json:"disableOwner"`
	Weight                uint64                 `json:"weight"`
}

const (
	rvOffChainID      = 1
	rvOffBLSKey       = 33
	rvOffExpiry       = 81
	rvOffWeight       = 89
	rvOffNodeID       = 97
	rvOffRemThreshold = 105
	rvOffRemAddrs     = 109
	rvOffDisThreshold = 117
	rvOffDisAddrs     = 121
	rvSize            = 129

	rvAddrStride = 20 // ids.ShortIDLen
)

func writePChainOwnerAddrs(b *zap.Builder, addrs []ids.ShortID) (off, count int) {
	if len(addrs) == 0 {
		return 0, 0
	}
	lb := b.StartList(rvAddrStride)
	for _, a := range addrs {
		lb.AddBytes(a[:])
	}
	off, _ = lb.Finish()
	return off, len(addrs) // AddBytes counts bytes, not elements
}

func readPChainOwner(root zap.Object, thresholdOff, addrsOff int) PChainOwner {
	list := root.ListStride(addrsOff, rvAddrStride)
	n := list.Len()
	var addrs []ids.ShortID
	if n > 0 {
		addrs = make([]ids.ShortID, n)
		for i := 0; i < n; i++ {
			copy(addrs[i][:], list.Object(i, rvAddrStride).BytesFixedSlice(0, rvAddrStride))
		}
	}
	return PChainOwner{
		Threshold: root.Uint32(thresholdOff),
		Addresses: addrs,
	}
}

func (r *RegisterL1Validator) Verify() error {
	if r.ChainID == constants.PrimaryNetworkID {
		return ErrInvalidChainID
	}
	if r.Weight == 0 {
		return ErrInvalidWeight
	}

	nodeID, err := ids.ToNodeID(r.NodeID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidNodeID, err)
	}
	if nodeID == ids.EmptyNodeID {
		return fmt.Errorf("%w: empty nodeID is disallowed", ErrInvalidNodeID)
	}

	err = verify.All(
		&secp256k1fx.OutputOwners{
			Threshold: r.RemainingBalanceOwner.Threshold,
			Addrs:     r.RemainingBalanceOwner.Addresses,
		},
		&secp256k1fx.OutputOwners{
			Threshold: r.DisableOwner.Threshold,
			Addrs:     r.DisableOwner.Addresses,
		},
	)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidOwner, err)
	}
	return nil
}

func (r *RegisterL1Validator) ValidationID() ids.ID {
	return hash.ComputeHash256Array(r.Bytes())
}

// NewRegisterL1Validator creates a new initialized RegisterL1Validator.
func NewRegisterL1Validator(
	chainID ids.ID,
	nodeID ids.NodeID,
	blsPublicKey [bls.PublicKeyLen]byte,
	expiry uint64,
	remainingBalanceOwner PChainOwner,
	disableOwner PChainOwner,
	weight uint64,
) (*RegisterL1Validator, error) {
	msg := &RegisterL1Validator{
		ChainID:               chainID,
		NodeID:                nodeID[:],
		BLSPublicKey:          blsPublicKey,
		Expiry:                expiry,
		RemainingBalanceOwner: remainingBalanceOwner,
		DisableOwner:          disableOwner,
		Weight:                weight,
	}
	b := zap.NewBuilder(zap.HeaderSize + rvSize +
		(len(remainingBalanceOwner.Addresses)+len(disableOwner.Addresses))*rvAddrStride + 64)
	remOff, remCount := writePChainOwnerAddrs(b, remainingBalanceOwner.Addresses)
	disOff, disCount := writePChainOwnerAddrs(b, disableOwner.Addresses)
	ob := b.StartObject(rvSize)
	ob.SetUint8(offMKind, uint8(mkindRegisterL1Validator))
	ob.SetBytesFixed(rvOffChainID, chainID[:])
	ob.SetBytesFixed(rvOffBLSKey, blsPublicKey[:])
	ob.SetUint64(rvOffExpiry, expiry)
	ob.SetUint64(rvOffWeight, weight)
	ob.SetBytes(rvOffNodeID, nodeID[:])
	ob.SetUint32(rvOffRemThreshold, remainingBalanceOwner.Threshold)
	ob.SetList(rvOffRemAddrs, remOff, remCount)
	ob.SetUint32(rvOffDisThreshold, disableOwner.Threshold)
	ob.SetList(rvOffDisAddrs, disOff, disCount)
	ob.FinishAsRoot()
	msg.initialize(b.Finish())
	return msg, nil
}

func parseRegisterL1Validator(root zap.Object) *RegisterL1Validator {
	msg := &RegisterL1Validator{
		ChainID:               readID(root, rvOffChainID),
		NodeID:                append([]byte(nil), root.Bytes(rvOffNodeID)...),
		Expiry:                root.Uint64(rvOffExpiry),
		Weight:                root.Uint64(rvOffWeight),
		RemainingBalanceOwner: readPChainOwner(root, rvOffRemThreshold, rvOffRemAddrs),
		DisableOwner:          readPChainOwner(root, rvOffDisThreshold, rvOffDisAddrs),
	}
	copy(msg.BLSPublicKey[:], root.BytesFixedSlice(rvOffBLSKey, bls.PublicKeyLen))
	return msg
}

// ParseRegisterL1Validator parses bytes into an initialized
// RegisterL1Validator.
func ParseRegisterL1Validator(b []byte) (*RegisterL1Validator, error) {
	payloadIntf, err := Parse(b)
	if err != nil {
		return nil, err
	}
	payload, ok := payloadIntf.(*RegisterL1Validator)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrWrongType, payloadIntf)
	}
	return payload, nil
}
