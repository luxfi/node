//go:build !grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rpcdb provides the default ZAP-backed database RPC.
//
// The host (luxd) holds a `database.Database` (currently zapdb under
// the hood) and exposes it to a VM plugin process over the ZAP
// transport. The plugin sees a `database.Database` interface that
// forwards every call across the wire.
//
// This is the default build — it uses ZAP for transport and pure-Go
// types from proto/zap/rpcdb on the wire (no protobuf). The grpc
// build tag selects rpcdb_grpc.go instead, which uses gRPC + the
// protobuf-generated types under proto/pb/rpcdb. The two are mutually
// exclusive: build with `-tags=grpc` or with no tag, never both.
package rpcdb

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/luxfi/database"
	zaprpcdb "github.com/luxfi/node/proto/zap/rpcdb"
)

// Re-export the wire types so callers that import "proto/rpcdb"
// see a single namespace regardless of which transport is active.
type (
	Error                                 = zaprpcdb.Error
	HasRequest                            = zaprpcdb.HasRequest
	HasResponse                           = zaprpcdb.HasResponse
	GetRequest                            = zaprpcdb.GetRequest
	GetResponse                           = zaprpcdb.GetResponse
	PutRequest                            = zaprpcdb.PutRequest
	PutResponse                           = zaprpcdb.PutResponse
	DeleteRequest                         = zaprpcdb.DeleteRequest
	DeleteResponse                        = zaprpcdb.DeleteResponse
	WriteBatchRequest                     = zaprpcdb.WriteBatchRequest
	WriteBatchResponse                    = zaprpcdb.WriteBatchResponse
	CompactRequest                        = zaprpcdb.CompactRequest
	CompactResponse                       = zaprpcdb.CompactResponse
	CloseRequest                          = zaprpcdb.CloseRequest
	CloseResponse                         = zaprpcdb.CloseResponse
	HealthCheckResponse                   = zaprpcdb.HealthCheckResponse
	NewIteratorWithStartAndPrefixRequest  = zaprpcdb.NewIteratorWithStartAndPrefixRequest
	NewIteratorWithStartAndPrefixResponse = zaprpcdb.NewIteratorWithStartAndPrefixResponse
	IteratorNextRequest                   = zaprpcdb.IteratorNextRequest
	IteratorNextResponse                  = zaprpcdb.IteratorNextResponse
	IteratorErrorRequest                  = zaprpcdb.IteratorErrorRequest
	IteratorErrorResponse                 = zaprpcdb.IteratorErrorResponse
	IteratorReleaseRequest                = zaprpcdb.IteratorReleaseRequest
	IteratorReleaseResponse               = zaprpcdb.IteratorReleaseResponse
)

const (
	Error_ERROR_UNSPECIFIED = zaprpcdb.Error_ERROR_UNSPECIFIED
	Error_ERROR_CLOSED      = zaprpcdb.Error_ERROR_CLOSED
	Error_ERROR_NOT_FOUND   = zaprpcdb.Error_ERROR_NOT_FOUND
)

// Transport is the minimal RPC dispatcher abstraction the rpcdb
// client uses to talk to the server. The default ZAP transport
// implementation in vms/rpcchainvm/zap satisfies this interface; any
// other in-process or socket-based transport that can dispatch
// (method name, request) → response also works.
//
// Keeping this interface in the rpcdb package — rather than importing
// luxfi/api/zap directly — avoids a circular dependency between the
// proto layer and the application layer.
type Transport interface {
	// Call dispatches a request to the named method and writes the
	// response into out. The on-the-wire encoding of req and out is
	// the transport's responsibility; this package only commits to
	// the named-method shape.
	Call(ctx context.Context, method string, req any, out any) error
}

