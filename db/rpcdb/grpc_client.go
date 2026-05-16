//go:build grpc

// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/luxfi/database"
	"github.com/luxfi/math/set"
	"github.com/luxfi/utils"

	rpcdbpb "github.com/luxfi/node/proto/pb/rpcdb"
)

var (
	_ database.Database = (*GRPCClient)(nil)
	_ database.Batch    = (*grpcBatch)(nil)
	_ database.Iterator = (*grpcIterator)(nil)
)

// GRPCClient is a database.Database that talks to a GRPCServer over
// gRPC. Symmetric with GRPCServer — both are pure transport adapters
// against the Layer-B wire types in github.com/luxfi/protocol/rpcdb.
type GRPCClient struct {
	client rpcdbpb.DatabaseClient

	closed utils.Atomic[bool]
}

// NewGRPCClient wraps a gRPC client connection to a remote rpcdb
// service.
func NewGRPCClient(client rpcdbpb.DatabaseClient) *GRPCClient {
	return &GRPCClient{client: client}
}

// Has attempts to return if the database has a key with the provided value.
func (c *GRPCClient) Has(key []byte) (bool, error) {
	resp, err := c.client.Has(context.Background(), &rpcdbpb.HasRequest{Key: key})
	if err != nil {
		return false, err
	}
	return resp.Has, codeToErr(resp.Err)
}

// Get attempts to return the value mapped to the key.
func (c *GRPCClient) Get(key []byte) ([]byte, error) {
	resp, err := c.client.Get(context.Background(), &rpcdbpb.GetRequest{Key: key})
	if err != nil {
		return nil, err
	}
	return resp.Value, codeToErr(resp.Err)
}

// Put attempts to set the value this key maps to.
func (c *GRPCClient) Put(key, value []byte) error {
	resp, err := c.client.Put(context.Background(), &rpcdbpb.PutRequest{
		Key:   key,
		Value: value,
	})
	if err != nil {
		return err
	}
	return codeToErr(resp.Err)
}

// Delete attempts to remove any mapping from the key.
func (c *GRPCClient) Delete(key []byte) error {
	resp, err := c.client.Delete(context.Background(), &rpcdbpb.DeleteRequest{Key: key})
	if err != nil {
		return err
	}
	return codeToErr(resp.Err)
}

// NewBatch returns a new batch.
func (c *GRPCClient) NewBatch() database.Batch {
	return &grpcBatch{db: c}
}

func (c *GRPCClient) NewIterator() database.Iterator {
	return c.NewIteratorWithStartAndPrefix(nil, nil)
}

func (c *GRPCClient) NewIteratorWithStart(start []byte) database.Iterator {
	return c.NewIteratorWithStartAndPrefix(start, nil)
}

func (c *GRPCClient) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return c.NewIteratorWithStartAndPrefix(nil, prefix)
}

// NewIteratorWithStartAndPrefix returns a new iterator.
func (c *GRPCClient) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	resp, err := c.client.NewIteratorWithStartAndPrefix(
		context.Background(),
		&rpcdbpb.NewIteratorWithStartAndPrefixRequest{Start: start, Prefix: prefix},
	)
	if err != nil {
		return &database.IteratorError{Err: err}
	}
	return newGRPCIterator(c, resp.Id)
}

// Compact attempts to optimize the space utilization in the provided range.
func (c *GRPCClient) Compact(start, limit []byte) error {
	resp, err := c.client.Compact(context.Background(), &rpcdbpb.CompactRequest{
		Start: start,
		Limit: limit,
	})
	if err != nil {
		return err
	}
	return codeToErr(resp.Err)
}

// Close attempts to close the database.
func (c *GRPCClient) Close() error {
	c.closed.Set(true)
	resp, err := c.client.Close(context.Background(), &rpcdbpb.CloseRequest{})
	if err != nil {
		return err
	}
	return codeToErr(resp.Err)
}

// Sync — rpc database delegates sync to the underlying database on the
// server side. Operations are already synchronous over RPC; no-op here
// matches the legacy internal/database/rpcdb behavior.
func (c *GRPCClient) Sync() error {
	return nil
}

