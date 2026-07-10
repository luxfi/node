// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// context_chunk_test.go — the heavy-block self-heal fix: GetContext must bound its Ancestors
// response by SERIALIZED SIZE (not just block COUNT) so it stays under the peer message cap.
//
// Benchmark-proven live bug: under 250-trader DEX load, heavy blocks made a 256-block context
// response sum to 3.4-5.7 MB > the 2 MB compressor cap; msgCreator.Ancestors FAILED to build,
// so a behind validator received NOTHING and could never resync (permanently stuck while the tip
// advanced). This pins the fix: the response is chunked to fit the budget, always serving at
// least one block so the behind node makes progress every round.

package chains

import (
	"context"
	"errors"
	"testing"
	"time"

	consensuschain "github.com/luxfi/consensus/engine/chain"
	consensusblock "github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/message"
)

// heavyBlock is a consensuschain.Block of a controlled byte size; only the methods GetContext
// touches (ID/Parent/Bytes) are overridden — the rest of the interface is embedded and unused.
type heavyBlock struct {
	consensusblock.Block
	id, parent ids.ID
	bytes      []byte
}

func (b *heavyBlock) ID() ids.ID     { return b.id }
func (b *heavyBlock) Parent() ids.ID { return b.parent }
func (b *heavyBlock) Bytes() []byte  { return b.bytes }

// sizeStubVM serves heavyBlocks by id (only GetBlock is called by GetContext).
type sizeStubVM struct {
	consensuschain.BlockBuilder
	blocks map[ids.ID]consensusblock.Block
}

func (v *sizeStubVM) GetBlock(_ context.Context, id ids.ID) (consensusblock.Block, error) {
	b, ok := v.blocks[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return b, nil
}

// sizeRecMsg records the containers GetContext hands to Ancestors, so the test can measure the
// assembled response size (the input the real zstd compressor would reject above the cap).
type sizeRecMsg struct {
	message.OutboundMsgBuilder
	containers [][]byte
}

func (m *sizeRecMsg) Ancestors(_ ids.ID, _ uint32, containers [][]byte) (message.OutboundMessage, error) {
	m.containers = containers
	return nil, nil
}

func TestGetContext_ChunksBySize_FitsUnderCap(t *testing.T) {
	// A 100-block chain of ~150 KiB blocks. Packed by COUNT alone (up to maxContextBlocks=256,
	// i.e. all 100), the response is ~15 MB — 7x over the 2 MB cap, so the old handler's message
	// failed to build. Bounded by SIZE, it serves only as many as fit.
	const nBlocks = 100
	const blkSize = 150 * 1024

	vm := &sizeStubVM{blocks: map[ids.ID]consensusblock.Block{}}
	parent := ids.Empty
	var tip ids.ID
	for i := 0; i < nBlocks; i++ {
		id := ids.GenerateTestID()
		vm.blocks[id] = &heavyBlock{id: id, parent: parent, bytes: make([]byte, blkSize)}
		parent = id
		tip = id
	}

	msg := &sizeRecMsg{}
	bh := &blockHandler{
		logger:           log.NewNoOpLogger(),
		vm:               vm,
		msgCreator:       msg,
		net:              &redStubNet{},
		chainID:          ids.GenerateTestID(),
		networkID:        ids.GenerateTestID(),
		maxContextBlocks: 256,
	}

	if err := bh.GetContext(context.Background(), ids.GenerateTestNodeID(), 1, time.Now().Add(time.Second), tip); err != nil {
		t.Fatalf("GetContext: %v", err)
	}

	total := 0
	for _, c := range msg.containers {
		total += len(c)
	}
	budget := constants.DefaultMaxMessageSize - 128*1024 // the handler's byteBudget

	if len(msg.containers) == 0 {
		t.Fatal("must serve at least one block (a behind node must always make progress)")
	}
	if total > budget {
		t.Fatalf("context response %d bytes exceeds budget %d — the real Ancestors compressor would REJECT it "+
			"(cap %d), stranding a behind validator (the live bug)", total, budget, constants.DefaultMaxMessageSize)
	}
	if len(msg.containers) >= nBlocks {
		t.Fatalf("expected SIZE truncation (fewer than %d blocks), got %d — response not chunked", nBlocks, len(msg.containers))
	}
	t.Logf("size-chunked: %d blocks, %d payload bytes (budget %d, cap %d) — fits, so the compressor accepts it",
		len(msg.containers), total, budget, constants.DefaultMaxMessageSize)
}

// A single heavy block is ALWAYS served even if it alone exceeds the budget — the walk must never
// deadlock (the trust-tiered validator cap gives such a block the send headroom; a stranger's
// tight cap correctly rejects it downstream).
func TestGetContext_SingleOversizeBlock_StillServed(t *testing.T) {
	oversize := constants.DefaultMaxMessageSize + 1<<20 // > the cap on its own
	id := ids.GenerateTestID()
	vm := &sizeStubVM{blocks: map[ids.ID]consensusblock.Block{
		id: &heavyBlock{id: id, parent: ids.Empty, bytes: make([]byte, oversize)},
	}}
	msg := &sizeRecMsg{}
	bh := &blockHandler{
		logger: log.NewNoOpLogger(), vm: vm, msgCreator: msg, net: &redStubNet{},
		chainID: ids.GenerateTestID(), networkID: ids.GenerateTestID(), maxContextBlocks: 256,
	}
	if err := bh.GetContext(context.Background(), ids.GenerateTestNodeID(), 1, time.Now().Add(time.Second), id); err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if len(msg.containers) != 1 {
		t.Fatalf("a single (even oversize) block must be served so the walk never deadlocks, got %d blocks", len(msg.containers))
	}
}
