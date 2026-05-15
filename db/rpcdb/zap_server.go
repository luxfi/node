// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/database"
	rpcdb "github.com/luxfi/proto/rpcdb"
)

// ZAP db channel MsgType IDs. These are the wire-level Layer-A
// dispatch tags for the rpcdb service over ZAP. They live in their
// own listener (one per VM plugin); they do NOT collide with the VM
// lifecycle MsgTypes (1..31) or sender (40..49) or warp (50..59)
// because each listener has its own dispatch table.
//
// Order is the canonical Lane A assignment — the cevm side hard-codes
// these numbers in `RemoteZapDB::call`. Do not reorder.
const (
	MsgDBHas             zapwire.MessageType = 1
	MsgDBGet             zapwire.MessageType = 2
	MsgDBPut             zapwire.MessageType = 3
	MsgDBDelete          zapwire.MessageType = 4
	MsgDBWriteBatch      zapwire.MessageType = 5
	MsgDBCompact         zapwire.MessageType = 6
	MsgDBClose           zapwire.MessageType = 7
	MsgDBHealthCheck     zapwire.MessageType = 8
	MsgDBIteratorNew     zapwire.MessageType = 9
	MsgDBIteratorNext    zapwire.MessageType = 10
	MsgDBIteratorError   zapwire.MessageType = 11
	MsgDBIteratorRelease zapwire.MessageType = 12
)

// Wire-byte values for the Error enum. These match
// rpcdb.Error_ERROR_* but are encoded as a single byte on the ZAP
// wire so the cevm side can decode them without pulling in protobuf.
const (
	dbErrUnspecified uint8 = 0
	dbErrClosed      uint8 = 1
	dbErrNotFound    uint8 = 2
)

func errCodeToByte(code rpcdb.Error) uint8 {
	switch code {
	case rpcdb.Error_ERROR_CLOSED:
		return dbErrClosed
	case rpcdb.Error_ERROR_NOT_FOUND:
		return dbErrNotFound
	default:
		return dbErrUnspecified
	}
}

// ZAPServer is the ZAP transport adapter for the rpcdb Service. It
// owns the listener and the dispatch loop; the actual storage logic
// lives in *Service. To swap the wire format (eg. to add framing
// over a different reliable byte stream) write a new file with a new
// adapter — never edit Service.
type ZAPServer struct {
	svc *Service

	listener *zapwire.Listener
	server   *zapwire.Server

	// closeOnce guards Close so callers can call it from both the
	// client lifecycle and a deferred test cleanup without panicking
	// on a double-close of the underlying listener.
	closeOnce sync.Once
}

// NewZAPServer wraps a database.Database for serving over ZAP.
//
// Equivalent to NewZAPServerFromService(NewService(db)) — kept as a
// one-liner because cevm-side test fixtures and the production
// rpcchainvm/zap path both build it this way.
func NewZAPServer(db database.Database) *ZAPServer {
	return NewZAPServerFromService(NewService(db))
}

// NewZAPServerFromService wraps an existing Service for serving over
// ZAP. Useful when a single Service needs multiple transport adapters
// at once (eg. tests that want both ZAP and direct in-process calls).
func NewZAPServerFromService(svc *Service) *ZAPServer {
	return &ZAPServer{svc: svc}
}

// Listen binds the ZAP listener to addr.
func (s *ZAPServer) Listen(addr string) error {
	listener, err := zapwire.Listen(addr, nil)
	if err != nil {
		return fmt.Errorf("rpcdb zap: listen %s: %w", addr, err)
	}
	s.listener = listener
	s.server = zapwire.NewServer(listener, zapwire.HandlerFunc(s.handle))
	return nil
}

// ListenOn wraps an existing net.Listener (for ephemeral ports etc.).
func (s *ZAPServer) ListenOn(raw net.Listener) {
	s.listener = zapwire.NewListener(raw, nil)
	s.server = zapwire.NewServer(s.listener, zapwire.HandlerFunc(s.handle))
}

// Addr returns the bound address (or nil if not yet listening).
func (s *ZAPServer) Addr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Serve blocks until ctx is cancelled or Close is called.
func (s *ZAPServer) Serve(ctx context.Context) error {
	if s.server == nil {
		return errors.New("rpcdb zap: not initialized — call Listen first")
	}
	return s.server.Serve(ctx)
}