// Method names — kept in one place so the client and server stay in
// sync.
const (
	methodHas                           = "rpcdb.Has"
	methodGet                           = "rpcdb.Get"
	methodPut                           = "rpcdb.Put"
	methodDelete                        = "rpcdb.Delete"
	methodWriteBatch                    = "rpcdb.WriteBatch"
	methodCompact                       = "rpcdb.Compact"
	methodClose                         = "rpcdb.Close"
	methodHealthCheck                   = "rpcdb.HealthCheck"
	methodNewIteratorWithStartAndPrefix = "rpcdb.NewIteratorWithStartAndPrefix"
	methodIteratorNext                  = "rpcdb.IteratorNext"
	methodIteratorError                 = "rpcdb.IteratorError"
	methodIteratorRelease               = "rpcdb.IteratorRelease"
)

// DatabaseServer hosts a `database.Database` over a ZAP transport.
// The server registers its methods with the transport's handler
// surface so a remote DatabaseClient can call them. Every database
// call (Has/Get/Put/…) becomes one round-trip on the underlying
// connection, which today is a Z-Wing-encrypted ZAP channel.
type DatabaseServer struct {
	db database.Database

	iteratorLock   sync.RWMutex
	nextIteratorID uint64
	iterators      map[uint64]database.Iterator
}

// NewServer wraps `db` for service over a ZAP transport.
func NewServer(db database.Database) *DatabaseServer {
	return &DatabaseServer{
		db:        db,
		iterators: make(map[uint64]database.Iterator),
	}
}

// Has services rpcdb.Has.
func (s *DatabaseServer) Has(_ context.Context, req *HasRequest) (*HasResponse, error) {
	has, err := s.db.Has(req.Key)
	if err != nil {
		return &HasResponse{Has: false, Err: errorToRPCError(err)}, nil
	}
	return &HasResponse{Has: has, Err: Error_ERROR_UNSPECIFIED}, nil
}

// Get services rpcdb.Get.
func (s *DatabaseServer) Get(_ context.Context, req *GetRequest) (*GetResponse, error) {
	value, err := s.db.Get(req.Key)
	if err != nil {
		return &GetResponse{Value: nil, Err: errorToRPCError(err)}, nil
	}
	return &GetResponse{Value: value, Err: Error_ERROR_UNSPECIFIED}, nil
}

// Put services rpcdb.Put.
func (s *DatabaseServer) Put(_ context.Context, req *PutRequest) (*PutResponse, error) {
	return &PutResponse{Err: errorToRPCError(s.db.Put(req.Key, req.Value))}, nil
}

// Delete services rpcdb.Delete.
func (s *DatabaseServer) Delete(_ context.Context, req *DeleteRequest) (*DeleteResponse, error) {
	return &DeleteResponse{Err: errorToRPCError(s.db.Delete(req.Key))}, nil
}

// WriteBatch services rpcdb.WriteBatch.
func (s *DatabaseServer) WriteBatch(_ context.Context, req *WriteBatchRequest) (*WriteBatchResponse, error) {
	batch := s.db.NewBatch()
	for _, p := range req.Puts {
		if err := batch.Put(p.Key, p.Value); err != nil {
			return &WriteBatchResponse{Err: errorToRPCError(err)}, nil
		}
	}
	for _, d := range req.Deletes {
		if err := batch.Delete(d.Key); err != nil {
			return &WriteBatchResponse{Err: errorToRPCError(err)}, nil
		}
	}
	return &WriteBatchResponse{Err: errorToRPCError(batch.Write())}, nil
}

// Compact services rpcdb.Compact.
func (s *DatabaseServer) Compact(_ context.Context, req *CompactRequest) (*CompactResponse, error) {
	return &CompactResponse{Err: errorToRPCError(s.db.Compact(req.Start, req.Limit))}, nil
}

// Close services rpcdb.Close.
func (s *DatabaseServer) Close(_ context.Context, _ *CloseRequest) (*CloseResponse, error) {
	return &CloseResponse{Err: errorToRPCError(s.db.Close())}, nil
}

// HealthCheck services rpcdb.HealthCheck.
func (s *DatabaseServer) HealthCheck(ctx context.Context) (*HealthCheckResponse, error) {
	health, err := s.db.HealthCheck(ctx)
	if err != nil {
		return &HealthCheckResponse{Details: []byte(err.Error())}, nil
	}
	details := []byte("healthy")
	if health != nil {
		if str, ok := health.(string); ok {
			details = []byte(str)
		}
	}
	return &HealthCheckResponse{Details: details}, nil
}

