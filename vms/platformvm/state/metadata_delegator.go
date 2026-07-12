// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
)

type delegatorMetadata struct {
	PotentialReward uint64
	StakerStartTime uint64

	txID ids.ID
}

// parseDelegatorMetadata overlays metadata's persisted fields (native
// delegatorMetadata wire) from bytes. Empty bytes means nothing was persisted —
// the caller's tx-derived StakerStartTime default is kept.
func parseDelegatorMetadata(bytes []byte, metadata *delegatorMetadata) error {
	if len(bytes) == 0 {
		return nil
	}
	return unmarshalDelegatorMetadata(bytes, metadata)
}

func writeDelegatorMetadata(db database.KeyValueWriter, metadata *delegatorMetadata) error {
	metadataBytes, err := marshalDelegatorMetadata(metadata)
	if err != nil {
		return err
	}
	return db.Put(metadata.txID[:], metadataBytes)
}
