// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

var _ Unsigned = (*Export)(nil)

type Export struct {
	// ChainID provides cross chain replay protection
	ChainID ids.ID `json:"chainID"`
	// Nonce provides internal chain replay protection
	Nonce       uint64      `json:"nonce"`
	MaxFee      uint64      `json:"maxFee"`
	PeerChainID ids.ID      `json:"peerChainID"`
	IsReturn    bool        `json:"isReturn"`
	Amount      uint64      `json:"amount"`
	To          ids.ShortID `json:"to"`
}

func (e *Export) Visit(v Visitor) error {
	return v.Export(e)
}

// Marshal encodes the Export as one native ZAP object with the tx kind byte at
// offset 0.
func (e *Export) Marshal() ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + sizeExport)
	ob := b.StartObject(sizeExport)
	ob.SetUint8(offKind, uint8(kindExport))
	ob.SetBytesFixed(offExportChainID, e.ChainID[:])
	ob.SetUint64(offExportNonce, e.Nonce)
	ob.SetUint64(offExportMaxFee, e.MaxFee)
	ob.SetBytesFixed(offExportPeerChainID, e.PeerChainID[:])
	ob.SetBool(offExportIsReturn, e.IsReturn)
	ob.SetUint64(offExportAmount, e.Amount)
	ob.SetBytesFixed(offExportTo, e.To[:])
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseExport(obj zap.Object) *Export {
	e := &Export{
		Nonce:    obj.Uint64(offExportNonce),
		MaxFee:   obj.Uint64(offExportMaxFee),
		IsReturn: obj.Bool(offExportIsReturn),
		Amount:   obj.Uint64(offExportAmount),
	}
	copy(e.ChainID[:], obj.BytesFixedSlice(offExportChainID, idLen))
	copy(e.PeerChainID[:], obj.BytesFixedSlice(offExportPeerChainID, idLen))
	copy(e.To[:], obj.BytesFixedSlice(offExportTo, shortLen))
	return e
}
