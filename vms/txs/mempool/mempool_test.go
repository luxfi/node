// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	vmcore "github.com/luxfi/vm"
)

var _ Tx = (*dummyTx)(nil)

type dummyTx struct {
	size     int
	id       ids.ID
	inputIDs []ids.ID
}

func (tx *dummyTx) Size() int {
	return tx.size
}

func (tx *dummyTx) ID() ids.ID {
	return tx.id
}

func (tx *dummyTx) InputIDs() set.Set[ids.ID] {
	return set.Of(tx.inputIDs...)
}

type noMetrics struct{}

func (*noMetrics) Update(int, int) {}

func newMempool() *mempool[*dummyTx] {
	return New[*dummyTx](&noMetrics{})
}

func TestAdd(t *testing.T) {
	tx0 := newTx(0, 32)

	tests := []struct {
		name       string
		initialTxs []*dummyTx
		tx         *dummyTx
		err        error
		dropReason error
	}{
		{
			name:       "successfully add tx",
			initialTxs: nil,
			tx:         tx0,
			err:        nil,
			dropReason: nil,
		},
		{
			name:       "attempt adding duplicate tx",
			initialTxs: []*dummyTx{tx0},
			tx:         tx0,
			err:        ErrDuplicateTx,
			dropReason: nil,
		},
		{
			name:       "attempt adding too large tx",
			initialTxs: nil,
			tx:         newTx(0, MaxTxSize+1),
			err:        ErrTxTooLarge,
			dropReason: ErrTxTooLarge,
		},
		{
			name:       "attempt adding tx when full",
			initialTxs: newTxs(maxMempoolSize/MaxTxSize, MaxTxSize),
			tx:         newTx(maxMempoolSize/MaxTxSize, MaxTxSize),
			err:        ErrMempoolFull,
			dropReason: nil,
		},
		{
			name:       "attempt adding conflicting tx",
			initialTxs: []*dummyTx{tx0},
			tx:         newTx(0, 32),
			err:        ErrConflictsWithOtherTx,
			dropReason: ErrConflictsWithOtherTx,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			mempool := newMempool()

			for _, tx := range test.initialTxs {
				require.NoError(mempool.Add(tx))
			}

			err := mempool.Add(test.tx)
			require.ErrorIs(err, test.err)

			txID := test.tx.ID()

			if err != nil {
				mempool.MarkDropped(txID, err)
			}

			err = mempool.GetDropReason(txID)
			require.ErrorIs(err, test.dropReason)
		})
	}
}

func TestGet(t *testing.T) {
	require := require.New(t)

	mempool := newMempool()

	tx := newTx(0, 32)
	txID := tx.ID()

	_, exists := mempool.Get(txID)
	require.False(exists)

	require.NoError(mempool.Add(tx))

	returned, exists := mempool.Get(txID)
	require.True(exists)
	require.Equal(tx, returned)

	mempool.Remove(tx)

	_, exists = mempool.Get(txID)
	require.False(exists)
}

func TestPeek(t *testing.T) {
	require := require.New(t)

	mempool := newMempool()

	_, exists := mempool.Peek()
	require.False(exists)

	tx0 := newTx(0, 32)
	tx1 := newTx(1, 32)

	require.NoError(mempool.Add(tx0))
	require.NoError(mempool.Add(tx1))

	tx, exists := mempool.Peek()
	require.True(exists)
	require.Equal(tx, tx0)

	mempool.Remove(tx0)

	tx, exists = mempool.Peek()
	require.True(exists)
	require.Equal(tx, tx1)

	mempool.Remove(tx0)

	tx, exists = mempool.Peek()
	require.True(exists)
	require.Equal(tx, tx1)

	mempool.Remove(tx1)

	_, exists = mempool.Peek()
	require.False(exists)
}

func TestRemoveConflict(t *testing.T) {
	require := require.New(t)

	mempool := newMempool()

	tx := newTx(0, 32)
	txConflict := newTx(0, 32)

	require.NoError(mempool.Add(tx))

	returnedTx, exists := mempool.Peek()
	require.True(exists)
	require.Equal(returnedTx, tx)

	mempool.Remove(txConflict)

	_, exists = mempool.Peek()
	require.False(exists)
}

