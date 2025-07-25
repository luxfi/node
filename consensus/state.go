// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package consensus

import (
	"errors"

	"github.com/luxfi/node/proto/pb/p2p"
)

const (
	Initializing State = iota
	StateSyncing
	Bootstrapping
	NormalOp
)

var ErrUnknownState = errors.New("unknown state")

type State uint8

func (st State) String() string {
	switch st {
	case Initializing:
		return "Initializing state"
	case StateSyncing:
		return "State syncing state"
	case Bootstrapping:
		return "Bootstrapping state"
	case NormalOp:
		return "Normal operations state"
	default:
		return "Unknown state"
	}
}

type EngineState struct {
	Type  p2p.EngineType
	State State
}
