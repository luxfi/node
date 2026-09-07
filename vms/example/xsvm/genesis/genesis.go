// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

// Native ZAP wire for the xsvm genesis: the struct IS the wire. One zap object;
// the allocations are a fixed-stride list (28-byte records: 20-byte address +
// u64 balance). There is no codec, no version prefix, no slot map.
//
// Object fixed section (offsets object-relative, little-endian):
//
//	Timestamp   i64 @ 0
//	Allocations 8B  @ 8   fixed-stride list ptr (stride 28)

import (
	"encoding/binary"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/example/xsvm/block"
	"github.com/luxfi/zap"
)

const (
	offGenesisTimestamp = 0
	offGenesisAllocs    = 8
	sizeGenesis         = 16

	// Allocation record: address 20B @0, balance u64 @20.
	allocAddr    = 0
	allocBalance = 20
	allocStride  = 28

	genShortLen = 20
)

type Genesis struct {
	Timestamp   int64        `json:"timestamp"`
	Allocations []Allocation `json:"allocations"`
}

type Allocation struct {
	Address ids.ShortID `json:"address"`
	Balance uint64      `json:"balance"`
}

// Marshal encodes the genesis as one native ZAP object.
func (g *Genesis) Marshal() ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + sizeGenesis + len(g.Allocations)*allocStride)

	var allocOff, allocCount int
	if len(g.Allocations) > 0 {
		lb := b.StartList(allocStride)
		rec := make([]byte, allocStride)
		for i := range g.Allocations {
			copy(rec[allocAddr:], g.Allocations[i].Address[:])
			binary.LittleEndian.PutUint64(rec[allocBalance:], g.Allocations[i].Balance)
			lb.AddBytes(rec)
		}
		// AddBytes counts bytes, not elements — use the real element count.
		allocOff, _ = lb.Finish()
		allocCount = len(g.Allocations)
	}

	ob := b.StartObject(sizeGenesis)
	ob.SetInt64(offGenesisTimestamp, g.Timestamp)
	ob.SetList(offGenesisAllocs, allocOff, allocCount)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func Parse(bytes []byte) (*Genesis, error) {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	g := &Genesis{
		Timestamp: obj.Int64(offGenesisTimestamp),
	}
	arr := obj.ListStride(offGenesisAllocs, allocStride)
	n := arr.Len()
	if n > 0 {
		g.Allocations = make([]Allocation, n)
		for i := 0; i < n; i++ {
			e := arr.Object(i, allocStride)
			copy(g.Allocations[i].Address[:], e.BytesFixedSlice(allocAddr, genShortLen))
			g.Allocations[i].Balance = e.Uint64(allocBalance)
		}
	}
	return g, nil
}

func Block(genesis *Genesis) (*block.Stateless, error) {
	bytes, err := genesis.Marshal()
	if err != nil {
		return nil, err
	}
	return &block.Stateless{
		ParentID:  hash.ComputeHash256Array(bytes),
		Timestamp: genesis.Timestamp,
	}, nil
}
