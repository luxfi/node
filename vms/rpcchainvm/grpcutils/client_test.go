// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package grpcutils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
)

func TestDialOptsSmoke(t *testing.T) {
	require := require.New(t)

	opts := newDialOpts()
	require.Len(opts, 3)

	opts = newDialOpts(
		WithChainUnaryInterceptor(grpc_prometheus.UnaryClientInterceptor),
		WithChainStreamInterceptor(grpc_prometheus.StreamClientInterceptor),
	)
	require.Len(opts, 5)
}

// Test_WaitForReady shows the expected results from the DialOption during
// client creation.  If true the client will block and wait forever for the
// server to become Ready even if the listener is closed.
// ref. https://github.com/grpc/grpc/blob/master/doc/wait-for-ready.md
func TestWaitForReady(t *testing.T) {
	require := require.New(t)

	listener, err := NewListener()
	require.NoError(err)
	defer listener.Close()

	server := NewServer()
	defer server.Stop()

	go func() {
		time.Sleep(100 * time.Millisecond)
		Serve(listener, server)
	}()

	// The default is WaitForReady = true.
	_, err = Dial(listener.Addr().String())
	require.NoError(err)

	// require.NoError(db.Put([]byte("foo"), []byte("bar")))

	noWaitListener, err := NewListener()
	require.NoError(err)
	// close listener causes RPC to fail fast.
	// The client would timeout otherwise.
	_ = noWaitListener.Close()

	// By directly calling `grpc.Dial` rather than `Dial`, the default does not
	// include setting grpc.WaitForReady(true).
	_, err = grpc.Dial(
		noWaitListener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(err)

	// err = db.Put([]byte("foo"), []byte("bar"))
	// status, ok := status.FromError(err)
	// require.True(ok)
	// require.Equal(codes.Unavailable, status.Code())
}

func TestWaitForReadyCallOption(t *testing.T) {

	// require := require.New(t)

	// listener, err := NewListener()
	// require.NoError(err)
	// conn, err := Dial(listener.Addr().String())
	// require.NoError(err)
	// // close listener causes RPC to fail fast.
	// _ = listener.Close()

	// db := pb.NewDatabaseClient(conn)
	// _, err = db.Put(context.Background(), &pb.PutRequest{Key: []byte("foo"), Value: []byte("bar")}, grpc.WaitForReady(false))
	// s, ok := status.FromError(err)
	// fmt.Printf("status: %v\n", s)
	// require.True(ok)
	// require.Equal(codes.Unavailable, s.Code())
}
