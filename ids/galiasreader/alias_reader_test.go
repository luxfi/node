// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package galiasreader

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/ids/idstest"
	"github.com/luxfi/node/vms/rpcchainvm/grpcutils"

	aliasreaderpb "github.com/luxfi/node/proto/pb/aliasreader"
)

func TestInterface(t *testing.T) {
	for _, test := range idstest.AliasTests {
		t.Run(test.Name, func(t *testing.T) {
			require := require.New(t)

			listener, err := grpcutils.NewListener()
			require.NoError(err)
			defer listener.Close()
			serverCloser := grpcutils.ServerCloser{}
			defer serverCloser.Stop()
			w := ids.NewAliaser()

			server := grpcutils.NewServer()
			aliasreaderpb.RegisterAliasReaderServer(server, NewServer(w))
			serverCloser.Add(server)

			go grpcutils.Serve(listener, server)

			conn, err := grpcutils.Dial(listener.Addr().String())
			require.NoError(err)
			defer conn.Close()

			r := NewClient(aliasreaderpb.NewAliasReaderClient(conn))
			test.Test(t, r, w)
		})
	}
}
