// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import "github.com/luxfi/zap"

var _ Unsigned = (*Import)(nil)

type Import struct {
	// Nonce provides internal chain replay protection
	Nonce  uint64 `json:"nonce"`
	MaxFee uint64 `json:"maxFee"`
	// Message includes the chainIDs to provide cross chain replay protection
	Message []byte `json:"message"`
}

func (i *Import) Visit(v Visitor) error {
	return v.Import(i)
}

// Marshal encodes the Import as one native ZAP object with the tx kind byte at
// offset 0.
func (i *Import) Marshal() ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + sizeImport + len(i.Message))
	ob := b.StartObject(sizeImport)
	ob.SetUint8(offKind, uint8(kindImport))
	ob.SetUint64(offImportNonce, i.Nonce)
	ob.SetUint64(offImportMaxFee, i.MaxFee)
	ob.SetBytes(offImportMessage, i.Message)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseImport(obj zap.Object) *Import {
	i := &Import{
		Nonce:  obj.Uint64(offImportNonce),
		MaxFee: obj.Uint64(offImportMaxFee),
	}
	if m := obj.Bytes(offImportMessage); len(m) > 0 {
		i.Message = append([]byte(nil), m...)
	}
	return i
}
