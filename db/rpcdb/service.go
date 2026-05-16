// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rpcdb is luxd's canonical Layer C home for the rpcdb service:
// one Service implementation backed by a database.Database, with a
// transport adapter per supported wire protocol.
//
// Layered topology:
//
//	Layer A — wire framing                   (github.com/luxfi/api/zap)
//	Layer B — service spec (data carriers)   (github.com/luxfi/protocol/rpcdb)
//	Layer C — service impl + transports      (this package)
//
// One Service. Many transport adapters. Adding a new transport is a
// new file that wraps `*Service`; the storage logic stays here and
// stays orthogonal to framing concerns.
//
// Files:
//   - service.go      (this) — transport-neutral Service struct + methods
//   - grpc_server.go  (build tag `grpc`) — gRPC adapter
//   - zap_server.go   (default)         — ZAP adapter
package rpcdb

import (
	"context"
	"errors"
	"sync"

	"github.com/luxfi/database"
	rpcdb "github.com/luxfi/protocol/rpcdb"
)

// errUnknownIterator is the sentinel returned by IteratorNext / Error
// / Release when the caller hands in an iterator id that was never
// allocated (or has already been released). Transport adapters should
// surface this however their wire spec demands.
var errUnknownIterator = errors.New("rpcdb: unknown iterator")

// iterationBatchBytes is the upper bound on bytes batched into a
// single IteratorNext response. Picked to match the cevm-side
// expectation (RemoteZapDB::iterator_next reads up to ~128 KiB per
// roundtrip).
const iterationBatchBytes = 128 * 1024

// Service is the rpcdb service implementation. It holds one
// `database.Database` and a server-managed iterator pool, and exposes
// every rpcdb operation as a (request → response) method on Go data
// carriers from the proto/rpcdb wire-types package.
//
// Service is transport-agnostic: it knows nothing about gRPC, ZAP, or
// any other framing. Each transport adapter wraps `*Service` and
// translates its own wire format into these method calls.
type Service struct {
	db database.Database

	// iteratorMu serializes mutations of nextIteratorID and iterators.
	// It does NOT serialize calls to a particular iterator — the
	// underlying database.Iterator is documented as not safe for
	// concurrent use, and the caller is expected to respect that.
	iteratorMu     sync.RWMutex
	nextIteratorID uint64
	iterators      map[uint64]database.Iterator
}

// NewService wraps `db` so it can be served over any transport.
func NewService(db database.Database) *Service {
	return &Service{
		db:        db,
		iterators: make(map[uint64]database.Iterator),
	}
}

// DB returns the underlying database. Adapters that need to surface
// transport-specific behavior (eg. health-check serialization) can
// reach in for it; everyone else uses the methods.
func (s *Service) DB() database.Database {
	return s.db
}

// CloseIterators releases every server-side iterator. Adapters call
// this on shutdown so iterators don't leak past the listener.
func (s *Service) CloseIterators() {
	s.iteratorMu.Lock()
	defer s.iteratorMu.Unlock()
	for id, it := range s.iterators {
		it.Release()
		delete(s.iterators, id)
	}
}

// errToCode maps a database error to the wire enum.
func errToCode(err error) rpcdb.Error {
	switch {
	case err == nil:
		return rpcdb.Error_ERROR_UNSPECIFIED
	case errors.Is(err, database.ErrNotFound):
		return rpcdb.Error_ERROR_NOT_FOUND
	case errors.Is(err, database.ErrClosed):
		return rpcdb.Error_ERROR_CLOSED
	default:
		// Any other error is reported as CLOSED on the wire — the
		// proto enum doesn't have a richer error space. Adapters that
		// can carry a transport-level error message should also pass
		// the original err out-of-band.
		return rpcdb.Error_ERROR_CLOSED
	}
}

// CodeToErr maps a wire-typed Error back to a database sentinel. Used
// by remote clients on the receive side; lives here so server and
// client agree on the inverse of errToCode.
func CodeToErr(code rpcdb.Error) error {
	switch code {
	case rpcdb.Error_ERROR_UNSPECIFIED:
		return nil
	case rpcdb.Error_ERROR_NOT_FOUND:
		return database.ErrNotFound
	case rpcdb.Error_ERROR_CLOSED:
		return database.ErrClosed
	default:
		return database.ErrClosed
	}
}

// Has services rpcdb.Has.
func (s *Service) Has(_ context.Context, req *rpcdb.HasRequest) (*rpcdb.HasResponse, error) {
	has, err := s.db.Has(req.Key)
	if err != nil {
		return &rpcdb.HasResponse{Has: false, Err: errToCode(err)}, nil
	}
	return &rpcdb.HasResponse{Has: has, Err: rpcdb.Error_ERROR_UNSPECIFIED}, nil
}

// Get services rpcdb.Get.
func (s *Service) Get(_ context.Context, req *rpcdb.GetRequest) (*rpcdb.GetResponse, error) {
	value, err := s.db.Get(req.Key)
	if err != nil {
		return &rpcdb.GetResponse{Value: nil, Err: errToCode(err)}, nil
	}
	return &rpcdb.GetResponse{Value: value, Err: rpcdb.Error_ERROR_UNSPECIFIED}, nil
}

// Put services rpcdb.Put.
func (s *Service) Put(_ context.Context, req *rpcdb.PutRequest) (*rpcdb.PutResponse, error) {
	return &rpcdb.PutResponse{Err: errToCode(s.db.Put(req.Key, req.Value))}, nil
}

