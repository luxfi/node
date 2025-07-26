// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import db "github.com/luxfi/database"

// WriteAll writes all of the batches to the underlying database of baseBatch.
// Assumes all batches have the same underlying db.
func WriteAll(baseBatch db.Batch, batches ...db.Batch) error {
	baseBatch = baseBatch.Inner()
	// Replay the inner batches onto [baseBatch] so that it includes all DB
	// operations as they would be applied to the base db.
	for _, batch := range batches {
		batch = batch.Inner()
		if err := batch.Replay(baseBatch); err != nil {
			return err
		}
	}
	// Write all of the combined operations in one atomic batch.
	return baseBatch.Write()
}
