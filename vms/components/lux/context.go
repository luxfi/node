package lux

import (
	"github.com/luxfi/consensus"
)

// ContextInitializable can be initialized with a consensus context
type ContextInitializable interface {
	InitCtx(*consensus.Context)
}
