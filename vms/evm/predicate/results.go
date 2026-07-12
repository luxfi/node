// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

// Native ZAP wire for predicate results: the nested map IS the wire. There is
// no codec, no version prefix, no slot map. The value is a
// map[txHash]map[precompileAddr]resultBytes; each level is encoded as a
// (u32 per-entry length list, concatenated blob) so a variable number of
// variable-length children packs into one zap object. Map keys are sorted by
// their raw bytes before encoding so the output is deterministic — required
// because these results are committed as part of block building/verification.
//
// Object fixed sections (offsets object-relative, little-endian):
//
//	root     txLenList ptr @0, txBlob ptr @8                          size 16
//	txEntry  hash 32B @0, addrLenList ptr @32, addrBlob ptr @40       size 48
//	addrEntry addr 20B @0, result bytes ptr @20                       size 28

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/luxfi/geth/common"

	"github.com/luxfi/math/set"
	"github.com/luxfi/zap"
)

const (
	// addrEntry: precompile address + its result bytes.
	prAddrAddr  = 0
	prAddrBytes = 20
	prAddrSize  = 28

	// txEntry: tx hash + the per-address length list and blob.
	prTxHash     = 0
	prTxAddrLen  = 32
	prTxAddrBlob = 40
	prTxSize     = 48

	// root: the per-tx length list and blob.
	prRootTxLen  = 0
	prRootTxBlob = 8
	prRootSize   = 16

	prLenStride = 4 // uint32 per-entry length
)

type (
	// PrecompileResults is a map of results for each precompile address to the
	// resulting bitset.
	PrecompileResults map[common.Address]set.Bits

	// BlockResults maps the transactions in a block to their precompile
	// results.
	BlockResults map[common.Hash]PrecompileResults

	// encodedBlockResults is used to serialize and deserialize BlockResults.
	encodedBlockResults map[common.Hash]map[common.Address][]byte
)

// ParseBlockResults parses bytes into predicate results.
func ParseBlockResults(b []byte) (BlockResults, error) {
	encodedResults, err := parseEncodedBlockResults(b)
	if err != nil {
		return BlockResults{}, fmt.Errorf("failed to unmarshal predicate results: %w", err)
	}

	// Convert encoded representation into in-memory representation
	results := make(BlockResults, len(encodedResults))
	for txHash, addrToBytes := range encodedResults {
		decoded := make(PrecompileResults, len(addrToBytes))
		for addr, bs := range addrToBytes {
			decoded[addr] = set.BitsFromBytes(bs)
		}
		results[txHash] = decoded
	}

	return results, nil
}

// Get returns the predicate results for txHash from precompile address.
func (b *BlockResults) Get(txHash common.Hash, address common.Address) set.Bits {
	if result, ok := (*b)[txHash][address]; ok {
		return result
	}
	return set.NewBits()
}

// Set sets the predicate results for the given txHash. Results are overwritten,
// not merged.
func (b *BlockResults) Set(txHash common.Hash, txResults PrecompileResults) {
	if len(txResults) == 0 {
		delete(*b, txHash)
		return
	}

	if *b == nil {
		*b = make(map[common.Hash]PrecompileResults)
	}
	(*b)[txHash] = txResults
}

// Bytes marshals the predicate results.
func (b *BlockResults) Bytes() ([]byte, error) {
	// Convert to results representation before marshaling to avoid serializing
	// set.Bits directly, which is not supported by the native wire.
	results := make(encodedBlockResults, len(*b))
	for txHash, addrToBits := range *b {
		encoded := make(map[common.Address][]byte, len(addrToBits))
		for addr, bits := range addrToBits {
			encoded[addr] = bits.Bytes()
		}
		results[txHash] = encoded
	}

	return marshalEncodedBlockResults(results), nil
}

// marshalEncodedBlockResults encodes the nested map as one native ZAP object
// tree. Tx hashes are sorted so the output is byte-deterministic.
func marshalEncodedBlockResults(results encodedBlockResults) []byte {
	b := zap.NewBuilder(zap.HeaderSize + prRootSize + 256)

	hashes := make([]common.Hash, 0, len(results))
	for h := range results {
		hashes = append(hashes, h)
	}
	slices.SortFunc(hashes, func(a, b common.Hash) int { return bytes.Compare(a[:], b[:]) })

	var (
		txBlob   []byte
		lenOff   int
		lenCount int
	)
	if len(hashes) > 0 {
		lb := b.StartList(prLenStride)
		for _, h := range hashes {
			entry := marshalTxEntry(h, results[h])
			lb.AddUint32(uint32(len(entry)))
			txBlob = append(txBlob, entry...)
		}
		lenOff, lenCount = lb.Finish()
	}

	ob := b.StartObject(prRootSize)
	ob.SetList(prRootTxLen, lenOff, lenCount)
	ob.SetBytes(prRootTxBlob, txBlob)
	ob.FinishAsRoot()
	return b.Finish()
}

