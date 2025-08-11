// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rpcdb provides database RPC functionality
package rpcdb

import (
	"context"
	"sync"

	"github.com/luxfi/database"
	rpcdbpb "github.com/luxfi/node/proto/pb/rpcdb"
	"google.golang.org/protobuf/types/known/emptypb"
)

var (
	_ database.Database      = (*DatabaseClient)(nil)
	_ rpcdbpb.DatabaseServer = (*DatabaseServer)(nil)
)

// DatabaseServer is a database server that listens over RPC.
type DatabaseServer struct {
	rpcdbpb.UnsafeDatabaseServer
	db database.Database

	iteratorLock   sync.RWMutex
	nextIteratorID uint64
	iterators      map[uint64]database.Iterator
}

// NewServer returns a new database server
func NewServer(db database.Database) *DatabaseServer {
	return &DatabaseServer{
		db:        db,
		iterators: make(map[uint64]database.Iterator),
	}
}

func (db *DatabaseServer) Has(ctx context.Context, req *rpcdbpb.HasRequest) (*rpcdbpb.HasResponse, error) {
	has, err := db.db.Has(req.Key)
	if err != nil {
		return nil, err
	}
	return &rpcdbpb.HasResponse{
		Has: has,
	}, nil
}

func (db *DatabaseServer) Get(ctx context.Context, req *rpcdbpb.GetRequest) (*rpcdbpb.GetResponse, error) {
	value, err := db.db.Get(req.Key)
	if err != nil {
		if err == database.ErrNotFound {
			errCode := uint32(1)
			return &rpcdbpb.GetResponse{
				Err: &errCode,
			}, nil
		}
		return nil, err
	}
	return &rpcdbpb.GetResponse{
		Value: value,
	}, nil
}

func (db *DatabaseServer) Put(ctx context.Context, req *rpcdbpb.PutRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, db.db.Put(req.Key, req.Value)
}

func (db *DatabaseServer) Delete(ctx context.Context, req *rpcdbpb.DeleteRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, db.db.Delete(req.Key)
}

func (db *DatabaseServer) Compact(ctx context.Context, req *rpcdbpb.CompactRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, db.db.Compact(req.Start, req.Limit)
}

func (db *DatabaseServer) Close(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, db.db.Close()
}

func (db *DatabaseServer) WriteBatch(ctx context.Context, req *rpcdbpb.WriteBatchRequest) (*emptypb.Empty, error) {
	batch := db.db.NewBatch()
	for _, op := range req.Ops {
		if put := op.GetPut(); put != nil {
			if err := batch.Put(put.Key, put.Value); err != nil {
				return nil, err
			}
		} else if del := op.GetDelete(); del != nil {
			if err := batch.Delete(del.Key); err != nil {
				return nil, err
			}
		}
	}
	return &emptypb.Empty{}, batch.Write()
}

func (db *DatabaseServer) NewIteratorWithStartAndPrefix(ctx context.Context, req *rpcdbpb.NewIteratorWithStartAndPrefixRequest) (*rpcdbpb.NewIteratorWithStartAndPrefixResponse, error) {
	it := db.db.NewIteratorWithStartAndPrefix(req.Start, req.Prefix)
	
	db.iteratorLock.Lock()
	id := db.nextIteratorID
	db.nextIteratorID++
	db.iterators[id] = it
	db.iteratorLock.Unlock()
	
	return &rpcdbpb.NewIteratorWithStartAndPrefixResponse{
		Id: id,
	}, nil
}

func (db *DatabaseServer) IteratorNext(ctx context.Context, req *rpcdbpb.IteratorNextRequest) (*rpcdbpb.IteratorNextResponse, error) {
	db.iteratorLock.RLock()
	it, ok := db.iterators[req.Id]
	db.iteratorLock.RUnlock()
	
	if !ok {
		return nil, database.ErrNotFound
	}
	
	foundNext := it.Next()
	resp := &rpcdbpb.IteratorNextResponse{
		FoundNext: foundNext,
	}
	
	if foundNext {
		resp.Key = it.Key()
		resp.Value = it.Value()
	}
	
	return resp, it.Error()
}

func (db *DatabaseServer) IteratorError(ctx context.Context, req *rpcdbpb.IteratorErrorRequest) (*rpcdbpb.IteratorErrorResponse, error) {
	db.iteratorLock.RLock()
	it, ok := db.iterators[req.Id]
	db.iteratorLock.RUnlock()
	
	if !ok {
		return nil, database.ErrNotFound
	}
	
	return &rpcdbpb.IteratorErrorResponse{}, it.Error()
}

func (db *DatabaseServer) IteratorRelease(ctx context.Context, req *rpcdbpb.IteratorReleaseRequest) (*emptypb.Empty, error) {
	db.iteratorLock.Lock()
	it, ok := db.iterators[req.Id]
	if ok {
		it.Release()
		delete(db.iterators, req.Id)
	}
	db.iteratorLock.Unlock()
	
	return &emptypb.Empty{}, nil
}