func TestIterate(t *testing.T) {
	require := require.New(t)

	mempool := newMempool()

	var (
		iteratedTxs []*dummyTx
		maxLen      = 2
	)
	addTxs := func(tx *dummyTx) bool {
		iteratedTxs = append(iteratedTxs, tx)
		return len(iteratedTxs) < maxLen
	}
	mempool.Iterate(addTxs)
	require.Empty(iteratedTxs)

	tx0 := newTx(0, 32)
	require.NoError(mempool.Add(tx0))

	mempool.Iterate(addTxs)
	require.Equal([]*dummyTx{tx0}, iteratedTxs)

	tx1 := newTx(1, 32)
	require.NoError(mempool.Add(tx1))

	iteratedTxs = nil
	mempool.Iterate(addTxs)
	require.Equal([]*dummyTx{tx0, tx1}, iteratedTxs)

	tx2 := newTx(2, 32)
	require.NoError(mempool.Add(tx2))

	iteratedTxs = nil
	mempool.Iterate(addTxs)
	require.Equal([]*dummyTx{tx0, tx1}, iteratedTxs)

	mempool.Remove(tx0, tx2)

	iteratedTxs = nil
	mempool.Iterate(addTxs)
	require.Equal([]*dummyTx{tx1}, iteratedTxs)
}

func TestDropped(t *testing.T) {
	require := require.New(t)

	mempool := newMempool()

	tx := newTx(0, 32)
	txID := tx.ID()
	testErr := errors.New("test")

	mempool.MarkDropped(txID, testErr)

	err := mempool.GetDropReason(txID)
	require.ErrorIs(err, testErr)

	require.NoError(mempool.Add(tx))
	require.NoError(mempool.GetDropReason(txID))

	mempool.MarkDropped(txID, testErr)
	require.NoError(mempool.GetDropReason(txID))
}

func newTxs(num int, size int) []*dummyTx {
	txs := make([]*dummyTx, num)
	for i := range txs {
		txs[i] = newTx(uint64(i), size)
	}
	return txs
}

func newTx(index uint64, size int) *dummyTx {
	return &dummyTx{
		size:     size,
		id:       ids.GenerateTestID(),
		inputIDs: []ids.ID{ids.Empty.Prefix(index)},
	}
}

// shows that valid tx is not added to mempool if this would exceed its maximum
// size
func TestBlockBuilderMaxMempoolSizeHandling(t *testing.T) {
	require := require.New(t)

	mpool := newMempool()

	tx := newTx(0, 32)

	// shortcut to simulated almost filled mempool
	mpool.bytesAvailable = tx.Size() - 1

	err := mpool.Add(tx)
	require.ErrorIs(err, ErrMempoolFull)

	// tx should not be marked as dropped if the mempool is full
	txID := tx.ID()
	mpool.MarkDropped(txID, err)
	require.NoError(mpool.GetDropReason(txID))

	// shortcut to simulated almost filled mempool
	mpool.bytesAvailable = tx.Size()

	err = mpool.Add(tx)
	require.NoError(err, "should have added tx to mempool")
}