// Close releases all iterators and closes the listener. Caller is
// expected to also cancel the context passed to Serve so the accept
// loop exits cleanly. Safe to call multiple times.
//
// We deliberately do NOT call s.server.Close() because the upstream
// zapwire.Server.Close races with in-flight accept (it nils its conns
// map mid-Serve, causing "assignment to entry in nil map"). Closing
// the listener is enough to make Accept return; ctx cancellation does
// the rest.
func (s *ZAPServer) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.svc.CloseIterators()
		if s.listener != nil {
			err = s.listener.Close()
		}
	})
	return err
}

func (s *ZAPServer) handle(ctx context.Context, msgType zapwire.MessageType, payload []byte) (zapwire.MessageType, []byte, error) {
	switch msgType {
	case MsgDBHas:
		return s.handleHas(ctx, payload)
	case MsgDBGet:
		return s.handleGet(ctx, payload)
	case MsgDBPut:
		return s.handlePut(ctx, payload)
	case MsgDBDelete:
		return s.handleDelete(ctx, payload)
	case MsgDBWriteBatch:
		return s.handleWriteBatch(ctx, payload)
	case MsgDBCompact:
		return s.handleCompact(ctx, payload)
	case MsgDBClose:
		return s.handleClose(ctx, payload)
	case MsgDBHealthCheck:
		return s.handleHealthCheck(ctx)
	case MsgDBIteratorNew:
		return s.handleIteratorNew(ctx, payload)
	case MsgDBIteratorNext:
		return s.handleIteratorNext(ctx, payload)
	case MsgDBIteratorError:
		return s.handleIteratorError(ctx, payload)
	case MsgDBIteratorRelease:
		return s.handleIteratorRelease(ctx, payload)
	default:
		return 0, nil, fmt.Errorf("rpcdb zap: unknown msg type %d", msgType)
	}
}

func (s *ZAPServer) handleHas(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	r := zapwire.NewReader(payload)
	key, err := r.ReadBytes()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb Has decode: %w", err)
	}
	resp, err := s.svc.Has(ctx, &rpcdb.HasRequest{Key: key})
	if err != nil {
		return 0, nil, err
	}
	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	if resp.Has {
		buf.WriteUint8(1)
	} else {
		buf.WriteUint8(0)
	}
	buf.WriteUint8(errCodeToByte(resp.Err))
	return MsgDBHas, append([]byte(nil), buf.Bytes()...), nil
}

func (s *ZAPServer) handleGet(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	r := zapwire.NewReader(payload)
	key, err := r.ReadBytes()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb Get decode: %w", err)
	}
	resp, err := s.svc.Get(ctx, &rpcdb.GetRequest{Key: key})
	if err != nil {
		return 0, nil, err
	}
	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	buf.WriteBytes(resp.Value)
	buf.WriteUint8(errCodeToByte(resp.Err))
	return MsgDBGet, append([]byte(nil), buf.Bytes()...), nil
}

func (s *ZAPServer) handlePut(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	r := zapwire.NewReader(payload)
	key, err := r.ReadBytes()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb Put decode key: %w", err)
	}
	value, err := r.ReadBytes()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb Put decode value: %w", err)
	}
	resp, err := s.svc.Put(ctx, &rpcdb.PutRequest{Key: key, Value: value})
	if err != nil {
		return 0, nil, err
	}
	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	buf.WriteUint8(errCodeToByte(resp.Err))
	return MsgDBPut, append([]byte(nil), buf.Bytes()...), nil
}

func (s *ZAPServer) handleDelete(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	r := zapwire.NewReader(payload)
	key, err := r.ReadBytes()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb Delete decode: %w", err)
	}
	resp, err := s.svc.Delete(ctx, &rpcdb.DeleteRequest{Key: key})
	if err != nil {
		return 0, nil, err
	}
	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	buf.WriteUint8(errCodeToByte(resp.Err))
	return MsgDBDelete, append([]byte(nil), buf.Bytes()...), nil
}

func (s *ZAPServer) handleWriteBatch(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	r := zapwire.NewReader(payload)

	nputs, err := r.ReadUint32()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb WriteBatch decode nputs: %w", err)
	}
	puts := make([]*rpcdb.PutRequest, 0, nputs)
	for i := uint32(0); i < nputs; i++ {
		key, err := r.ReadBytes()
		if err != nil {
			return 0, nil, fmt.Errorf("rpcdb WriteBatch put[%d] key: %w", i, err)
		}
		value, err := r.ReadBytes()
		if err != nil {
			return 0, nil, fmt.Errorf("rpcdb WriteBatch put[%d] value: %w", i, err)
		}
		puts = append(puts, &rpcdb.PutRequest{Key: key, Value: value})
	}

	ndels, err := r.ReadUint32()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb WriteBatch decode ndels: %w", err)
	}
	dels := make([]*rpcdb.DeleteRequest, 0, ndels)
	for i := uint32(0); i < ndels; i++ {
		key, err := r.ReadBytes()
		if err != nil {
			return 0, nil, fmt.Errorf("rpcdb WriteBatch del[%d]: %w", i, err)
		}
		dels = append(dels, &rpcdb.DeleteRequest{Key: key})
	}

	resp, err := s.svc.WriteBatch(ctx, &rpcdb.WriteBatchRequest{Puts: puts, Deletes: dels})
	if err != nil {
		return 0, nil, err
	}
	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	buf.WriteUint8(errCodeToByte(resp.Err))
	return MsgDBWriteBatch, append([]byte(nil), buf.Bytes()...), nil
}

