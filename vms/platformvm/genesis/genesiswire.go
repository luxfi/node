// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

// Native-ZAP wire for the P-Chain genesis blob (struct-is-wire; no codec, no
// version prefix, no slot registry). The blob is one zap object carrying the
// scalar header plus three variable-length element lists — UTXOs, validator
// txs, chain txs — each encoded as a lengths list + concatenated blob (the
// same framing the migrated block package uses for its tx lists). Embedded
// txs are stored as their own self-delimiting signed bytes and re-parsed via
// txs.Parse, so their TxID (= hash(signedBytes)) is preserved byte-for-byte.
// Re-genesis means this layout is free to be the canonical one.

import (
	"fmt"

	lux "github.com/luxfi/utxo"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/zap"
)

// Genesis object layout (size 72):
const (
	genTimestamp     = 0  // u64
	genInitialSupply = 8  // u64
	genMessage       = 16 // text ptr (8B)
	genUTXOLens      = 24 // list ptr (8B): per-UTXO blob lengths
	genUTXOBlob      = 32 // bytes ptr (8B): concatenated UTXO blobs
	genVdrLens       = 40 // list ptr (8B)
	genVdrBlob       = 48 // bytes ptr (8B)
	genChainLens     = 56 // list ptr (8B)
	genChainBlob     = 64 // bytes ptr (8B)
	genSize          = 72
	genLenStride     = 4 // uint32
)

// genesis UTXO sub-object (size 16): utxo wire bytes @0, per-UTXO message @8.
const (
	gutxoWire = 0
	gutxoMsg  = 8
	gutxoSize = 16
)

func marshalGenesis(g *Genesis) ([]byte, error) {
	utxoBlobs := make([][]byte, len(g.UTXOs))
	for i, u := range g.UTXOs {
		raw, err := marshalGenesisUTXO(u)
		if err != nil {
			return nil, fmt.Errorf("genesis: marshal utxo %d: %w", i, err)
		}
		utxoBlobs[i] = raw
	}
	vdrBlobs, err := txBlobs(g.Validators)
	if err != nil {
		return nil, fmt.Errorf("genesis: marshal validators: %w", err)
	}
	chainBlobs, err := txBlobs(g.Chains)
	if err != nil {
		return nil, fmt.Errorf("genesis: marshal chains: %w", err)
	}

	b := zap.NewBuilder(zap.HeaderSize + genSize + 1024)
	utxoLenOff, utxoLenCount, utxoBlob := writeBlobList(b, utxoBlobs)
	vdrLenOff, vdrLenCount, vdrBlob := writeBlobList(b, vdrBlobs)
	chainLenOff, chainLenCount, chainBlob := writeBlobList(b, chainBlobs)

	ob := b.StartObject(genSize)
	ob.SetUint64(genTimestamp, g.Timestamp)
	ob.SetUint64(genInitialSupply, g.InitialSupply)
	ob.SetText(genMessage, g.Message)
	ob.SetList(genUTXOLens, utxoLenOff, utxoLenCount)
	ob.SetBytes(genUTXOBlob, utxoBlob)
	ob.SetList(genVdrLens, vdrLenOff, vdrLenCount)
	ob.SetBytes(genVdrBlob, vdrBlob)
	ob.SetList(genChainLens, chainLenOff, chainLenCount)
	ob.SetBytes(genChainBlob, chainBlob)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseGenesis(genesisBytes []byte) (*Genesis, error) {
	msg, err := zap.Parse(genesisBytes)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()

	g := &Genesis{
		Timestamp:     obj.Uint64(genTimestamp),
		InitialSupply: obj.Uint64(genInitialSupply),
		Message:       obj.Text(genMessage),
	}

	utxoBlobs, err := readBlobList(obj, genUTXOLens, genUTXOBlob)
	if err != nil {
		return nil, fmt.Errorf("genesis: read utxos: %w", err)
	}
	if len(utxoBlobs) > 0 {
		g.UTXOs = make([]*UTXO, len(utxoBlobs))
		for i, raw := range utxoBlobs {
			u, err := parseGenesisUTXO(raw)
			if err != nil {
				return nil, fmt.Errorf("genesis: parse utxo %d: %w", i, err)
			}
			g.UTXOs[i] = u
		}
	}

	if g.Validators, err = parseTxBlobs(obj, genVdrLens, genVdrBlob); err != nil {
		return nil, fmt.Errorf("genesis: read validators: %w", err)
	}
	if g.Chains, err = parseTxBlobs(obj, genChainLens, genChainBlob); err != nil {
		return nil, fmt.Errorf("genesis: read chains: %w", err)
	}
	return g, nil
}

func marshalGenesisUTXO(u *UTXO) ([]byte, error) {
	wire, err := u.UTXO.WireBytes()
	if err != nil {
		return nil, err
	}
	b := zap.NewBuilder(zap.HeaderSize + gutxoSize + len(wire) + len(u.Message))
	ob := b.StartObject(gutxoSize)
	ob.SetBytes(gutxoWire, wire)
	ob.SetBytes(gutxoMsg, u.Message)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseGenesisUTXO(blob []byte) (*UTXO, error) {
	msg, err := zap.Parse(blob)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	inner, err := lux.ParseUTXO(obj.Bytes(gutxoWire))
	if err != nil {
		return nil, err
	}
	var message []byte
	if m := obj.Bytes(gutxoMsg); len(m) > 0 {
		message = append([]byte(nil), m...)
	}
	return &UTXO{UTXO: *inner, Message: message}, nil
}

// ---- shared lengths-list + blob framing ----

func txBlobs(list []*txs.Tx) ([][]byte, error) {
	if len(list) == 0 {
		return nil, nil
	}
	out := make([][]byte, len(list))
	for i, tx := range list {
		if tx == nil {
			return nil, fmt.Errorf("nil tx at index %d", i)
		}
		out[i] = tx.Bytes()
	}
	return out, nil
}

func writeBlobList(b *zap.Builder, blobs [][]byte) (lenOff, lenCount int, blob []byte) {
	if len(blobs) == 0 {
		return 0, 0, nil
	}
	lb := b.StartList(genLenStride)
	for _, raw := range blobs {
		lb.AddUint32(uint32(len(raw)))
		blob = append(blob, raw...)
	}
	lenOff, lenCount = lb.Finish()
	return lenOff, lenCount, blob
}

func readBlobList(obj zap.Object, lenPtrOff, blobPtrOff int) ([][]byte, error) {
	lengths := obj.ListStride(lenPtrOff, genLenStride)
	n := lengths.Len()
	if n == 0 {
		return nil, nil
	}
	blob := obj.Bytes(blobPtrOff)
	out := make([][]byte, n)
	cursor := 0
	for i := 0; i < n; i++ {
		size := int(lengths.Uint32(i))
		if size < 0 || cursor+size > len(blob) {
			return nil, fmt.Errorf("element %d length %d overruns blob (%d)", i, size, len(blob))
		}
		out[i] = blob[cursor : cursor+size]
		cursor += size
	}
	return out, nil
}

func parseTxBlobs(obj zap.Object, lenPtrOff, blobPtrOff int) ([]*txs.Tx, error) {
	blobs, err := readBlobList(obj, lenPtrOff, blobPtrOff)
	if err != nil {
		return nil, err
	}
	if len(blobs) == 0 {
		return nil, nil
	}
	out := make([]*txs.Tx, len(blobs))
	for i, raw := range blobs {
		tx, err := txs.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse tx %d: %w", i, err)
		}
		out[i] = tx
	}
	return out, nil
}