func (c *GRPCClient) HealthCheck(ctx context.Context) (interface{}, error) {
	health, err := c.client.HealthCheck(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(health.Details), nil
}

// Backup is not supported over the gRPC database client.
func (c *GRPCClient) Backup(_ io.Writer, _ uint64) (uint64, error) {
	return 0, errors.New("rpcdb: backup not supported")
}

// Load is not supported over the gRPC database client.
func (c *GRPCClient) Load(_ io.Reader) error {
	return errors.New("rpcdb: load not supported")
}

// codeToErr maps a generated-pb Error enum back to a database sentinel.
// Lives next to the gRPC adapter so the adapter knows the inverse of
// toPbErr. Mirrors rpcdb.CodeToErr in service.go but for the
// protobuf-typed enum.
func codeToErr(code rpcdbpb.Error) error {
	switch code {
	case rpcdbpb.Error_ERROR_CLOSED:
		return database.ErrClosed
	case rpcdbpb.Error_ERROR_NOT_FOUND:
		return database.ErrNotFound
	default:
		return nil
	}
}

type grpcBatch struct {
	database.BatchOps

	db *GRPCClient
}

func (b *grpcBatch) Write() error {
	request := &rpcdbpb.WriteBatchRequest{}
	keySet := set.NewSet[string](len(b.Ops))
	for i := len(b.Ops) - 1; i >= 0; i-- {
		op := b.Ops[i]
		key := string(op.Key)
		if keySet.Contains(key) {
			continue
		}
		keySet.Add(key)

		if op.Delete {
			request.Deletes = append(request.Deletes, &rpcdbpb.DeleteRequest{Key: op.Key})
		} else {
			request.Puts = append(request.Puts, &rpcdbpb.PutRequest{
				Key:   op.Key,
				Value: op.Value,
			})
		}
	}

	resp, err := b.db.client.WriteBatch(context.Background(), request)
	if err != nil {
		return err
	}
	return codeToErr(resp.Err)
}

func (b *grpcBatch) Inner() database.Batch {
	return b
}

type grpcIterator struct {
	db *GRPCClient
	id uint64

	data        []*rpcdbpb.PutRequest
	fetchedData chan []*rpcdbpb.PutRequest

	errLock sync.RWMutex
	err     error

	reqUpdateError chan chan struct{}

	once     sync.Once
	onClose  chan struct{}
	onClosed chan struct{}
}

func newGRPCIterator(db *GRPCClient, id uint64) *grpcIterator {
	it := &grpcIterator{
		db:             db,
		id:             id,
		fetchedData:    make(chan []*rpcdbpb.PutRequest),
		reqUpdateError: make(chan chan struct{}),
		onClose:        make(chan struct{}),
		onClosed:       make(chan struct{}),
	}
	go it.fetch()
	return it
}

// Invariant: fetch is the only thread with access to send requests to
// the server's iterator. This is needed because iterators are not
// thread safe and the server expects the client (us) to only ever
// issue one request at a time for a given iterator id.
func (it *grpcIterator) fetch() {
	defer func() {
		resp, err := it.db.client.IteratorRelease(context.Background(), &rpcdbpb.IteratorReleaseRequest{Id: it.id})
		if err != nil {
			it.setError(err)
		} else {
			it.setError(codeToErr(resp.Err))
		}

		close(it.fetchedData)
		close(it.onClosed)
	}()

	for {
		resp, err := it.db.client.IteratorNext(context.Background(), &rpcdbpb.IteratorNextRequest{Id: it.id})
		if err != nil {
			it.setError(err)
			return
		}

		if len(resp.Data) == 0 {
			return
		}

		for {
			select {
			case it.fetchedData <- resp.Data:
			case onUpdated := <-it.reqUpdateError:
				it.updateError()
				close(onUpdated)
				continue
			case <-it.onClose:
				return
			}
			break
		}
	}
}

// Next attempts to move the iterator to the next element.
func (it *grpcIterator) Next() bool {
	if it.db.closed.Get() {
		it.data = nil
		it.setError(database.ErrClosed)
		return false
	}
	if len(it.data) > 1 {
		it.data[0] = nil
		it.data = it.data[1:]
		return true
	}

	it.data = <-it.fetchedData
	return len(it.data) > 0
}

// Error returns any error that occurred while iterating.
func (it *grpcIterator) Error() error {
	if err := it.getError(); err != nil {
		return err
	}

	onUpdated := make(chan struct{})
	select {
	case it.reqUpdateError <- onUpdated:
		<-onUpdated
	case <-it.onClosed:
	}

	return it.getError()
}

// Key returns the key of the current element.
func (it *grpcIterator) Key() []byte {
	if len(it.data) == 0 {
		return nil
	}
	return it.data[0].Key
}

// Value returns the value of the current element.
func (it *grpcIterator) Value() []byte {
	if len(it.data) == 0 {
		return nil
	}
	return it.data[0].Value
}

// Release frees any resources held by the iterator.
func (it *grpcIterator) Release() {
	it.once.Do(func() {
		close(it.onClose)
		<-it.onClosed
	})
}

func (it *grpcIterator) updateError() {
	resp, err := it.db.client.IteratorError(context.Background(), &rpcdbpb.IteratorErrorRequest{Id: it.id})
	if err != nil {
		it.setError(err)
	} else {
		it.setError(codeToErr(resp.Err))
	}
}

func (it *grpcIterator) setError(err error) {
	if err == nil {
		return
	}

	it.errLock.Lock()
	defer it.errLock.Unlock()

	if it.err == nil {
		it.err = err
	}
}

func (it *grpcIterator) getError() error {
	it.errLock.RLock()
	defer it.errLock.RUnlock()

	return it.err
}
