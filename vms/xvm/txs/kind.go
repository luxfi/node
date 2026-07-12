// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import "github.com/luxfi/zap"

// xkind is the 1-byte tx discriminator at object offset 0 of every X-chain
// unsigned-tx buffer. It is the whole dispatch: Parse reads it and returns the
// typed unsigned tx. There is no codec, no version, no slot map.
//
// The values are the registration order of the old linearcodec (BaseTx,
// CreateAssetTx, OperationTx, ImportTx, ExportTx) shifted by one so 0 stays a
// reserved sentinel that never appears on the wire.
type xkind uint8

const (
	xkindReserved xkind = iota
	xkindBase           // 1
	xkindCreateAsset    // 2
	xkindOperation      // 3
	xkindImport         // 4
	xkindExport         // 5
)

// offXKind is the fixed wire position of the discriminator (object offset 0).
const offXKind = 0

// xkindOf reads the discriminator from a parsed unsigned-tx buffer.
func xkindOf(msg *zap.Message) xkind {
	return xkind(msg.Root().Uint8(offXKind))
}
