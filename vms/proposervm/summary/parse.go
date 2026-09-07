// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"fmt"

	"github.com/luxfi/crypto/hash"
)

func Parse(bytes []byte) (StateSummary, error) {
	summary := &stateSummary{
		id:    hash.ComputeHash256Array(bytes),
		bytes: bytes,
	}
	if err := summary.unmarshal(bytes); err != nil {
		return nil, fmt.Errorf("could not unmarshal summary due to: %w", err)
	}
	return summary, nil
}