// NewIteratorWithStartAndPrefix services rpcdb.NewIteratorWithStartAndPrefix.
func (s *DatabaseServer) NewIteratorWithStartAndPrefix(_ context.Context, req *NewIteratorWithStartAndPrefixRequest) (*NewIteratorWithStartAndPrefixResponse, error) {
	it := s.db.NewIteratorWithStartAndPrefix(req.Start, req.Prefix)
	s.iteratorLock.Lock()
	id := s.nextIteratorID
	s.nextIteratorID++
	s.iterators[id] = it
	s.iteratorLock.Unlock()
	return &NewIteratorWithStartAndPrefixResponse{Id: id}, nil
}

// IteratorNext services rpcdb.IteratorNext.
func (s *DatabaseServer) IteratorNext(_ context.Context, req *IteratorNextRequest) (*IteratorNextResponse, error) {
	s.iteratorLock.RLock()
	it, ok := s.iterators[req.Id]
	s.iteratorLock.RUnlock()
	if !ok {
		return &IteratorNextResponse{Data: nil}, nil
	}
	const maxBatchSize = 100
	var data []*PutRequest
	for i := 0; i < maxBatchSize && it.Next(); i++ {
		data = append(data, &PutRequest{Key: it.Key(), Value: it.Value()})
	}
	return &IteratorNextResponse{Data: data}, nil
}

// IteratorError services rpcdb.IteratorError.
func (s *DatabaseServer) IteratorError(_ context.Context, req *IteratorErrorRequest) (*IteratorErrorResponse, error) {
	s.iteratorLock.RLock()
	it, ok := s.iterators[req.Id]
	s.iteratorLock.RUnlock()
	if !ok {
		return &IteratorErrorResponse{Err: Error_ERROR_NOT_FOUND}, nil
	}
	return &IteratorErrorResponse{Err: errorToRPCError(it.Error())}, nil
}

// IteratorRelease services rpcdb.IteratorRelease.
func (s *DatabaseServer) IteratorRelease(_ context.Context, req *IteratorReleaseRequest) (*IteratorReleaseResponse, error) {
	s.iteratorLock.Lock()
	it, ok := s.iterators[req.Id]
	if ok {
		it.Release()
		delete(s.iterators, req.Id)
	}
	s.iteratorLock.Unlock()
	if !ok {
		return &IteratorReleaseResponse{Err: Error_ERROR_NOT_FOUND}, nil
	}
	return &IteratorReleaseResponse{Err: Error_ERROR_UNSPECIFIED}, nil
}

// Register installs `s` on `r` so a remote DatabaseClient can dial in.
// `r` is a method-name → handler registry; the ZAP transport in
// vms/rpcchainvm/zap satisfies this.
type HandlerRegistry interface {
	HandleFunc(method string, handler func(context.Context, any, any) error)
}

