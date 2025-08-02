// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"bytes"

	db "github.com/luxfi/database"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
)

var (
	inboundSmallerValuePrefix = []byte{0}
	inboundSmallerIndexPrefix = []byte{1}
	inboundLargerValuePrefix  = []byte{2}
	inboundLargerIndexPrefix  = []byte{3}

	// note that inbound and outbound have their smaller and larger values
	// swapped

	// inbound specifies the prefixes to use for inbound shared memory
	// ie. reading and deleting a message received from another chain.
	inbound = prefixes{
		smallerValuePrefix: inboundSmallerValuePrefix,
		smallerIndexPrefix: inboundSmallerIndexPrefix,
		largerValuePrefix:  inboundLargerValuePrefix,
		largerIndexPrefix:  inboundLargerIndexPrefix,
	}
	// outbound specifies the prefixes to use for outbound shared memory
	// ie. writing a message to another chain.
	outbound = prefixes{
		smallerValuePrefix: inboundLargerValuePrefix,
		smallerIndexPrefix: inboundLargerIndexPrefix,
		largerValuePrefix:  inboundSmallerValuePrefix,
		largerIndexPrefix:  inboundSmallerIndexPrefix,
	}
)

type prefixes struct {
	smallerValuePrefix []byte
	smallerIndexPrefix []byte
	largerValuePrefix  []byte
	largerIndexPrefix  []byte
}

func (p *prefixes) getValueDB(myChainID, peerChainID ids.ID, database db.Database) db.Database {
	if bytes.Compare(myChainID[:], peerChainID[:]) == -1 {
		return prefixdb.New(p.smallerValuePrefix, database)
	}
	return prefixdb.New(p.largerValuePrefix, database)
}

func (p *prefixes) getValueAndIndexDB(myChainID, peerChainID ids.ID, database db.Database) (db.Database, db.Database) {
	var valueDB, indexDB db.Database
	if bytes.Compare(myChainID[:], peerChainID[:]) == -1 {
		valueDB = prefixdb.New(p.smallerValuePrefix, database)
		indexDB = prefixdb.New(p.smallerIndexPrefix, database)
	} else {
		valueDB = prefixdb.New(p.largerValuePrefix, database)
		indexDB = prefixdb.New(p.largerIndexPrefix, database)
	}
	return valueDB, indexDB
}
