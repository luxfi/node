// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampling

import "fmt"

func NewBinarySampler(choice int) BinarySampler {
	return BinarySampler{
		preference: choice,
	}
}

// BinarySampler is the implementation of a binary slush instance
type BinarySampler struct {
	// preference is the choice that last had a successful poll. Unless there
	// hasn't been a successful poll, in which case it is the initially provided
	// choice.
	preference int
}

func (sl *BinarySampler) Preference() int {
	return sl.preference
}

func (sl *BinarySampler) RecordSuccessfulPoll(choice int) {
	sl.preference = choice
}

func (sl *BinarySampler) String() string {
	return fmt.Sprintf("SL(Preference = %d)", sl.preference)
}
