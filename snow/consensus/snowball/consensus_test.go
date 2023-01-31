// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"github.com/ava-labs/avalanchego/ids"
)

<<<<<<< HEAD
=======
var _ Consensus = (*Byzantine)(nil)

// Byzantine is a naive implementation of a multi-choice snowball instance
type Byzantine struct {
	// params contains all the configurations of a snowball instance
	params Parameters

	// Hardcode the preference
	preference ids.ID
}

func (b *Byzantine) Initialize(params Parameters, choice ids.ID) {
	b.params = params
	b.preference = choice
}

func (b *Byzantine) Parameters() Parameters      { return b.params }
func (*Byzantine) Add(choice ids.ID)             {}
func (b *Byzantine) Preference() ids.ID          { return b.preference }
func (*Byzantine) RecordPoll(votes ids.Bag) bool { return false }
func (*Byzantine) RecordUnsuccessfulPoll()       {}
func (*Byzantine) Finalized() bool               { return true }
func (b *Byzantine) String() string              { return b.preference.String() }

>>>>>>> 707ffe48f (Add UnusedReceiver linter (#2224))
var (
	Red   = ids.Empty.Prefix(0)
	Blue  = ids.Empty.Prefix(1)
	Green = ids.Empty.Prefix(2)

	_ Consensus = (*Byzantine)(nil)
)

// Byzantine is a naive implementation of a multi-choice snowball instance
type Byzantine struct {
	// Hardcode the preference
	preference ids.ID
}

func (b *Byzantine) Initialize(_ Parameters, choice ids.ID) {
	b.preference = choice
}

func (*Byzantine) Add(ids.ID) {}

func (b *Byzantine) Preference() ids.ID {
	return b.preference
}

func (*Byzantine) RecordPoll(ids.Bag) bool {
	return false
}

func (*Byzantine) RecordUnsuccessfulPoll() {}

func (*Byzantine) Finalized() bool {
	return true
}

func (b *Byzantine) String() string {
	return b.preference.String()
}
