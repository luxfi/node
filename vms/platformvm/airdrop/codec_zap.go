// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package airdrop

// ZAP-native codec for the REQL→LUX airdrop claims map.
//
// The legacy persistence path serialized a map[common.Address]*AirdropClaim
// via json/v2. That format isn't deterministic across implementations
// (map iteration order, big.Int formatting) and ties the on-disk shape to
// json/v2 — which we are ripping out of the internal data plane.
//
// Wire shape (single ZAP envelope, kind byte = airdropClaimsKind):
//
//	Kind     u8     @ 0    # = airdropClaimsKind
//	_pad     u8x3   @ 1
//	Count    u32    @ 4
//	Entries  bytes  @ 8    # concatenated ClaimEntry records (each var-len)
//
// Each ClaimEntry record (variable-tail, length-prefixed):
//
//	u32  entryLen
//	u8   entryKind             # = entryKindClaim
//	20B  EthAddress             ( @ 0..20 of payload )
//	20B  LuxAddress (ShortID)   ( @ 20..40 )
//	32B  TxID                   ( @ 40..72 )
//	i64  ClaimUnixNs            ( @ 72..80 )
//	i64  VestingEndUnixNs       ( @ 80..88 )
//	u32  amountLen              ( @ 88..92 )    big.Int big-endian bytes
//	N    amount bytes           ( @ 92..)
//
// Hard fork: airdrop state is persisted under a single db key and was
// part of the LP-023 reset window; existing dev clusters already
// re-bootstrapped with no prior claims.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

const (
	airdropClaimsKind uint8 = 1

	offEnvKind    = 0
	offEnvCount   = 4
	offEnvEntries = 8
	sizeAirdropEnvelope = 16

	airdropEntryKindClaim uint8 = 1

	claimOffEthAddress       = 0
	claimOffLuxAddress       = 20
	claimOffTxID             = 40
	claimOffClaimUnixNs      = 72
	claimOffVestingEndUnixNs = 80
	claimOffAmountLen        = 88
	claimPayloadFixed        = 92
)

// ErrCorruptAirdropBlob is returned when a parsed claims blob is
// internally inconsistent (wrong kind, payload too short, etc.).
var ErrCorruptAirdropBlob = errors.New("airdrop: corrupt claims blob")

// encodeClaims serializes a claims map to a ZAP envelope. Claims are
// emitted in lexicographic order of the eth address bytes so the
// encoding is deterministic.
func encodeClaims(claims map[common.Address]*AirdropClaim) []byte {
	addrs := make([]common.Address, 0, len(claims))
	for a := range claims {
		addrs = append(addrs, a)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return string(addrs[i][:]) < string(addrs[j][:])
	})

	var entries []byte
	for _, a := range addrs {
		entries = append(entries, encodeClaimEntry(claims[a])...)
	}

	b := zap.NewBuilder(sizeAirdropEnvelope + len(entries) + 32)
	ob := b.StartObject(sizeAirdropEnvelope)
	ob.SetUint8(offEnvKind, airdropClaimsKind)
	ob.SetUint32(offEnvCount, uint32(len(addrs)))
	ob.SetBytes(offEnvEntries, entries)
	ob.FinishAsRoot()
	return b.Finish()
}

// decodeClaims parses a ZAP envelope into a claims map.
func decodeClaims(data []byte) (map[common.Address]*AirdropClaim, error) {
	msg, err := zap.Parse(data)
	if err != nil {
		return nil, err
	}
	root := msg.Root()
	if root.Uint8(offEnvKind) != airdropClaimsKind {
		return nil, fmt.Errorf("%w: bad kind", ErrCorruptAirdropBlob)
	}

	count := root.Uint32(offEnvCount)
	entries := root.Bytes(offEnvEntries)
	out := make(map[common.Address]*AirdropClaim, count)
	for i := uint32(0); i < count; i++ {
		if len(entries) < 5 {
			return nil, fmt.Errorf("%w: entry header at %d", ErrCorruptAirdropBlob, i)
		}
		entryLen := binary.LittleEndian.Uint32(entries[:4])
		if uint32(len(entries)) < 4+entryLen {
			return nil, fmt.Errorf("%w: entry %d truncated", ErrCorruptAirdropBlob, i)
		}
		kind := entries[4]
		payload := entries[5 : 4+entryLen]
		entries = entries[4+entryLen:]

		if kind != airdropEntryKindClaim {
			return nil, fmt.Errorf("%w: unknown entry kind 0x%02x", ErrCorruptAirdropBlob, kind)
		}
		claim, addr, err := decodeClaimEntry(payload)
		if err != nil {
			return nil, err
		}
		out[addr] = claim
	}
	if len(entries) != 0 {
		return nil, fmt.Errorf("%w: %d trailing bytes", ErrCorruptAirdropBlob, len(entries))
	}
	return out, nil
}

func encodeClaimEntry(c *AirdropClaim) []byte {
	var amountBytes []byte
	if c.AmountClaimed != nil {
		amountBytes = c.AmountClaimed.Bytes()
	}
	payloadLen := claimPayloadFixed + len(amountBytes)
	out := make([]byte, 5+payloadLen)
	binary.LittleEndian.PutUint32(out[0:4], uint32(1+payloadLen))
	out[4] = airdropEntryKindClaim

	p := out[5:]
	copy(p[claimOffEthAddress:claimOffEthAddress+20], c.Address[:])
	copy(p[claimOffLuxAddress:claimOffLuxAddress+20], c.LuxAddress[:])
	copy(p[claimOffTxID:claimOffTxID+32], c.TxID[:])
	binary.LittleEndian.PutUint64(p[claimOffClaimUnixNs:], uint64(c.ClaimTime.UnixNano()))
	binary.LittleEndian.PutUint64(p[claimOffVestingEndUnixNs:], uint64(c.VestingEndTime.UnixNano()))
	binary.LittleEndian.PutUint32(p[claimOffAmountLen:], uint32(len(amountBytes)))
	copy(p[claimPayloadFixed:], amountBytes)
	return out
}

func decodeClaimEntry(p []byte) (*AirdropClaim, common.Address, error) {
	if len(p) < claimPayloadFixed {
		return nil, common.Address{}, fmt.Errorf("%w: claim payload %d < %d", ErrCorruptAirdropBlob, len(p), claimPayloadFixed)
	}
	amountLen := binary.LittleEndian.Uint32(p[claimOffAmountLen:])
	if uint32(len(p)) < uint32(claimPayloadFixed)+amountLen {
		return nil, common.Address{}, fmt.Errorf("%w: claim amount tail %d < %d", ErrCorruptAirdropBlob, len(p), uint32(claimPayloadFixed)+amountLen)
	}

	c := &AirdropClaim{
		ClaimTime:      time.Unix(0, int64(binary.LittleEndian.Uint64(p[claimOffClaimUnixNs:]))),
		VestingEndTime: time.Unix(0, int64(binary.LittleEndian.Uint64(p[claimOffVestingEndUnixNs:]))),
		AmountClaimed:  new(big.Int).SetBytes(p[claimPayloadFixed : uint32(claimPayloadFixed)+amountLen]),
	}
	copy(c.Address[:], p[claimOffEthAddress:claimOffEthAddress+20])
	copy(c.LuxAddress[:], p[claimOffLuxAddress:claimOffLuxAddress+20])
	var tx ids.ID
	copy(tx[:], p[claimOffTxID:claimOffTxID+32])
	c.TxID = tx
	return c, c.Address, nil
}
