// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowtest

import (
	"context"
	"errors"
	"fmt"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/snow/choices"
)

var (
	_ snow.Decidable = (*Decidable)(nil)

	ErrInvalidStateTransition = errors.New("invalid state transition")
)

type Decidable struct {
	IDV     ids.ID
	AcceptV error
	RejectV error
	StatusV Status
}

func (d *Decidable) ID() ids.ID {
	return d.IDV
}

func (d *Decidable) Status() choices.Status {
	return choices.Status(d.StatusV)
}

func (d *Decidable) Accept(context.Context) error {
	if d.StatusV == Rejected {
		return fmt.Errorf("%w from %s to %s",
			ErrInvalidStateTransition,
			Rejected,
			Accepted,
		)
	}

	d.StatusV = Accepted
	return d.AcceptV
}

func (d *Decidable) Reject(context.Context) error {
	if d.StatusV == Accepted {
		return fmt.Errorf("%w from %s to %s",
			ErrInvalidStateTransition,
			Accepted,
			Rejected,
		)
	}

	d.StatusV = Rejected
	return d.RejectV
}
