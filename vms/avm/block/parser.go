// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"fmt"
	"reflect"

	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/vms/avm/fxs"
	"github.com/luxfi/node/vms/avm/txs"
)

// CodecVersion is the current default codec version
const CodecVersion = txs.CodecVersion

var _ Parser = (*parser)(nil)

type Parser interface {
	txs.Parser

	ParseBlock(bytes []byte) (Block, error)
	ParseGenesisBlock(bytes []byte) (Block, error)

	InitializeBlock(block Block) error
	InitializeGenesisBlock(block Block) error
}

type parser struct {
	txs.Parser
}

func NewParser(fxs []fxs.Fx) (Parser, error) {
	p, err := txs.NewParser(fxs)
	if err != nil {
		return nil, err
	}
	c := p.CodecRegistry()
	gc := p.GenesisCodecRegistry()

	err = utils.Err(
		c.RegisterType(&StandardBlock{}),
		gc.RegisterType(&StandardBlock{}),
	)
	return &parser{
		Parser: p,
	}, err
}

func NewCustomParser(
	typeToFxIndex map[reflect.Type]int,
	clock *mockable.Clock,
	log logging.Logger,
	fxs []fxs.Fx,
) (Parser, error) {
	p, err := txs.NewCustomParser(typeToFxIndex, clock, log, fxs)
	if err != nil {
		return nil, err
	}
	c := p.CodecRegistry()
	gc := p.GenesisCodecRegistry()

	err = utils.Err(
		c.RegisterType(&StandardBlock{}),
		gc.RegisterType(&StandardBlock{}),
	)
	return &parser{
		Parser: p,
	}, err
}

func (p *parser) ParseBlock(bytes []byte) (Block, error) {
	return parse(p.Codec(), bytes)
}

func (p *parser) ParseGenesisBlock(bytes []byte) (Block, error) {
	return parse(p.GenesisCodec(), bytes)
}

func parse(cm codec.Manager, bytes []byte) (Block, error) {
	var blk Block
	if _, err := cm.Unmarshal(bytes, &blk); err != nil {
		return nil, err
	}
	return blk, blk.initialize(bytes, cm)
}

func (p *parser) InitializeBlock(block Block) error {
	return initialize(block, p.Codec())
}

func (p *parser) InitializeGenesisBlock(block Block) error {
	return initialize(block, p.GenesisCodec())
}

func initialize(blk Block, cm codec.Manager) error {
	// We serialize this block as a pointer so that it can be deserialized into
	// a Block
	bytes, err := cm.Marshal(CodecVersion, &blk)
	if err != nil {
		return fmt.Errorf("couldn't marshal block: %w", err)
	}
	return blk.initialize(bytes, cm)
}