// Delete services rpcdb.Delete.
func (s *Service) Delete(_ context.Context, req *rpcdb.DeleteRequest) (*rpcdb.DeleteResponse, error) {
	return &rpcdb.DeleteResponse{Err: errToCode(s.db.Delete(req.Key))}, nil
}

// WriteBatch services rpcdb.WriteBatch.
func (s *Service) WriteBatch(_ context.Context, req *rpcdb.WriteBatchRequest) (*rpcdb.WriteBatchResponse, error) {
	batch := s.db.NewBatch()
	for _, p := range req.Puts {
		if err := batch.Put(p.Key, p.Value); err != nil {
			return &rpcdb.WriteBatchResponse{Err: errToCode(err)}, nil
		}
	}
	for _, d := range req.Deletes {
		if err := batch.Delete(d.Key); err != nil {
			return &rpcdb.WriteBatchResponse{Err: errToCode(err)}, nil
		}
	}
	return &rpcdb.WriteBatchResponse{Err: errToCode(batch.Write())}, nil
}

// Compact services rpcdb.Compact.
func (s *Service) Compact(_ context.Context, req *rpcdb.CompactRequest) (*rpcdb.CompactResponse, error) {
	return &rpcdb.CompactResponse{Err: errToCode(s.db.Compact(req.Start, req.Limit))}, nil
}

// Close services rpcdb.Close.
func (s *Service) Close(_ context.Context, _ *rpcdb.CloseRequest) (*rpcdb.CloseResponse, error) {
	return &rpcdb.CloseResponse{Err: errToCode(s.db.Close())}, nil
}

// HealthCheck services rpcdb.HealthCheck.
func (s *Service) HealthCheck(ctx context.Context) (*rpcdb.HealthCheckResponse, error) {
	health, err := s.db.HealthCheck(ctx)
	if err != nil {
		return &rpcdb.HealthCheckResponse{Details: []byte(err.Error())}, nil
	}
	details := []byte("healthy")
	if health != nil {
		if str, ok := health.(string); ok {
			details = []byte(str)
		}
	}
	return &rpcdb.HealthCheckResponse{Details: details}, nil
}

// NewIteratorWithStartAndPrefix services rpcdb.NewIteratorWithStartAndPrefix.
func (s *Service) NewIteratorWithStartAndPrefix(_ context.Context, req *rpcdb.NewIteratorWithStartAndPrefixRequest) (*rpcdb.NewIteratorWithStartAndPrefixResponse, error) {
	it := s.db.NewIteratorWithStartAndPrefix(req.Start, req.Prefix)
	s.iteratorMu.Lock()
	id := s.nextIteratorID
	s.nextIteratorID++
	s.iterators[id] = it
	s.iteratorMu.Unlock()
	return &rpcdb.NewIteratorWithStartAndPrefixResponse{Id: id}, nil
}

// IteratorNext services rpcdb.IteratorNext, returning up to
// iterationBatchBytes worth of (key, value) pairs per call. An empty
// Data slice signals exhaustion.
//
// Returned bytes are deep-copied so the caller can hold them past the
// next iterator advance; the underlying database.Iterator is allowed
// to recycle its internal buffers.
func (s *Service) IteratorNext(_ context.Context, req *rpcdb.IteratorNextRequest) (*rpcdb.IteratorNextResponse, error) {
	s.iteratorMu.RLock()
	it, ok := s.iterators[req.Id]
	s.iteratorMu.RUnlock()
	if !ok {
		return nil, errUnknownIterator
	}

	var (
		size int
		data []*rpcdb.PutRequest
	)
	for size < iterationBatchBytes && it.Next() {
		k := it.Key()
		v := it.Value()
		size += len(k) + len(v)

		kc := make([]byte, len(k))
		copy(kc, k)
		vc := make([]byte, len(v))
		copy(vc, v)
		data = append(data, &rpcdb.PutRequest{Key: kc, Value: vc})
	}
	return &rpcdb.IteratorNextResponse{Data: data}, nil
}

// IteratorError services rpcdb.IteratorError.
func (s *Service) IteratorError(_ context.Context, req *rpcdb.IteratorErrorRequest) (*rpcdb.IteratorErrorResponse, error) {
	s.iteratorMu.RLock()
	it, ok := s.iterators[req.Id]
	s.iteratorMu.RUnlock()
	if !ok {
		return &rpcdb.IteratorErrorResponse{Err: rpcdb.Error_ERROR_NOT_FOUND}, nil
	}
	return &rpcdb.IteratorErrorResponse{Err: errToCode(it.Error())}, nil
}

// IteratorRelease services rpcdb.IteratorRelease. Idempotent: calling
// it twice for the same id returns UNSPECIFIED on the second call.
func (s *Service) IteratorRelease(_ context.Context, req *rpcdb.IteratorReleaseRequest) (*rpcdb.IteratorReleaseResponse, error) {
	s.iteratorMu.Lock()
	it, ok := s.iterators[req.Id]
	if !ok {
		s.iteratorMu.Unlock()
		return &rpcdb.IteratorReleaseResponse{Err: rpcdb.Error_ERROR_UNSPECIFIED}, nil
	}
	delete(s.iterators, req.Id)
	s.iteratorMu.Unlock()

	dbErr := it.Error()
	it.Release()
	return &rpcdb.IteratorReleaseResponse{Err: errToCode(dbErr)}, nil
}
