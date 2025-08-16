// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowtest

import (
	"context"
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/choices"
)

// MockConsensus is a mock implementation of consensus operations
type MockConsensus struct {
	PreferenceF func() ids.ID
	AcceptF     func(context.Context, ids.ID) error
	RejectF     func(context.Context, ids.ID) error
}

func (c *MockConsensus) Preference() ids.ID {
	if c.PreferenceF != nil {
		return c.PreferenceF()
	}
	return ids.Empty
}

func (c *MockConsensus) Accept(ctx context.Context, id ids.ID) error {
	if c.AcceptF != nil {
		return c.AcceptF(ctx, id)
	}
	return nil
}

func (c *MockConsensus) Reject(ctx context.Context, id ids.ID) error {
	if c.RejectF != nil {
		return c.RejectF(ctx, id)
	}
	return nil
}

// MockState is a mock implementation of state operations
type MockState struct {
	GetF func(ids.ID) (choices.Status, error)
	SetF func(ids.ID, choices.Status) error
}

func (s *MockState) Get(id ids.ID) (choices.Status, error) {
	if s.GetF != nil {
		return s.GetF(id)
	}
	return choices.Unknown, errors.New("not implemented")
}

func (s *MockState) Set(id ids.ID, status choices.Status) error {
	if s.SetF != nil {
		return s.SetF(id, status)
	}
	return nil
}
