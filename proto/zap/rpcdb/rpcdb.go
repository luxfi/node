// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rpcdb provides the ZAP-native database RPC types — the
// shapes that flow over the wire when a VM plugin (separate process)
// reads/writes the host's database via the ZAP transport.
//
// This package is the single source of truth for the on-the-wire
// types; the grpc-tagged variant under proto/pb/rpcdb is generated
// from these shapes and is mutually exclusive with the ZAP default
// (you build with either `-tags=grpc` or no tag, never both).
//
// The actual storage backend on the server side is luxfi/database —
// which is currently zapdb under the hood. The same interface is
// presented to plugin clients regardless of which DB engine the host
// runs.
package rpcdb

// Error is the on-the-wire error code returned by every database RPC.
type Error int32

const (
	// Error_ERROR_UNSPECIFIED is the success / no-error sentinel.
	Error_ERROR_UNSPECIFIED Error = 0
	// Error_ERROR_CLOSED maps to database.ErrClosed.
	Error_ERROR_CLOSED Error = 1
	// Error_ERROR_NOT_FOUND maps to database.ErrNotFound.
	Error_ERROR_NOT_FOUND Error = 2
)

func (e Error) String() string {
	switch e {
	case Error_ERROR_CLOSED:
		return "CLOSED"
	case Error_ERROR_NOT_FOUND:
		return "NOT_FOUND"
	default:
		return "UNSPECIFIED"
	}
}

// HasRequest is the wire payload for Database.Has.
type HasRequest struct {
	Key []byte
}

// HasResponse is the wire reply for Database.Has.
type HasResponse struct {
	Has bool
	Err Error
}

// GetRequest is the wire payload for Database.Get.
type GetRequest struct {
	Key []byte
}

// GetResponse is the wire reply for Database.Get.
type GetResponse struct {
	Value []byte
	Err   Error
}

// PutRequest is the wire payload for Database.Put (and the PUT entries
// in WriteBatch / IteratorNext).
type PutRequest struct {
	Key   []byte
	Value []byte
}

// PutResponse is the wire reply for Database.Put.
type PutResponse struct {
	Err Error
}

// DeleteRequest is the wire payload for Database.Delete.
type DeleteRequest struct {
	Key []byte
}

// DeleteResponse is the wire reply for Database.Delete.
type DeleteResponse struct {
	Err Error
}

// WriteBatchRequest is the wire payload for Database.WriteBatch.
type WriteBatchRequest struct {
	Puts    []*PutRequest
	Deletes []*DeleteRequest
}

// WriteBatchResponse is the wire reply for Database.WriteBatch.
type WriteBatchResponse struct {
	Err Error
}

// CompactRequest is the wire payload for Database.Compact.
type CompactRequest struct {
	Start []byte
	Limit []byte
}

// CompactResponse is the wire reply for Database.Compact.
type CompactResponse struct {
	Err Error
}

// CloseRequest is the wire payload for Database.Close.
type CloseRequest struct{}

// CloseResponse is the wire reply for Database.Close.
type CloseResponse struct {
	Err Error
}

// HealthCheckResponse is the wire reply for Database.HealthCheck.
type HealthCheckResponse struct {
	Details []byte
}

// NewIteratorWithStartAndPrefixRequest is the wire payload for
// Database.NewIteratorWithStartAndPrefix.
type NewIteratorWithStartAndPrefixRequest struct {
	Start  []byte
	Prefix []byte
}

// NewIteratorWithStartAndPrefixResponse is the wire reply for
// Database.NewIteratorWithStartAndPrefix.
type NewIteratorWithStartAndPrefixResponse struct {
	Id uint64
}

// IteratorNextRequest is the wire payload for Database.IteratorNext.
type IteratorNextRequest struct {
	Id uint64
}

// IteratorNextResponse is the wire reply for Database.IteratorNext.
// Data is the next batch of (key, value) pairs to read; an empty
// batch indicates the iterator has been exhausted.
type IteratorNextResponse struct {
	Data []*PutRequest
}

// IteratorErrorRequest is the wire payload for Database.IteratorError.
type IteratorErrorRequest struct {
	Id uint64
}

// IteratorErrorResponse is the wire reply for Database.IteratorError.
type IteratorErrorResponse struct {
	Err Error
}

// IteratorReleaseRequest is the wire payload for Database.IteratorRelease.
type IteratorReleaseRequest struct {
	Id uint64
}

// IteratorReleaseResponse is the wire reply for Database.IteratorRelease.
type IteratorReleaseResponse struct {
	Err Error
}