func (s *ZAPServer) handleCompact(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	r := zapwire.NewReader(payload)
	start, err := r.ReadBytes()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb Compact decode start: %w", err)
	}
	limit, err := r.ReadBytes()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb Compact decode limit: %w", err)
	}
	resp, err := s.svc.Compact(ctx, &rpcdb.CompactRequest{Start: start, Limit: limit})
	if err != nil {
		return 0, nil, err
	}
	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	buf.WriteUint8(errCodeToByte(resp.Err))
	return MsgDBCompact, append([]byte(nil), buf.Bytes()...), nil
}

func (s *ZAPServer) handleClose(ctx context.Context, _ []byte) (zapwire.MessageType, []byte, error) {
	resp, err := s.svc.Close(ctx, &rpcdb.CloseRequest{})
	if err != nil {
		return 0, nil, err
	}
	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	buf.WriteUint8(errCodeToByte(resp.Err))
	return MsgDBClose, append([]byte(nil), buf.Bytes()...), nil
}

func (s *ZAPServer) handleHealthCheck(ctx context.Context) (zapwire.MessageType, []byte, error) {
	resp, err := s.svc.HealthCheck(ctx)
	if err != nil {
		return 0, nil, err
	}
	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	buf.WriteBytes(resp.Details)
	return MsgDBHealthCheck, append([]byte(nil), buf.Bytes()...), nil
}

func (s *ZAPServer) handleIteratorNew(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	r := zapwire.NewReader(payload)
	start, err := r.ReadBytes()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb IteratorNew decode start: %w", err)
	}
	prefix, err := r.ReadBytes()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb IteratorNew decode prefix: %w", err)
	}
	resp, err := s.svc.NewIteratorWithStartAndPrefix(ctx, &rpcdb.NewIteratorWithStartAndPrefixRequest{Start: start, Prefix: prefix})
	if err != nil {
		return 0, nil, err
	}
	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	buf.WriteUint64(resp.Id)
	return MsgDBIteratorNew, append([]byte(nil), buf.Bytes()...), nil
}

func (s *ZAPServer) handleIteratorNext(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	r := zapwire.NewReader(payload)
	id, err := r.ReadUint64()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb IteratorNext decode id: %w", err)
	}
	resp, err := s.svc.IteratorNext(ctx, &rpcdb.IteratorNextRequest{Id: id})
	if err != nil {
		return 0, nil, err
	}
	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	buf.WriteUint32(uint32(len(resp.Data)))
	for _, e := range resp.Data {
		buf.WriteBytes(e.Key)
		buf.WriteBytes(e.Value)
	}
	return MsgDBIteratorNext, append([]byte(nil), buf.Bytes()...), nil
}

func (s *ZAPServer) handleIteratorError(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	r := zapwire.NewReader(payload)
	id, err := r.ReadUint64()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb IteratorError decode id: %w", err)
	}
	resp, err := s.svc.IteratorError(ctx, &rpcdb.IteratorErrorRequest{Id: id})
	if err != nil {
		return 0, nil, err
	}
	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	buf.WriteUint8(errCodeToByte(resp.Err))
	return MsgDBIteratorError, append([]byte(nil), buf.Bytes()...), nil
}

func (s *ZAPServer) handleIteratorRelease(ctx context.Context, payload []byte) (zapwire.MessageType, []byte, error) {
	r := zapwire.NewReader(payload)
	id, err := r.ReadUint64()
	if err != nil {
		return 0, nil, fmt.Errorf("rpcdb IteratorRelease decode id: %w", err)
	}
	resp, err := s.svc.IteratorRelease(ctx, &rpcdb.IteratorReleaseRequest{Id: id})
	if err != nil {
		return 0, nil, err
	}
	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	buf.WriteUint8(errCodeToByte(resp.Err))
	return MsgDBIteratorRelease, append([]byte(nil), buf.Bytes()...), nil
}
