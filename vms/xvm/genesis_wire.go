// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"fmt"

	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/zap"
)

// Native-ZAP genesis wire. The X-Chain genesis is a list of assets; each asset
// is an alias string paired with its CreateAssetTx unsigned bytes (the struct
// is the source of truth, marshalled via txs.UnsignedBytes). There is no
// pcodecs.Manager, no reflection — the same struct-is-wire discipline as the
// tx layer.
//
// Object layout:
//
//	Count       u32  @ 0
//	AliasLens   list @ 4    (u32 per asset — alias byte length)
//	AliasBlob   bytes @ 12  (concatenated alias bytes)
//	TxLens      list @ 20   (u32 per asset — unsigned-tx byte length)
//	TxBlob      bytes @ 28  (concatenated CreateAssetTx unsigned bytes)
const (
	genOffCount     = 0
	genOffAliasLens = 4
	genOffAliasBlob = 12
	genOffTxLens    = 20
	genOffTxBlob    = 28
	genSize         = 36
)

// marshalGenesis encodes the Genesis to its canonical native-ZAP bytes.
func marshalGenesis(g *Genesis) ([]byte, error) {
	var aliasBlob, txBlob []byte
	aliasLens := make([]uint32, 0, len(g.Txs))
	txLens := make([]uint32, 0, len(g.Txs))
	for i := range g.Txs {
		ga := g.Txs[i]
		ub, err := txs.UnsignedBytes(&ga.CreateAssetTx)
		if err != nil {
			return nil, fmt.Errorf("marshal genesis asset %d (%s): %w", i, ga.Alias, err)
		}
		aliasLens = append(aliasLens, uint32(len(ga.Alias)))
		aliasBlob = append(aliasBlob, ga.Alias...)
		txLens = append(txLens, uint32(len(ub)))
		txBlob = append(txBlob, ub...)
	}

	b := zap.NewBuilder(zap.HeaderSize + genSize + len(aliasBlob) + len(txBlob) + 8*len(g.Txs) + 64)
	aliasLensOff := writeU32List(b, aliasLens)
	txLensOff := writeU32List(b, txLens)

	ob := b.StartObject(genSize)
	ob.SetUint32(genOffCount, uint32(len(g.Txs)))
	ob.SetList(genOffAliasLens, aliasLensOff, len(aliasLens))
	ob.SetBytes(genOffAliasBlob, aliasBlob)
	ob.SetList(genOffTxLens, txLensOff, len(txLens))
	ob.SetBytes(genOffTxBlob, txBlob)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

// parseGenesis decodes canonical genesis bytes into a *Genesis with each
// GenesisAsset's embedded CreateAssetTx re-hydrated from its unsigned bytes.
func parseGenesis(genesisBytes []byte) (*Genesis, error) {
	msg, err := zap.Parse(genesisBytes)
	if err != nil {
		return nil, fmt.Errorf("parse xvm genesis: %w", err)
	}
	if msg.Size() != len(genesisBytes) {
		return nil, fmt.Errorf("parse xvm genesis: trailing bytes (non-canonical)")
	}
	root := msg.Root()
	n := int(root.Uint32(genOffCount))
	aliasLens := readU32List(root, genOffAliasLens)
	aliasBlob := root.Bytes(genOffAliasBlob)
	txLens := readU32List(root, genOffTxLens)
	txBlob := root.Bytes(genOffTxBlob)
	if len(aliasLens) != n || len(txLens) != n {
		return nil, fmt.Errorf("parse xvm genesis: asset count mismatch")
	}

	g := &Genesis{Txs: make([]*GenesisAsset, 0, n)}
	aPos, tPos := 0, 0
	for i := 0; i < n; i++ {
		aLen, tLen := int(aliasLens[i]), int(txLens[i])
		if aPos+aLen > len(aliasBlob) || tPos+tLen > len(txBlob) {
			return nil, fmt.Errorf("parse xvm genesis: asset %d out of bounds", i)
		}
		alias := string(aliasBlob[aPos : aPos+aLen])
		aPos += aLen
		ub := txBlob[tPos : tPos+tLen]
		tPos += tLen

		unsigned, err := txs.ParseUnsignedTx(ub)
		if err != nil {
			return nil, fmt.Errorf("hydrate genesis asset %d (%s): %w", i, alias, err)
		}
		cat, ok := unsigned.(*txs.CreateAssetTx)
		if !ok {
			return nil, fmt.Errorf("genesis asset %d (%s): expected CreateAssetTx, got %T", i, alias, unsigned)
		}
		ga := &GenesisAsset{Alias: alias, CreateAssetTx: *cat}
		g.Txs = append(g.Txs, ga)
	}
	return g, nil
}

func writeU32List(b *zap.Builder, xs []uint32) int {
	lb := b.StartList(4)
	for _, x := range xs {
		lb.AddUint32(x)
	}
	off, _ := lb.Finish()
	return off
}

func readU32List(o zap.Object, ptrOff int) []uint32 {
	l := o.ListStride(ptrOff, 4)
	n := l.Len()
	out := make([]uint32, n)
	for i := 0; i < n; i++ {
		out[i] = l.Uint32(i)
	}
	return out
}
