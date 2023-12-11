// Copyright (C) 2019-2023, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/luxdefi/node/database"
	"github.com/luxdefi/node/ids"
)

type delegatorMetadata struct {
	PotentialReward uint64

	txID ids.ID
}

func parseDelegatorMetadata(bytes []byte, metadata *delegatorMetadata) error {
	var err error
	metadata.PotentialReward, err = database.ParseUInt64(bytes)
	return err
}

func writeDelegatorMetadata(db database.KeyValueWriter, metadata *delegatorMetadata) error {
	return database.PutUInt64(db, metadata.txID[:], metadata.PotentialReward)
}
