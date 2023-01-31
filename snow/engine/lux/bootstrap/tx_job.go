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

package bootstrap

import (
<<<<<<< HEAD
=======
	"context"
>>>>>>> 53a8245a8 (Update consensus)
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

<<<<<<< HEAD
=======
<<<<<<< HEAD:snow/engine/avalanche/bootstrap/tx_job.go
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowstorm"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common/queue"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
=======
>>>>>>> 53a8245a8 (Update consensus)
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/choices"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
	"github.com/luxdefi/luxd/snow/engine/lux/vertex"
	"github.com/luxdefi/luxd/snow/engine/common/queue"
	"github.com/luxdefi/luxd/utils/logging"
<<<<<<< HEAD
=======
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/bootstrap/tx_job.go
>>>>>>> 53a8245a8 (Update consensus)
)

var errMissingTxDependenciesOnAccept = errors.New("attempting to accept a transaction with missing dependencies")

type txParser struct {
	log                     logging.Logger
	numAccepted, numDropped prometheus.Counter
	vm                      vertex.DAGVM
}

<<<<<<< HEAD
func (p *txParser) Parse(txBytes []byte) (queue.Job, error) {
	tx, err := p.vm.ParseTx(txBytes)
=======
func (p *txParser) Parse(ctx context.Context, txBytes []byte) (queue.Job, error) {
	tx, err := p.vm.ParseTx(ctx, txBytes)
>>>>>>> 53a8245a8 (Update consensus)
	if err != nil {
		return nil, err
	}
	return &txJob{
		log:         p.log,
		numAccepted: p.numAccepted,
		numDropped:  p.numDropped,
		tx:          tx,
	}, nil
}

type txJob struct {
	log                     logging.Logger
	numAccepted, numDropped prometheus.Counter
	tx                      snowstorm.Tx
}

<<<<<<< HEAD
func (t *txJob) ID() ids.ID { return t.tx.ID() }
func (t *txJob) MissingDependencies() (ids.Set, error) {
	missing := ids.Set{}
=======
func (t *txJob) ID() ids.ID {
	return t.tx.ID()
}

<<<<<<< HEAD
<<<<<<< HEAD
<<<<<<< HEAD
func (t *txJob) MissingDependencies(context.Context) (set.Set[ids.ID], error) {
	missing := set.Set[ids.ID]{}
=======
func (t *txJob) MissingDependencies() (ids.Set, error) {
=======
func (t *txJob) MissingDependencies(context.Context) (ids.Set, error) {
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
	missing := ids.Set{}
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
func (t *txJob) MissingDependencies(context.Context) (set.Set[ids.ID], error) {
	missing := set.Set[ids.ID]{}
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
>>>>>>> 53a8245a8 (Update consensus)
	deps, err := t.tx.Dependencies()
	if err != nil {
		return missing, err
	}
	for _, dep := range deps {
		if dep.Status() != choices.Accepted {
			missing.Add(dep.ID())
		}
	}
	return missing, nil
}

// Returns true if this tx job has at least 1 missing dependency
<<<<<<< HEAD
func (t *txJob) HasMissingDependencies() (bool, error) {
=======
func (t *txJob) HasMissingDependencies(context.Context) (bool, error) {
>>>>>>> 53a8245a8 (Update consensus)
	deps, err := t.tx.Dependencies()
	if err != nil {
		return false, err
	}
	for _, dep := range deps {
		if dep.Status() != choices.Accepted {
			return true, nil
		}
	}
	return false, nil
}

<<<<<<< HEAD
func (t *txJob) Execute() error {
	hasMissingDeps, err := t.HasMissingDependencies()
=======
func (t *txJob) Execute(ctx context.Context) error {
	hasMissingDeps, err := t.HasMissingDependencies(ctx)
>>>>>>> 53a8245a8 (Update consensus)
	if err != nil {
		return err
	}
	if hasMissingDeps {
		t.numDropped.Inc()
		return errMissingTxDependenciesOnAccept
	}

	status := t.tx.Status()
	switch status {
	case choices.Unknown, choices.Rejected:
		t.numDropped.Inc()
		return fmt.Errorf("attempting to execute transaction with status %s", status)
	case choices.Processing:
		txID := t.tx.ID()
<<<<<<< HEAD
		if err := t.tx.Verify(); err != nil {
=======
		if err := t.tx.Verify(ctx); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
			t.log.Error("transaction failed verification during bootstrapping",
				zap.Stringer("txID", txID),
				zap.Error(err),
			)
			return fmt.Errorf("failed to verify transaction in bootstrapping: %w", err)
		}

		t.numAccepted.Inc()
		t.log.Trace("accepting transaction in bootstrapping",
			zap.Stringer("txID", txID),
		)
<<<<<<< HEAD
		if err := t.tx.Accept(); err != nil {
=======
		if err := t.tx.Accept(ctx); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
			t.log.Error("transaction failed to accept during bootstrapping",
				zap.Stringer("txID", txID),
				zap.Error(err),
			)
			return fmt.Errorf("failed to accept transaction in bootstrapping: %w", err)
		}
	}
	return nil
}
<<<<<<< HEAD
func (t *txJob) Bytes() []byte { return t.tx.Bytes() }
=======

func (t *txJob) Bytes() []byte {
	return t.tx.Bytes()
}
>>>>>>> 53a8245a8 (Update consensus)
