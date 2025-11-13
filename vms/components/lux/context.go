package lux

import (
	"context"
)

// ContextInitializable can be initialized with a context
type ContextInitializable interface {
	InitCtx(context.Context)
}