// marshalTxEntry encodes one (txHash → addr map) as its own zap object.
// Addresses are sorted for determinism.
func marshalTxEntry(hash common.Hash, addrToBytes map[common.Address][]byte) []byte {
	b := zap.NewBuilder(zap.HeaderSize + prTxSize + 128)

	addrs := make([]common.Address, 0, len(addrToBytes))
	for a := range addrToBytes {
		addrs = append(addrs, a)
	}
	slices.SortFunc(addrs, func(a, b common.Address) int { return bytes.Compare(a[:], b[:]) })

	var (
		addrBlob []byte
		lenOff   int
		lenCount int
	)
	if len(addrs) > 0 {
		lb := b.StartList(prLenStride)
		for _, a := range addrs {
			entry := marshalAddrEntry(a, addrToBytes[a])
			lb.AddUint32(uint32(len(entry)))
			addrBlob = append(addrBlob, entry...)
		}
		lenOff, lenCount = lb.Finish()
	}

	ob := b.StartObject(prTxSize)
	ob.SetBytesFixed(prTxHash, hash[:])
	ob.SetList(prTxAddrLen, lenOff, lenCount)
	ob.SetBytes(prTxAddrBlob, addrBlob)
	ob.FinishAsRoot()
	return b.Finish()
}

// marshalAddrEntry encodes one (precompile address → result bytes) as its own
// zap object.
func marshalAddrEntry(addr common.Address, result []byte) []byte {
	b := zap.NewBuilder(zap.HeaderSize + prAddrSize + len(result))
	ob := b.StartObject(prAddrSize)
	ob.SetBytesFixed(prAddrAddr, addr[:])
	ob.SetBytes(prAddrBytes, result)
	ob.FinishAsRoot()
	return b.Finish()
}

func parseEncodedBlockResults(bs []byte) (encodedBlockResults, error) {
	zmsg, err := zap.Parse(bs)
	if err != nil {
		return nil, err
	}
	root := zmsg.Root()
	lengths := root.ListStride(prRootTxLen, prLenStride)
	n := lengths.Len()
	results := make(encodedBlockResults, n)
	if n == 0 {
		return results, nil
	}
	blob := root.Bytes(prRootTxBlob)
	cursor := 0
	for i := 0; i < n; i++ {
		size := int(lengths.Uint32(i))
		if cursor+size > len(blob) {
			return nil, fmt.Errorf("predicate: tx entry %d length %d overruns blob (%d)", i, size, len(blob))
		}
		hash, addrMap, err := parseTxEntry(blob[cursor : cursor+size])
		if err != nil {
			return nil, err
		}
		results[hash] = addrMap
		cursor += size
	}
	return results, nil
}

func parseTxEntry(bs []byte) (common.Hash, map[common.Address][]byte, error) {
	zmsg, err := zap.Parse(bs)
	if err != nil {
		return common.Hash{}, nil, err
	}
	obj := zmsg.Root()
	var hash common.Hash
	copy(hash[:], obj.BytesFixedSlice(prTxHash, len(hash)))

	lengths := obj.ListStride(prTxAddrLen, prLenStride)
	n := lengths.Len()
	addrMap := make(map[common.Address][]byte, n)
	if n == 0 {
		return hash, addrMap, nil
	}
	blob := obj.Bytes(prTxAddrBlob)
	cursor := 0
	for i := 0; i < n; i++ {
		size := int(lengths.Uint32(i))
		if cursor+size > len(blob) {
			return common.Hash{}, nil, fmt.Errorf("predicate: addr entry %d length %d overruns blob (%d)", i, size, len(blob))
		}
		addr, result, err := parseAddrEntry(blob[cursor : cursor+size])
		if err != nil {
			return common.Hash{}, nil, err
		}
		addrMap[addr] = result
		cursor += size
	}
	return hash, addrMap, nil
}

func parseAddrEntry(bs []byte) (common.Address, []byte, error) {
	zmsg, err := zap.Parse(bs)
	if err != nil {
		return common.Address{}, nil, err
	}
	obj := zmsg.Root()
	var addr common.Address
	copy(addr[:], obj.BytesFixedSlice(prAddrAddr, len(addr)))
	result := append([]byte(nil), obj.Bytes(prAddrBytes)...)
	return addr, result, nil
}
