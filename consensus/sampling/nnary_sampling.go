// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampling

import (
	"fmt"

	"github.com/luxfi/ids"
)

func NewMultiSampler(choice ids.ID) MultiSampler {
	return MultiSampler{
		preference: choice,
	}
}

// MultiSampler is the implementation of a slush instance with an unbounded number
// of choices
type MultiSampler struct {
	// preference is the choice that last had a successful poll. Unless there
	// hasn't been a successful poll, in which case it is the initially provided
	// choice.
	preference ids.ID
}

func (sl *MultiSampler) Preference() ids.ID {
	return sl.preference
}

func (sl *MultiSampler) RecordSuccessfulPoll(choice ids.ID) {
	sl.preference = choice
}

func (sl *MultiSampler) String() string {
	return fmt.Sprintf("SL(Preference = %s)", sl.preference)
}
