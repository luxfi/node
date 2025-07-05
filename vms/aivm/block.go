// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aivm

import (
	"context"
	"errors"
	"time"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/choices"
	"github.com/luxfi/node/snow/consensus/snowman"
	"github.com/luxfi/node/utils/hashing"
)

var (
	_ snowman.Block = (*Block)(nil)

	errInvalidBlock = errors.New("invalid block")
)

// Block represents a block in the AI Chain
type Block struct {
	vm *VM

	id        ids.ID
	parentID  ids.ID
	height    uint64
	timestamp time.Time
	
	// AI-specific block data
	tasks     []*AITask
	results   []*TaskResult
	
	status    choices.Status
	bytes     []byte
}

// TaskResult represents the result of an AI task
type TaskResult struct {
	TaskID      ids.ID      `json:"taskId"`
	ExecutorID  ids.ShortID `json:"executorId"`
	Result      []byte      `json:"result"`
	Proof       []byte      `json:"proof"`
	ComputeTime uint64      `json:"computeTime"`
}

// ID implements the snowman.Block interface
func (b *Block) ID() ids.ID {
	return b.id
}

// Accept implements the snowman.Block interface
func (b *Block) Accept(context.Context) error {
	b.status = choices.Accepted
	
	// Process accepted tasks and results
	for _, task := range b.tasks {
		b.vm.taskRegistry[task.ID] = task
	}
	
	for _, result := range b.results {
		if task, exists := b.vm.taskRegistry[result.TaskID]; exists {
			task.Status = TaskCompleted
			task.Result = result.Result
			task.ProofOfWork = result.Proof
			task.CompletedAt = b.timestamp.Unix()
		}
	}
	
	// Update last accepted
	b.vm.preferredID = b.id
	
	return nil
}

// Reject implements the snowman.Block interface
func (b *Block) Reject(context.Context) error {
	b.status = choices.Rejected
	return nil
}

// Status implements the snowman.Block interface
func (b *Block) Status() choices.Status {
	return b.status
}

// Parent implements the snowman.Block interface
func (b *Block) Parent() ids.ID {
	return b.parentID
}

// Height implements the snowman.Block interface
func (b *Block) Height() uint64 {
	return b.height
}

// Timestamp implements the snowman.Block interface
func (b *Block) Timestamp() time.Time {
	return b.timestamp
}

// Verify implements the snowman.Block interface
func (b *Block) Verify(ctx context.Context) error {
	// Verify block structure
	if b.height == 0 && b.parentID != ids.Empty {
		return errInvalidBlock
	}
	
	// Verify task validity
	for _, task := range b.tasks {
		if task.ID == ids.Empty {
			return errors.New("invalid task ID")
		}
		if task.Fee == 0 {
			return errors.New("task must have non-zero fee")
		}
	}
	
	// Verify task results
	for _, result := range b.results {
		if _, exists := b.vm.taskRegistry[result.TaskID]; !exists {
			return errors.New("result for unknown task")
		}
		// TODO: Verify proof of work
	}
	
	b.status = choices.Processing
	return nil
}

// Bytes implements the snowman.Block interface
func (b *Block) Bytes() []byte {
	if b.bytes == nil {
		// TODO: Implement proper serialization
		b.bytes = hashing.ComputeHash256([]byte(b.id.String()))
	}
	return b.bytes
}