// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

var _ StateSummary = (*stateSummary)(nil)

type StateSummary interface {
	ID() ids.ID
	ForkHeight() uint64
	BlockBytes() []byte
	InnerSummaryBytes() []byte
	Bytes() []byte
}

// stateSummary is zap-backed: the struct IS the wire. One fixed-shape zap
// object, no codec, no version prefix.
//
// object (size 24): Height u64 @0, Block bytes ptr @8, InnerSummary bytes ptr @16.
type stateSummary struct {
	Height uint64
	//       proposervm information. We would then modify the StateSummary
	//       interface to expose the required information to generate the full
	//       block.
	Block        []byte
	InnerSummary []byte

	id    ids.ID
	bytes []byte
}

const (
	ssHeight = 0
	ssBlock  = 8
	ssInner  = 16
	ssSize   = 24
)

func (s *stateSummary) marshal() []byte {
	b := zap.NewBuilder(zap.HeaderSize + ssSize + len(s.Block) + len(s.InnerSummary))
	ob := b.StartObject(ssSize)
	ob.SetUint64(ssHeight, s.Height)
	ob.SetBytes(ssBlock, s.Block)
	ob.SetBytes(ssInner, s.InnerSummary)
	ob.FinishAsRoot()
	return b.Finish()
}

func (s *stateSummary) unmarshal(bytes []byte) error {
	msg, err := zap.Parse(bytes)
	if err != nil {
		return err
	}
	root := msg.Root()
	s.Height = root.Uint64(ssHeight)
	if blk := root.Bytes(ssBlock); len(blk) > 0 {
		s.Block = append([]byte(nil), blk...)
	}
	if inner := root.Bytes(ssInner); len(inner) > 0 {
		s.InnerSummary = append([]byte(nil), inner...)
	}
	return nil
}

func (s *stateSummary) ID() ids.ID {
	return s.id
}

func (s *stateSummary) ForkHeight() uint64 {
	return s.Height
}

func (s *stateSummary) BlockBytes() []byte {
	return s.Block
}

func (s *stateSummary) InnerSummaryBytes() []byte {
	return s.InnerSummary
}

func (s *stateSummary) Bytes() []byte {
	return s.bytes
}
