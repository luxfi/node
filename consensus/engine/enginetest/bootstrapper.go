// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package enginetest

import (
	"context"
	"errors"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/consensus/engine/core"
)

var (
	_ core.BootstrapableEngine = (*Bootstrapper)(nil)

	errClear = errors.New("unexpectedly called Clear")
)

type Bootstrapper struct {
	Engine

	CantClear bool

	ClearF func(ctx context.Context) error
}

func (b *Bootstrapper) Default(cant bool) {
	b.Engine.Default(cant)

	b.CantClear = cant
}

func (b *Bootstrapper) Clear(ctx context.Context) error {
	if b.ClearF != nil {
		return b.ClearF(ctx)
	}
	if b.CantClear && b.T != nil {
		require.FailNow(b.T, errClear.Error())
	}
	return errClear
}
