// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/consensus/chain"
	"github.com/luxfi/node/snow/engine/snowman/block"
)

var (
	_ block.Parser  = (*parseAcceptor)(nil)
	_ chain.Block = (*blockAcceptor)(nil)
)

type parseAcceptor struct {
	parser      block.Parser
	ctx         *snow.ConsensusContext
	numAccepted prometheus.Counter
}

func (p *parseAcceptor) ParseBlock(ctx context.Context, bytes []byte) (chain.Block, error) {
	blk, err := p.parser.ParseBlock(ctx, bytes)
	if err != nil {
		return nil, err
	}
	return &blockAcceptor{
		Block:       blk,
		ctx:         p.ctx,
		numAccepted: p.numAccepted,
	}, nil
}

type blockAcceptor struct {
	chain.Block

	ctx         *snow.ConsensusContext
	numAccepted prometheus.Counter
}

func (b *blockAcceptor) Accept(ctx context.Context) error {
	if err := b.ctx.BlockAcceptor.Accept(b.ctx, b.ID(), b.Bytes()); err != nil {
		return err
	}
	err := b.Block.Accept(ctx)
	b.numAccepted.Inc()
	return err
}