func TestWaitForEventCancelled(t *testing.T) {
	m := newMempool()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.WaitForEvent(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWaitForEventWithTx(t *testing.T) {
	require := require.New(t)

	m := newMempool()
	errs := make(chan error)
	go func() {
		tx := newTx(0, 32)
		errs <- m.Add(tx)
	}()

	msg, err := m.WaitForEvent(context.Background())
	require.NoError(err)
	require.Equal(vmcore.PendingTxs, msg.Type)
	require.NoError(<-errs)
}

// -----------------------------------------------------------------------------
// AdmissionVerifier tests (luxfi/node#115)
// -----------------------------------------------------------------------------

// fakeVerifier implements AdmissionVerifier[*dummyTx] for the tests below.
// It tracks every call so tests can assert call counts and inspect the txs
// that the gate observed.
type fakeVerifier struct {
	err  error
	seen []ids.ID
}

func (v *fakeVerifier) VerifyAdmit(tx *dummyTx) error {
	v.seen = append(v.seen, tx.ID())
	return v.err
}

// Nil verifier path: NewWithAdmissionVerifier with nil must behave
// byte-identically to New. We assert Add succeeds and Len reflects the tx.
func TestAdmissionVerifier_nilVerifierMatchesNew(t *testing.T) {
	require := require.New(t)

	m := NewWithAdmissionVerifier[*dummyTx](&noMetrics{}, nil)
	tx := newTx(0, 32)
	require.NoError(m.Add(tx))
	require.Equal(1, m.Len())
}

// Verifier returning nil: tx is admitted. The gate must run exactly once
// per Add (not on Get, Peek, Iterate, or Remove).
func TestAdmissionVerifier_acceptsWhenVerifierReturnsNil(t *testing.T) {
	require := require.New(t)

	v := &fakeVerifier{err: nil}
	m := NewWithAdmissionVerifier[*dummyTx](&noMetrics{}, v)
	tx := newTx(0, 32)
	require.NoError(m.Add(tx))
	require.Equal(1, m.Len())
	require.Len(v.seen, 1)
	require.Equal(tx.ID(), v.seen[0])

	// Get / Peek / Iterate do not re-run the verifier.
	_, _ = m.Get(tx.ID())
	_, _ = m.Peek()
	m.Iterate(func(*dummyTx) bool { return true })
	require.Len(v.seen, 1, "verifier must run only on Add")

	// Remove does not re-run the verifier.
	m.Remove(tx)
	require.Len(v.seen, 1, "Remove must not invoke the verifier")
}

// Verifier returning an error: tx is rejected, the returned error wraps
// ErrAdmissionRejected and carries the verifier's reason, and the drop
// reason is recorded.
func TestAdmissionVerifier_rejectsWhenVerifierReturnsError(t *testing.T) {
	require := require.New(t)

	verifyErr := errors.New("nizk proof invalid")
	v := &fakeVerifier{err: verifyErr}
	m := NewWithAdmissionVerifier[*dummyTx](&noMetrics{}, v)
	tx := newTx(0, 32)

	addErr := m.Add(tx)
	require.Error(addErr)
	require.ErrorIs(addErr, ErrAdmissionRejected, "outer error must wrap ErrAdmissionRejected")
	require.ErrorIs(addErr, verifyErr, "outer error must wrap the verifier's reason")

	require.Equal(0, m.Len(), "rejected tx must not be inserted")

	dropReason := m.GetDropReason(tx.ID())
	require.Error(dropReason, "rejected tx must have a recorded drop reason")
	require.ErrorIs(dropReason, ErrAdmissionRejected)
	require.ErrorIs(dropReason, verifyErr)

	require.Len(v.seen, 1, "verifier must be invoked exactly once")
}

// Cheap checks short-circuit before the verifier runs. We exercise the
// three cheap reject paths (duplicate, oversize, conflict) and assert the
// verifier never observed those txs — verification cost should not be paid
// on a tx that would have been dropped anyway.
func TestAdmissionVerifier_cheapChecksShortCircuit(t *testing.T) {
	require := require.New(t)

	v := &fakeVerifier{err: nil}
	m := NewWithAdmissionVerifier[*dummyTx](&noMetrics{}, v)

	// 1. Duplicate: first Add admits and runs the verifier once; second Add
	// must reject with ErrDuplicateTx without re-running it.
	tx := newTx(0, 32)
	require.NoError(m.Add(tx))
	require.Len(v.seen, 1)
	dupErr := m.Add(tx)
	require.ErrorIs(dupErr, ErrDuplicateTx)
	require.Len(v.seen, 1, "verifier must not run on duplicate")

	// 2. Oversize: tx larger than MaxTxSize is rejected before the
	// verifier sees it.
	bigTx := newTx(99, MaxTxSize+1)
	bigErr := m.Add(bigTx)
	require.ErrorIs(bigErr, ErrTxTooLarge)
	require.Len(v.seen, 1, "verifier must not run on oversize tx")

	// 3. Conflict: a second tx consuming the same UTXO as the admitted tx
	// is rejected before the verifier sees it.
	conflictTx := &dummyTx{
		id:       ids.GenerateTestID(),
		size:     32,
		inputIDs: tx.inputIDs, // same inputs as the admitted tx
	}
	conflictErr := m.Add(conflictTx)
	require.ErrorIs(conflictErr, ErrConflictsWithOtherTx)
	require.Len(v.seen, 1, "verifier must not run on conflicting tx")
}
