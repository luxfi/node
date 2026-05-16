//go:build grpc

// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/luxfi/database"
	rpcdbpb "github.com/luxfi/node/proto/pb/rpcdb"
	rpcdb "github.com/luxfi/protocol/rpcdb"
)

// GRPCServer is the gRPC transport adapter for the rpcdb Service. It
// wraps *Service and translates gRPC's protobuf-generated request /
// response types into the transport-neutral wire types in
// github.com/luxfi/protocol/rpcdb. Pure adapter — no storage logic
// lives here.
type GRPCServer struct {
	rpcdbpb.UnimplementedDatabaseServer
	svc *Service
}

// NewGRPCServer wraps a database.Database for serving over gRPC.
func NewGRPCServer(db database.Database) *GRPCServer {
	return NewGRPCServerFromService(NewService(db))
}

// NewGRPCServerFromService wraps an existing Service for serving over
// gRPC. Symmetric with NewZAPServerFromService.
func NewGRPCServerFromService(svc *Service) *GRPCServer {
	return &GRPCServer{svc: svc}
}

// Compile-time interface check.
var _ rpcdbpb.DatabaseServer = (*GRPCServer)(nil)

func toPbErr(code rpcdb.Error) rpcdbpb.Error {
	switch code {
	case rpcdb.Error_ERROR_NOT_FOUND:
		return rpcdbpb.Error_ERROR_NOT_FOUND
	case rpcdb.Error_ERROR_CLOSED:
		return rpcdbpb.Error_ERROR_CLOSED
	default:
		return rpcdbpb.Error_ERROR_UNSPECIFIED
	}
}

func (g *GRPCServer) Has(ctx context.Context, req *rpcdbpb.HasRequest) (*rpcdbpb.HasResponse, error) {
	resp, err := g.svc.Has(ctx, &rpcdb.HasRequest{Key: req.Key})
	if err != nil {
		return nil, err
	}
	return &rpcdbpb.HasResponse{Has: resp.Has, Err: toPbErr(resp.Err)}, nil
}

func (g *GRPCServer) Get(ctx context.Context, req *rpcdbpb.GetRequest) (*rpcdbpb.GetResponse, error) {
	resp, err := g.svc.Get(ctx, &rpcdb.GetRequest{Key: req.Key})
	if err != nil {
		return nil, err
	}
	return &rpcdbpb.GetResponse{Value: resp.Value, Err: toPbErr(resp.Err)}, nil
}

func (g *GRPCServer) Put(ctx context.Context, req *rpcdbpb.PutRequest) (*rpcdbpb.PutResponse, error) {
	resp, err := g.svc.Put(ctx, &rpcdb.PutRequest{Key: req.Key, Value: req.Value})
	if err != nil {
		return nil, err
	}
	return &rpcdbpb.PutResponse{Err: toPbErr(resp.Err)}, nil
}

func (g *GRPCServer) Delete(ctx context.Context, req *rpcdbpb.DeleteRequest) (*rpcdbpb.DeleteResponse, error) {
	resp, err := g.svc.Delete(ctx, &rpcdb.DeleteRequest{Key: req.Key})
	if err != nil {
		return nil, err
	}
	return &rpcdbpb.DeleteResponse{Err: toPbErr(resp.Err)}, nil
}

func (g *GRPCServer) WriteBatch(ctx context.Context, req *rpcdbpb.WriteBatchRequest) (*rpcdbpb.WriteBatchResponse, error) {
	puts := make([]*rpcdb.PutRequest, len(req.Puts))
	for i, p := range req.Puts {
		puts[i] = &rpcdb.PutRequest{Key: p.Key, Value: p.Value}
	}
	dels := make([]*rpcdb.DeleteRequest, len(req.Deletes))
	for i, d := range req.Deletes {
		dels[i] = &rpcdb.DeleteRequest{Key: d.Key}
	}
	resp, err := g.svc.WriteBatch(ctx, &rpcdb.WriteBatchRequest{Puts: puts, Deletes: dels})
	if err != nil {
		return nil, err
	}
	return &rpcdbpb.WriteBatchResponse{Err: toPbErr(resp.Err)}, nil
}

func (g *GRPCServer) Compact(ctx context.Context, req *rpcdbpb.CompactRequest) (*rpcdbpb.CompactResponse, error) {
	resp, err := g.svc.Compact(ctx, &rpcdb.CompactRequest{Start: req.Start, Limit: req.Limit})
	if err != nil {
		return nil, err
	}
	return &rpcdbpb.CompactResponse{Err: toPbErr(resp.Err)}, nil
}

func (g *GRPCServer) Close(ctx context.Context, _ *rpcdbpb.CloseRequest) (*rpcdbpb.CloseResponse, error) {
	resp, err := g.svc.Close(ctx, &rpcdb.CloseRequest{})
	if err != nil {
		return nil, err
	}
	return &rpcdbpb.CloseResponse{Err: toPbErr(resp.Err)}, nil
}

func (g *GRPCServer) HealthCheck(ctx context.Context, _ *emptypb.Empty) (*rpcdbpb.HealthCheckResponse, error) {
	resp, err := g.svc.HealthCheck(ctx)
	if err != nil {
		return nil, err
	}
	return &rpcdbpb.HealthCheckResponse{Details: resp.Details}, nil
}

func (g *GRPCServer) NewIteratorWithStartAndPrefix(ctx context.Context, req *rpcdbpb.NewIteratorWithStartAndPrefixRequest) (*rpcdbpb.NewIteratorWithStartAndPrefixResponse, error) {
	resp, err := g.svc.NewIteratorWithStartAndPrefix(ctx, &rpcdb.NewIteratorWithStartAndPrefixRequest{Start: req.Start, Prefix: req.Prefix})
	if err != nil {
		return nil, err
	}
	return &rpcdbpb.NewIteratorWithStartAndPrefixResponse{Id: resp.Id}, nil
}

func (g *GRPCServer) IteratorNext(ctx context.Context, req *rpcdbpb.IteratorNextRequest) (*rpcdbpb.IteratorNextResponse, error) {
	resp, err := g.svc.IteratorNext(ctx, &rpcdb.IteratorNextRequest{Id: req.Id})
	if err != nil {
		return nil, err
	}
	data := make([]*rpcdbpb.PutRequest, len(resp.Data))
	for i, e := range resp.Data {
		data[i] = &rpcdbpb.PutRequest{Key: e.Key, Value: e.Value}
	}
	return &rpcdbpb.IteratorNextResponse{Data: data}, nil
}

func (g *GRPCServer) IteratorError(ctx context.Context, req *rpcdbpb.IteratorErrorRequest) (*rpcdbpb.IteratorErrorResponse, error) {
	resp, err := g.svc.IteratorError(ctx, &rpcdb.IteratorErrorRequest{Id: req.Id})
	if err != nil {
		return nil, err
	}
	return &rpcdbpb.IteratorErrorResponse{Err: toPbErr(resp.Err)}, nil
}

func (g *GRPCServer) IteratorRelease(ctx context.Context, req *rpcdbpb.IteratorReleaseRequest) (*rpcdbpb.IteratorReleaseResponse, error) {
	resp, err := g.svc.IteratorRelease(ctx, &rpcdb.IteratorReleaseRequest{Id: req.Id})
	if err != nil {
		return nil, err
	}
	return &rpcdbpb.IteratorReleaseResponse{Err: toPbErr(resp.Err)}, nil
}