// Register binds every database method on `r`. The handler signatures
// reflect the `(ctx, request) -> response` shape of every rpcdb call;
// the transport is responsible for unmarshaling the request and
// marshaling the response.
func (s *DatabaseServer) Register(r HandlerRegistry) {
	r.HandleFunc(methodHas, func(ctx context.Context, in any, out any) error {
		req := in.(*HasRequest)
		resp, err := s.Has(ctx, req)
		if err != nil {
			return err
		}
		*(out.(*HasResponse)) = *resp
		return nil
	})
	r.HandleFunc(methodGet, func(ctx context.Context, in any, out any) error {
		req := in.(*GetRequest)
		resp, err := s.Get(ctx, req)
		if err != nil {
			return err
		}
		*(out.(*GetResponse)) = *resp
		return nil
	})
	r.HandleFunc(methodPut, func(ctx context.Context, in any, out any) error {
		req := in.(*PutRequest)
		resp, err := s.Put(ctx, req)
		if err != nil {
			return err
		}
		*(out.(*PutResponse)) = *resp
		return nil
	})
	r.HandleFunc(methodDelete, func(ctx context.Context, in any, out any) error {
		req := in.(*DeleteRequest)
		resp, err := s.Delete(ctx, req)
		if err != nil {
			return err
		}
		*(out.(*DeleteResponse)) = *resp
		return nil
	})
	r.HandleFunc(methodWriteBatch, func(ctx context.Context, in any, out any) error {
		req := in.(*WriteBatchRequest)
		resp, err := s.WriteBatch(ctx, req)
		if err != nil {
			return err
		}
		*(out.(*WriteBatchResponse)) = *resp
		return nil
	})
	r.HandleFunc(methodCompact, func(ctx context.Context, in any, out any) error {
		req := in.(*CompactRequest)
		resp, err := s.Compact(ctx, req)
		if err != nil {
			return err
		}
		*(out.(*CompactResponse)) = *resp
		return nil
	})
	r.HandleFunc(methodClose, func(ctx context.Context, in any, out any) error {
		req := in.(*CloseRequest)
		resp, err := s.Close(ctx, req)
		if err != nil {
			return err
		}
		*(out.(*CloseResponse)) = *resp
		return nil
	})
	r.HandleFunc(methodHealthCheck, func(ctx context.Context, _ any, out any) error {
		resp, err := s.HealthCheck(ctx)
		if err != nil {
			return err
		}
		*(out.(*HealthCheckResponse)) = *resp
		return nil
	})
	r.HandleFunc(methodNewIteratorWithStartAndPrefix, func(ctx context.Context, in any, out any) error {
		req := in.(*NewIteratorWithStartAndPrefixRequest)
		resp, err := s.NewIteratorWithStartAndPrefix(ctx, req)
		if err != nil {
			return err
		}
		*(out.(*NewIteratorWithStartAndPrefixResponse)) = *resp
		return nil
	})
	r.HandleFunc(methodIteratorNext, func(ctx context.Context, in any, out any) error {
		req := in.(*IteratorNextRequest)
		resp, err := s.IteratorNext(ctx, req)
		if err != nil {
			return err
		}
		*(out.(*IteratorNextResponse)) = *resp
		return nil
	})
	r.HandleFunc(methodIteratorError, func(ctx context.Context, in any, out any) error {
		req := in.(*IteratorErrorRequest)
		resp, err := s.IteratorError(ctx, req)
		if err != nil {
			return err
		}
		*(out.(*IteratorErrorResponse)) = *resp
		return nil
	})
	r.HandleFunc(methodIteratorRelease, func(ctx context.Context, in any, out any) error {
		req := in.(*IteratorReleaseRequest)
		resp, err := s.IteratorRelease(ctx, req)
		if err != nil {
			return err
		}
		*(out.(*IteratorReleaseResponse)) = *resp
		return nil
	})
}

// DatabaseClient is the plugin-side database that forwards every call
// to a remote DatabaseServer over a ZAP Transport. Implements
// database.Database, so the plugin can drop it in unmodified.
type DatabaseClient struct {
	t Transport
}

// NewClient builds a DatabaseClient that talks to a DatabaseServer
// across `t`.
func NewClient(t Transport) *DatabaseClient {
	return &DatabaseClient{t: t}
}

// Compile-time interface check.
var _ database.Database = (*DatabaseClient)(nil)

// Has forwards rpcdb.Has.
func (c *DatabaseClient) Has(key []byte) (bool, error) {
	resp := &HasResponse{}
	if err := c.t.Call(context.Background(), methodHas, &HasRequest{Key: key}, resp); err != nil {
		return false, err
	}
	if resp.Err != Error_ERROR_UNSPECIFIED {
		return false, rpcErrorToError(resp.Err)
	}
	return resp.Has, nil
}

