// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import (
	"fmt"
	
	"github.com/luxfi/node/vms/types"
)

// VerifyMemoFieldLength verifies that the memo field is within the allowed size
func VerifyMemoFieldLength(memo types.JSONByteSlice, isDurangoActive bool) error {
	if len(memo) > MaxMemoSize {
		return fmt.Errorf("%w: %d > %d", ErrMemoTooLarge, len(memo), MaxMemoSize)
	}
	return nil
}