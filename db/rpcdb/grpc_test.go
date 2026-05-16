//go:build grpc

// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcdb

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database/corruptabledb"
	"github.com/luxfi/database/dbtest"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/log"
	"github.com/luxfi/vm/rpc/grpcutils"

	rpcdbpb "github.com/luxfi/node/proto/pb/rpcdb"
)

type testGRPCDatabase struct {
	client *GRPCClient
	server *memdb.Database
}

func setupGRPCDB(t testing.TB) *testGRPCDatabase {
	require := require.New(t)

	db := &testGRPCDatabase{
		server: memdb.New(),
	}

	listener, err := grpcutils.NewListener()
	require.NoError(err)
	serverCloser := grpcutils.ServerCloser{}

	server := grpcutils.NewServer()
	rpcdbpb.RegisterDatabaseServer(server, NewGRPCServer(db.server))
	serverCloser.Add(server)

	go grpcutils.Serve(listener, server)

	conn, err := grpcutils.Dial(listener.Addr().String())
	require.NoError(err)

	db.client = NewGRPCClient(rpcdbpb.NewDatabaseClient(conn))

	t.Cleanup(func() {
		serverCloser.Stop()
		_ = conn.Close()
		_ = listener.Close()
	})

	return db
}

func TestGRPCInterface(t *testing.T) {
	for name, test := range dbtest.Tests {
		t.Run(name, func(t *testing.T) {
			db := setupGRPCDB(t)
			test(t, db.client)
		})
	}
}

func FuzzGRPCKeyValue(f *testing.F) {
	db := setupGRPCDB(f)
	dbtest.FuzzKeyValue(f, db.client)
}

func FuzzGRPCNewIteratorWithPrefix(f *testing.F) {
	db := setupGRPCDB(f)
	dbtest.FuzzNewIteratorWithPrefix(f, db.client)
}

func FuzzGRPCNewIteratorWithStartAndPrefix(f *testing.F) {
	db := setupGRPCDB(f)
	dbtest.FuzzNewIteratorWithStartAndPrefix(f, db.client)
}

func BenchmarkGRPCInterface(b *testing.B) {
	for _, size := range dbtest.BenchmarkSizes {
		keys, values := dbtest.SetupBenchmark(b, size[0], size[1], size[2])
		for name, bench := range dbtest.Benchmarks {
			b.Run(fmt.Sprintf("rpcdb_%d_pairs_%d_keys_%d_values_%s", size[0], size[1], size[2], name), func(b *testing.B) {
				db := setupGRPCDB(b)
				bench(b, db.client, keys, values)
			})
		}
	}
}

func TestGRPCHealthCheck(t *testing.T) {
	scenarios := []struct {
		name       string
		testFn     func(db *corruptabledb.Database) error
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "healthcheck success",
			testFn: func(_ *corruptabledb.Database) error {
				return nil
			},
		},
		{
			name: "healthcheck failed db closed",
			testFn: func(db *corruptabledb.Database) error {
				return db.Close()
			},
			wantErr:    true,
			wantErrMsg: "closed",
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			require := require.New(t)

			baseDB := setupGRPCDB(t)
			db := corruptabledb.New(baseDB.server, log.NoLog{})
			defer db.Close()
			require.NoError(scenario.testFn(db))

			_, err := db.HealthCheck(context.Background())
			if scenario.wantErr {
				require.Error(err) //nolint:forbidigo
				require.Contains(err.Error(), scenario.wantErrMsg)
				return
			}
			require.NoError(err)

			_, err = baseDB.client.HealthCheck(context.Background())
			require.NoError(err)
		})
	}
}