// Get forwards rpcdb.Get.
func (c *DatabaseClient) Get(key []byte) ([]byte, error) {
	resp := &GetResponse{}
	if err := c.t.Call(context.Background(), methodGet, &GetRequest{Key: key}, resp); err != nil {
		return nil, err
	}
	if resp.Err != Error_ERROR_UNSPECIFIED {
		return nil, rpcErrorToError(resp.Err)
	}
	return resp.Value, nil
}

// Put forwards rpcdb.Put.
func (c *DatabaseClient) Put(key, value []byte) error {
	resp := &PutResponse{}
	if err := c.t.Call(context.Background(), methodPut, &PutRequest{Key: key, Value: value}, resp); err != nil {
		return err
	}
	if resp.Err != Error_ERROR_UNSPECIFIED {
		return rpcErrorToError(resp.Err)
	}
	return nil
}

// Delete forwards rpcdb.Delete.
func (c *DatabaseClient) Delete(key []byte) error {
	resp := &DeleteResponse{}
	if err := c.t.Call(context.Background(), methodDelete, &DeleteRequest{Key: key}, resp); err != nil {
		return err
	}
	if resp.Err != Error_ERROR_UNSPECIFIED {
		return rpcErrorToError(resp.Err)
	}
	return nil
}

// NewBatch returns an in-memory batch that flushes via WriteBatch.
func (c *DatabaseClient) NewBatch() database.Batch {
	return &batch{c: c}
}

// NewIterator returns a forward-only iterator over the entire DB.
func (c *DatabaseClient) NewIterator() database.Iterator {
	return c.NewIteratorWithStartAndPrefix(nil, nil)
}

// NewIteratorWithStart returns a forward-only iterator from start.
func (c *DatabaseClient) NewIteratorWithStart(start []byte) database.Iterator {
	return c.NewIteratorWithStartAndPrefix(start, nil)
}

// NewIteratorWithPrefix returns a forward-only iterator over keys
// matching prefix.
func (c *DatabaseClient) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return c.NewIteratorWithStartAndPrefix(nil, prefix)
}

// NewIteratorWithStartAndPrefix forwards rpcdb.NewIteratorWithStartAndPrefix.
func (c *DatabaseClient) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	resp := &NewIteratorWithStartAndPrefixResponse{}
	err := c.t.Call(context.Background(), methodNewIteratorWithStartAndPrefix,
		&NewIteratorWithStartAndPrefixRequest{Start: start, Prefix: prefix}, resp)
	if err != nil {
		return &iterator{c: c, err: err}
	}
	return &iterator{c: c, id: resp.Id}
}

// Backup is not supported across an RPC boundary.
func (*DatabaseClient) Backup(io.Writer, uint64) (uint64, error) {
	return 0, errors.New("rpcdb: backup not supported")
}

// Load is not supported across an RPC boundary.
func (*DatabaseClient) Load(io.Reader) error {
	return errors.New("rpcdb: load not supported")
}

// Compact forwards rpcdb.Compact.
func (c *DatabaseClient) Compact(start, limit []byte) error {
	resp := &CompactResponse{}
	if err := c.t.Call(context.Background(), methodCompact, &CompactRequest{Start: start, Limit: limit}, resp); err != nil {
		return err
	}
	if resp.Err != Error_ERROR_UNSPECIFIED {
		return rpcErrorToError(resp.Err)
	}
	return nil
}

// Sync is a no-op on the client; the server owns persistence.
func (*DatabaseClient) Sync() error { return nil }

// Close forwards rpcdb.Close.
func (c *DatabaseClient) Close() error {
	resp := &CloseResponse{}
	if err := c.t.Call(context.Background(), methodClose, &CloseRequest{}, resp); err != nil {
		return err
	}
	if resp.Err != Error_ERROR_UNSPECIFIED {
		return rpcErrorToError(resp.Err)
	}
	return nil
}

// HealthCheck forwards rpcdb.HealthCheck.
func (c *DatabaseClient) HealthCheck(ctx context.Context) (interface{}, error) {
	resp := &HealthCheckResponse{}
	if err := c.t.Call(ctx, methodHealthCheck, struct{}{}, resp); err != nil {
		return nil, err
	}
	return resp.Details, nil
}

