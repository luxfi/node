// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/luxfi/node/database"
	"github.com/luxfi/node/utils/units"

	rpcdbpb "github.com/luxfi/node/proto/pb/rpcdb"
)

const iterationBatchSize = 128 * units.KiB

var errUnknownIterator = errors.New("unknown iterator")

// DatabaseServer is a database that is managed over RPC.
type DatabaseServer struct {
	rpcdbpb.UnsafeDatabaseServer

	db database.Database

	// iteratorLock protects [nextIteratorID] and [iterators] from concurrent
	// modifications. Similarly to [batchLock], [iteratorLock] does not protect
	// the actual Iterator. Iterators are documented as not being safe for
	// concurrent use. Therefore, it is up to the client to respect this
	// invariant.
	iteratorLock   sync.RWMutex
	nextIteratorID uint64
	iterators      map[uint64]database.Iterator
}

// NewServer returns a database instance that is managed remotely
func NewServer(db database.Database) *DatabaseServer {
	return &DatabaseServer{
		db:        db,
		iterators: make(map[uint64]database.Iterator),
	}
}

// Has delegates the Has call to the managed database and returns the result
func (db *DatabaseServer) Has(_ context.Context, req *rpcdbpb.HasRequest) (*rpcdbpb.HasResponse, error) {
	has, err := db.db.Has(req.Key)
	return &rpcdbpb.HasResponse{
		Has: has,
		Err: errorToErrEnum[err],
	}, errorToRPCError(err)
}

// Get delegates the Get call to the managed database and returns the result
func (db *DatabaseServer) Get(_ context.Context, req *rpcdbpb.GetRequest) (*rpcdbpb.GetResponse, error) {
	value, err := db.db.Get(req.Key)
	return &rpcdbpb.GetResponse{
		Value: value,
		Err:   errorToErrEnum[err],
	}, errorToRPCError(err)
}

// Put delegates the Put call to the managed database and returns the result
func (db *DatabaseServer) Put(_ context.Context, req *rpcdbpb.PutRequest) (*rpcdbpb.PutResponse, error) {
	err := db.db.Put(req.Key, req.Value)
	return &rpcdbpb.PutResponse{Err: errorToErrEnum[err]}, errorToRPCError(err)
}

// Delete delegates the Delete call to the managed database and returns the
// result
func (db *DatabaseServer) Delete(_ context.Context, req *rpcdbpb.DeleteRequest) (*rpcdbpb.DeleteResponse, error) {
	err := db.db.Delete(req.Key)
	return &rpcdbpb.DeleteResponse{Err: errorToErrEnum[err]}, errorToRPCError(err)
}

// Compact delegates the Compact call to the managed database and returns the
// result
func (db *DatabaseServer) Compact(_ context.Context, req *rpcdbpb.CompactRequest) (*rpcdbpb.CompactResponse, error) {
	err := db.db.Compact(req.Start, req.Limit)
	return &rpcdbpb.CompactResponse{Err: errorToErrEnum[err]}, errorToRPCError(err)
}

// Close delegates the Close call to the managed database and returns the result
func (db *DatabaseServer) Close(context.Context, *rpcdbpb.CloseRequest) (*rpcdbpb.CloseResponse, error) {
	err := db.db.Close()
	return &rpcdbpb.CloseResponse{Err: errorToErrEnum[err]}, errorToRPCError(err)
}

// HealthCheck performs a heath check against the underlying database.
func (db *DatabaseServer) HealthCheck(ctx context.Context, _ *emptypb.Empty) (*rpcdbpb.HealthCheckResponse, error) {
	health, err := db.db.HealthCheck(ctx)
	if err != nil {
		return &rpcdbpb.HealthCheckResponse{}, err
	}

	details, err := json.Marshal(health)
	return &rpcdbpb.HealthCheckResponse{
		Details: details,
	}, err
}

// WriteBatch takes in a set of key-value pairs and atomically writes them to
// the internal database
func (db *DatabaseServer) WriteBatch(_ context.Context, req *rpcdbpb.WriteBatchRequest) (*rpcdbpb.WriteBatchResponse, error) {
	batch := db.db.NewBatch()
	for _, put := range req.Puts {
		if err := batch.Put(put.Key, put.Value); err != nil {
			return &rpcdbpb.WriteBatchResponse{
				Err: errorToErrEnum[err],
			}, errorToRPCError(err)
		}
	}
	for _, del := range req.Deletes {
		if err := batch.Delete(del.Key); err != nil {
			return &rpcdbpb.WriteBatchResponse{
				Err: errorToErrEnum[err],
			}, errorToRPCError(err)
		}
	}

	err := batch.Write()
	return &rpcdbpb.WriteBatchResponse{
		Err: errorToErrEnum[err],
	}, errorToRPCError(err)
}

// NewIteratorWithStartAndPrefix allocates an iterator and returns the iterator
// ID
func (db *DatabaseServer) NewIteratorWithStartAndPrefix(_ context.Context, req *rpcdbpb.NewIteratorWithStartAndPrefixRequest) (*rpcdbpb.NewIteratorWithStartAndPrefixResponse, error) {
	it := db.db.NewIteratorWithStartAndPrefix(req.Start, req.Prefix)

	db.iteratorLock.Lock()
	defer db.iteratorLock.Unlock()

	id := db.nextIteratorID
	db.iterators[id] = it
	db.nextIteratorID++
	return &rpcdbpb.NewIteratorWithStartAndPrefixResponse{Id: id}, nil
}

// IteratorNext attempts to call next on the requested iterator
func (db *DatabaseServer) IteratorNext(_ context.Context, req *rpcdbpb.IteratorNextRequest) (*rpcdbpb.IteratorNextResponse, error) {
	db.iteratorLock.RLock()
	it, exists := db.iterators[req.Id]
	db.iteratorLock.RUnlock()
	if !exists {
		return nil, errUnknownIterator
	}

	var (
		size int
		data []*rpcdbpb.PutRequest
	)
	for size < iterationBatchSize && it.Next() {
		key := it.Key()
		value := it.Value()
		size += len(key) + len(value)

		data = append(data, &rpcdbpb.PutRequest{
			Key:   key,
			Value: value,
		})
	}

	return &rpcdbpb.IteratorNextResponse{Data: data}, nil
}

// IteratorError attempts to report any errors that occurred during iteration
func (db *DatabaseServer) IteratorError(_ context.Context, req *rpcdbpb.IteratorErrorRequest) (*rpcdbpb.IteratorErrorResponse, error) {
	db.iteratorLock.RLock()
	it, exists := db.iterators[req.Id]
	db.iteratorLock.RUnlock()
	if !exists {
		return nil, errUnknownIterator
	}
	err := it.Error()
	return &rpcdbpb.IteratorErrorResponse{Err: errorToErrEnum[err]}, errorToRPCError(err)
}

// IteratorRelease attempts to release the resources allocated to an iterator
func (db *DatabaseServer) IteratorRelease(_ context.Context, req *rpcdbpb.IteratorReleaseRequest) (*rpcdbpb.IteratorReleaseResponse, error) {
	db.iteratorLock.Lock()
	it, exists := db.iterators[req.Id]
	if !exists {
		db.iteratorLock.Unlock()
		return &rpcdbpb.IteratorReleaseResponse{}, nil
	}
	delete(db.iterators, req.Id)
	db.iteratorLock.Unlock()

	err := it.Error()
	it.Release()
	return &rpcdbpb.IteratorReleaseResponse{Err: errorToErrEnum[err]}, errorToRPCError(err)
}