// DatabaseClient is a database client that talks over RPC.
type DatabaseClient struct {
	client rpcdbpb.DatabaseClient
}

// NewClient returns a new database client
func NewClient(client rpcdbpb.DatabaseClient) *DatabaseClient {
	return &DatabaseClient{
		client: client,
	}
}

func (db *DatabaseClient) Has(key []byte) (bool, error) {
	resp, err := db.client.Has(context.Background(), &rpcdbpb.HasRequest{
		Key: key,
	})
	if err != nil {
		return false, err
	}
	return resp.Has, nil
}

func (db *DatabaseClient) Get(key []byte) ([]byte, error) {
	resp, err := db.client.Get(context.Background(), &rpcdbpb.GetRequest{
		Key: key,
	})
	if err != nil {
		return nil, err
	}
	if resp.Err != nil && *resp.Err != 0 {
		return nil, database.ErrNotFound
	}
	return resp.Value, nil
}

func (db *DatabaseClient) Put(key []byte, value []byte) error {
	_, err := db.client.Put(context.Background(), &rpcdbpb.PutRequest{
		Key:   key,
		Value: value,
	})
	return err
}

func (db *DatabaseClient) Delete(key []byte) error {
	_, err := db.client.Delete(context.Background(), &rpcdbpb.DeleteRequest{
		Key: key,
	})
	return err
}

func (db *DatabaseClient) NewBatch() database.Batch {
	return &batch{
		db: db,
	}
}

func (db *DatabaseClient) NewIterator() database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, nil)
}

func (db *DatabaseClient) NewIteratorWithStart(start []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(start, nil)
}

func (db *DatabaseClient) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	return db.NewIteratorWithStartAndPrefix(nil, prefix)
}

func (db *DatabaseClient) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	resp, err := db.client.NewIteratorWithStartAndPrefix(context.Background(), &rpcdbpb.NewIteratorWithStartAndPrefixRequest{
		Start:  start,
		Prefix: prefix,
	})
	if err != nil {
		return &iterator{
			db:  db,
			err: err,
		}
	}
	return &iterator{
		db: db,
		id: resp.Id,
	}
}

func (db *DatabaseClient) Compact(start []byte, limit []byte) error {
	_, err := db.client.Compact(context.Background(), &rpcdbpb.CompactRequest{
		Start: start,
		Limit: limit,
	})
	return err
}

func (db *DatabaseClient) Close() error {
	_, err := db.client.Close(context.Background(), &emptypb.Empty{})
	return err
}

type batch struct {
	db     *DatabaseClient
	ops    []*rpcdbpb.BatchOp
	size   int
}

func (b *batch) Put(key, value []byte) error {
	b.ops = append(b.ops, &rpcdbpb.BatchOp{
		Op: &rpcdbpb.BatchOp_Put{
			Put: &rpcdbpb.BatchPut{
				Key:   key,
				Value: value,
			},
		},
	})
	b.size += len(key) + len(value)
	return nil
}

func (b *batch) Delete(key []byte) error {
	b.ops = append(b.ops, &rpcdbpb.BatchOp{
		Op: &rpcdbpb.BatchOp_Delete{
			Delete: &rpcdbpb.BatchDelete{
				Key: key,
			},
		},
	})
	b.size += len(key)
	return nil
}

func (b *batch) ValueSize() int {
	return b.size
}

func (b *batch) Write() error {
	_, err := b.db.client.WriteBatch(context.Background(), &rpcdbpb.WriteBatchRequest{
		Ops: b.ops,
	})
	return err
}

func (b *batch) Reset() {
	b.ops = b.ops[:0]
	b.size = 0
}

func (b *batch) Replay(w database.KeyValueWriter) error {
	for _, op := range b.ops {
		if put := op.GetPut(); put != nil {
			if err := w.Put(put.Key, put.Value); err != nil {
				return err
			}
		} else if del := op.GetDelete(); del != nil {
			if err := w.Delete(del.Key); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *batch) Inner() database.Batch {
	return b
}

type iterator struct {
	db    *DatabaseClient
	id    uint64
	key   []byte
	value []byte
	err   error
}

func (it *iterator) Next() bool {
	resp, err := it.db.client.IteratorNext(context.Background(), &rpcdbpb.IteratorNextRequest{
		Id: it.id,
	})
	if err != nil {
		it.err = err
		return false
	}
	it.key = resp.Key
	it.value = resp.Value
	return resp.FoundNext
}

func (it *iterator) Error() error {
	return it.err
}

func (it *iterator) Key() []byte {
	return it.key
}

func (it *iterator) Value() []byte {
	return it.value
}

func (it *iterator) Release() {
	it.db.client.IteratorRelease(context.Background(), &rpcdbpb.IteratorReleaseRequest{
		Id: it.id,
	})
}