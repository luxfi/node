<<<<<<< HEAD
<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
=======
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
>>>>>>> 34554f662 (Update LICENSE)
>>>>>>> c5eafdb72 (Update LICENSE)
// See the file LICENSE for licensing terms.

package lux

import (
<<<<<<< HEAD
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
=======
	"context"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/utils/set"
>>>>>>> 53a8245a8 (Update consensus)
)

// TODO: Implement pruning of accepted decisions.
// To perfectly preserve the protocol, this implementation will need to store
// the hashes of all accepted decisions. It is possible to add a heuristic that
// removes sufficiently old decisions. However, that will need to be analyzed to
// ensure safety. It is doable with a weak syncrony assumption.

// Consensus represents a general lux instance that can be used directly
// to process a series of partially ordered elements.
type Consensus interface {
<<<<<<< HEAD
=======
	health.Checker

>>>>>>> 53a8245a8 (Update consensus)
	// Takes in alpha, beta1, beta2, the accepted frontier, the join statuses,
	// the mutation statuses, and the consumer statuses. If accept or reject is
	// called, the status maps should be immediately updated accordingly.
	// Assumes each element in the accepted frontier will return accepted from
	// the join status map.
<<<<<<< HEAD
=======
<<<<<<<< HEAD:snow/consensus/avalanche/consensus.go
<<<<<<< HEAD
<<<<<<< HEAD
	Initialize(context.Context, *snow.ConsensusContext, Parameters, []Vertex) error
=======
	Initialize(*snow.ConsensusContext, Parameters, []Vertex) error
>>>>>>> 95d66853a (Remove Parameters() from consensus interfaces (#2236))
=======
	Initialize(context.Context, *snow.ConsensusContext, Parameters, []Vertex) error
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
========
<<<<<<< HEAD:snow/consensus/avalanche/consensus.go
	Initialize(context.Context, *snow.ConsensusContext, Parameters, []Vertex) error
=======
>>>>>>> 53a8245a8 (Update consensus)
	Initialize(*snow.ConsensusContext, Parameters, []Vertex) error

	// Returns the parameters that describe this lux instance
	Parameters() Parameters
<<<<<<< HEAD
=======
>>>>>>> 04d685aa2 (Update consensus):snow/consensus/lux/consensus.go
>>>>>>>> 53a8245a8 (Update consensus):snow/consensus/lux/consensus.go
>>>>>>> 53a8245a8 (Update consensus)

	// Returns the number of vertices processing
	NumProcessing() int

	// Returns true if the transaction is virtuous.
	// That is, no transaction has been added that conflicts with it
	IsVirtuous(snowstorm.Tx) bool

	// Adds a new decision. Assumes the dependencies have already been added.
	// Assumes that mutations don't conflict with themselves. Returns if a
	// critical error has occurred.
<<<<<<< HEAD
	Add(Vertex) error
=======
	Add(context.Context, Vertex) error
>>>>>>> 53a8245a8 (Update consensus)

	// VertexIssued returns true iff Vertex has been added
	VertexIssued(Vertex) bool

	// TxIssued returns true if a vertex containing this transaction has been added
	TxIssued(snowstorm.Tx) bool

	// Returns the set of transaction IDs that are virtuous but not contained in
	// any preferred vertices.
<<<<<<< HEAD
	Orphans() ids.Set

	// Returns a set of vertex IDs that were virtuous at the last update.
	Virtuous() ids.Set

	// Returns a set of vertex IDs that are preferred
	Preferences() ids.Set
=======
	Orphans() set.Set[ids.ID]

	// Returns a set of vertex IDs that were virtuous at the last update.
	Virtuous() set.Set[ids.ID]

	// Returns a set of vertex IDs that are preferred
	Preferences() set.Set[ids.ID]
>>>>>>> 53a8245a8 (Update consensus)

	// RecordPoll collects the results of a network poll. If a result has not
	// been added, the result is dropped. Returns if a critical error has
	// occurred.
<<<<<<< HEAD
	RecordPoll(ids.UniqueBag) error
=======
	RecordPoll(context.Context, ids.UniqueBag) error
>>>>>>> 53a8245a8 (Update consensus)

	// Quiesce is guaranteed to return true if the instance is finalized. It
	// may, but doesn't need to, return true if all processing vertices are
	// rogue. It must return false if there is a virtuous vertex that is still
	// processing.
	Quiesce() bool

	// Finalized returns true if all transactions that have been added have been
	// finalized. Note, it is possible that after returning finalized, a new
	// decision may be added such that this instance is no longer finalized.
	Finalized() bool
<<<<<<< HEAD

	// HealthCheck returns information about the consensus health.
	HealthCheck() (interface{}, error)
=======
>>>>>>> 53a8245a8 (Update consensus)
}