// batch buffers Put/Delete locally and writes once on flush.
type batch struct {
	c       *DatabaseClient
	puts    []*PutRequest
	deletes []*DeleteRequest
	size    int
}

func (b *batch) Put(key, value []byte) error {
	b.puts = append(b.puts, &PutRequest{Key: key, Value: value})
	b.size += len(key) + len(value)
	return nil
}

func (b *batch) Delete(key []byte) error {
	b.deletes = append(b.deletes, &DeleteRequest{Key: key})
	b.size += len(key)
	return nil
}

func (b *batch) Size() int { return b.size }

func (b *batch) Write() error {
	resp := &WriteBatchResponse{}
	if err := b.c.t.Call(context.Background(), methodWriteBatch, &WriteBatchRequest{Puts: b.puts, Deletes: b.deletes}, resp); err != nil {
		return err
	}
	if resp.Err != Error_ERROR_UNSPECIFIED {
		return rpcErrorToError(resp.Err)
	}
	return nil
}

func (b *batch) Reset() {
	b.puts = b.puts[:0]
	b.deletes = b.deletes[:0]
	b.size = 0
}

func (b *batch) Replay(w database.KeyValueWriterDeleter) error {
	for _, p := range b.puts {
		if err := w.Put(p.Key, p.Value); err != nil {
			return err
		}
	}
	for _, d := range b.deletes {
		if err := w.Delete(d.Key); err != nil {
			return err
		}
	}
	return nil
}

func (b *batch) Inner() database.Batch { return b }

// iterator paginates IteratorNext calls.
type iterator struct {
	c         *DatabaseClient
	id        uint64
	data      []*PutRequest
	dataIndex int
	err       error
	closed    bool
}

func (it *iterator) Next() bool {
	if it.err != nil || it.closed {
		return false
	}
	if it.dataIndex < len(it.data)-1 {
		it.dataIndex++
		return true
	}
	resp := &IteratorNextResponse{}
	if err := it.c.t.Call(context.Background(), methodIteratorNext, &IteratorNextRequest{Id: it.id}, resp); err != nil {
		it.err = err
		return false
	}
	if len(resp.Data) == 0 {
		return false
	}
	it.data = resp.Data
	it.dataIndex = 0
	return true
}

func (it *iterator) Error() error {
	if it.err != nil {
		return it.err
	}
	resp := &IteratorErrorResponse{}
	if err := it.c.t.Call(context.Background(), methodIteratorError, &IteratorErrorRequest{Id: it.id}, resp); err != nil {
		return err
	}
	if resp.Err != Error_ERROR_UNSPECIFIED {
		return rpcErrorToError(resp.Err)
	}
	return nil
}

func (it *iterator) Key() []byte {
	if it.dataIndex >= 0 && it.dataIndex < len(it.data) {
		return it.data[it.dataIndex].Key
	}
	return nil
}

func (it *iterator) Value() []byte {
	if it.dataIndex >= 0 && it.dataIndex < len(it.data) {
		return it.data[it.dataIndex].Value
	}
	return nil
}

func (it *iterator) Release() {
	if it.closed {
		return
	}
	it.closed = true
	resp := &IteratorReleaseResponse{}
	_ = it.c.t.Call(context.Background(), methodIteratorRelease, &IteratorReleaseRequest{Id: it.id}, resp)
}

func errorToRPCError(err error) Error {
	if err == nil {
		return Error_ERROR_UNSPECIFIED
	}
	switch err {
	case database.ErrNotFound:
		return Error_ERROR_NOT_FOUND
	case database.ErrClosed:
		return Error_ERROR_CLOSED
	default:
		return Error_ERROR_CLOSED
	}
}

func rpcErrorToError(err Error) error {
	switch err {
	case Error_ERROR_UNSPECIFIED:
		return nil
	case Error_ERROR_NOT_FOUND:
		return database.ErrNotFound
	case Error_ERROR_CLOSED:
		return database.ErrClosed
	default:
		return database.ErrClosed
	}
}
