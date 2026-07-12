// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

// Native ZAP wire for xsvm txs: the struct IS the wire. Each Unsigned tx is one
// zap object keyed by a 1-byte kind discriminator at object offset 0 — the
// whole dispatch. parseUnsigned reads it and returns the typed tx. There is no
// codec, no version prefix, no slot map.
//
// Object fixed sections (offsets object-relative, little-endian):
//
//	Transfer kind@0 ChainID 32B@1 Nonce u64@33 MaxFee u64@41 AssetID 32B@49
//	         Amount u64@81 To 20B@89                                    size 109
//	Export   kind@0 ChainID 32B@1 Nonce u64@33 MaxFee u64@41 PeerChainID 32B@49
//	         IsReturn bool@81 Amount u64@82 To 20B@90                   size 110
//	Import   kind@0 Nonce u64@1 MaxFee u64@9 Message bytes@17           size 25

import (
	"fmt"

	"github.com/luxfi/zap"
)

// kind is the 1-byte tx discriminator at object offset 0 of every Unsigned tx
// buffer.
type kind uint8

const (
	kindReserved kind = iota
	kindTransfer
	kindExport
	kindImport
)

// Shared id widths (ids.IDLen / ids.ShortIDLen / secp256k1.SignatureLen).
const (
	idLen    = 32
	shortLen = 20
	sigLen   = 65
)

// offKind is the fixed wire position of the discriminator (object offset 0).
const offKind = 0

// Transfer object layout.
const (
	offTransferChainID = 1
	offTransferNonce   = 33
	offTransferMaxFee  = 41
	offTransferAssetID = 49
	offTransferAmount  = 81
	offTransferTo      = 89
	sizeTransfer       = 109
)

// Export object layout.
const (
	offExportChainID     = 1
	offExportNonce       = 33
	offExportMaxFee      = 41
	offExportPeerChainID = 49
	offExportIsReturn    = 81
	offExportAmount      = 82
	offExportTo          = 90
	sizeExport           = 110
)

// Import object layout.
const (
	offImportNonce   = 1
	offImportMaxFee  = 9
	offImportMessage = 17
	sizeImport       = 25
)

// parseUnsigned reads the kind byte at object offset 0 and returns the typed
// Unsigned tx.
func parseUnsigned(bytes []byte) (Unsigned, error) {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	switch k := kind(obj.Uint8(offKind)); k {
	case kindTransfer:
		return parseTransfer(obj), nil
	case kindExport:
		return parseExport(obj), nil
	case kindImport:
		return parseImport(obj), nil
	default:
		return nil, fmt.Errorf("xsvm/tx: unknown tx kind %d", k)
	}
}
