// Copyright (C) 2023-2026, Lux Industries Inc. All rights reserved.
// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

// Visitor dispatches by the canonical P-Chain block type. Under
// carries a timestamp and is one of the four canonical kinds.
type Visitor interface {
	AbortBlock(*AbortBlock) error
	CommitBlock(*CommitBlock) error
	ProposalBlock(*ProposalBlock) error
	StandardBlock(*StandardBlock) error
}
