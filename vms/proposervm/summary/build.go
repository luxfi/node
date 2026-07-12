// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"github.com/luxfi/crypto/hash"
)

func Build(
	forkHeight uint64,
	block []byte,
	coreSummary []byte,
) (StateSummary, error) {
	summary := &stateSummary{
		Height:       forkHeight,
		Block:        block,
		InnerSummary: coreSummary,
	}

	summary.bytes = summary.marshal()
	summary.id = hash.ComputeHash256Array(summary.bytes)
	return summary, nil
}
