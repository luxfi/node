// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"context"

	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/events"
	"github.com/luxdefi/luxd/utils/wrappers"
)

var _ events.Blockable = (*rejector)(nil)

type rejector struct {
	g        *Directed
	errs     *wrappers.Errs
	deps     ids.Set
	rejected bool // true if the tx has been rejected
	txID     ids.ID
}

func (r *rejector) Dependencies() ids.Set { return r.deps }

func (r *rejector) Fulfill(context.Context, ids.ID) {
	if r.rejected || r.errs.Errored() {
		return
	}
	r.rejected = true
	asSet := ids.NewSet(1)
	asSet.Add(r.txID)
	r.errs.Add(r.g.reject(asSet))
}

func (*rejector) Abandon(context.Context, ids.ID) {}
func (*rejector) Update(context.Context)          {}
