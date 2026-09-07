// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/xvm/fxs"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/timer/mockable"
)

var _ Parser = (*parser)(nil)

// Parser parses both X-chain txs and blocks. It embeds the codec-free
// txs.Parser (for ParseTx / ParseGenesisTx) and adds native-ZAP block parsing.
type Parser interface {
	txs.Parser

	ParseBlock(bytes []byte) (Block, error)
	ParseGenesisBlock(bytes []byte) (Block, error)
}

type parser struct {
	txs.Parser
}

func NewParser(fxs []fxs.Fx) (Parser, error) {
	p, err := txs.NewParser(fxs)
	if err != nil {
		return nil, err
	}
	return &parser{Parser: p}, nil
}

func NewCustomParser(
	fxIndex *txs.FxIndex,
	clock *mockable.Clock,
	log log.Logger,
	fxs []fxs.Fx,
) (Parser, error) {
	p, err := txs.NewCustomParser(fxIndex, clock, log, fxs)
	if err != nil {
		return nil, err
	}
	return &parser{Parser: p}, nil
}

// ParseBlock decodes a native-ZAP X-chain block (byte-preserving).
func (*parser) ParseBlock(bytes []byte) (Block, error) {
	return parseStandardBlock(bytes)
}

// ParseGenesisBlock decodes the genesis block. Same wire as any other block —
// the genesis/standard distinction was a codec-version artifact that no longer
// exists.
func (*parser) ParseGenesisBlock(bytes []byte) (Block, error) {
	return parseStandardBlock(bytes)
}
