// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcutils

import (
	"context"
	"errors"
	"io"
	"sync"

	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	maxOps = 100
)

var (
	_ Conn = &redialer{}

	errUnimplemented = errors.New("unimplemented")
)

type Conn interface {
	io.Closer
	grpc.ClientConnInterface
}

type conn struct {
	redialer *redialer

	refs int // number of references to this connection
	ops  int // number of operations performed on this connection
	conn *grpc.ClientConn
}

// Lock is held
func (c *conn) IncRef() {
	c.refs++
	c.ops++
}

// Lock is held
func (c *conn) DecRef() {
	c.refs--
	if !c.redialer.closed && c.refs == 0 {
		c.redialer.closeErrs.Add(c.conn.Close())
		delete(c.redialer.oldConns, c)
	}
}

type redialer struct {
	addr string
	opts []grpc.DialOption

	lock        sync.Mutex
	closed      bool
	currentConn *conn
	oldConns    map[*conn]struct{}
	closeErrs   wrappers.Errs
}

// Lock is held
func (r *redialer) getConn() (*conn, error) {
	if r.currentConn.ops >= maxOps {
		newConn, err := createClientConn(r.addr, r.opts...)
		if err != nil {
			return nil, err
		}

		oldConn := r.currentConn
		r.currentConn = &conn{
			redialer: r,
			refs:     1,
			conn:     newConn,
		}
		r.oldConns[oldConn] = struct{}{}
		oldConn.DecRef()
	}

	r.currentConn.IncRef()
	return r.currentConn, nil
}

func (r *redialer) Invoke(ctx context.Context, method string, args interface{}, reply interface{}, opts ...grpc.CallOption) error {
	r.lock.Lock()
	if r.closed {
		r.lock.Unlock()
		return r.currentConn.conn.Invoke(ctx, method, args, reply, opts...)
	}
	c, err := r.getConn()
	r.lock.Unlock()
	if err != nil {
		return err
	}

	err = c.conn.Invoke(ctx, method, args, reply, opts...)

	r.lock.Lock()
	c.DecRef()
	r.lock.Unlock()
	return err
}

// We don't currently use any Streams
func (r *redialer) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errUnimplemented
}

func (r *redialer) Close() error {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.closed = true

	r.closeErrs.Add(r.currentConn.conn.Close())
	for conn := range r.oldConns {
		r.closeErrs.Add(conn.conn.Close())
		delete(r.oldConns, conn)
	}
	return r.closeErrs.Err
}

func Dial(addr string, opts ...grpc.DialOption) (Conn, error) {
	if len(opts) == 0 {
		opts = append(opts, DefaultDialOptions...)
	}

	c, err := createClientConn(addr, opts...)
	if err != nil {
		return nil, err
	}

	r := &redialer{
		addr: addr,
		opts: opts,
	}
	r.currentConn = &conn{
		redialer: r,
		refs:     1,
		conn:     c,
	}
	return r, nil
}