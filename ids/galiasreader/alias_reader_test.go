// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package galiasreader

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/vms/rpcchainvm/grpcutils"

	aliasreaderpb "github.com/luxdefi/node/proto/pb/aliasreader"
)

func TestInterface(t *testing.T) {
	require := require.New(t)

	for _, test := range ids.AliasTests {
		listener, err := grpcutils.NewListener()
		require.NoError(err)
		serverCloser := grpcutils.ServerCloser{}
		w := ids.NewAliaser()

		server := grpcutils.NewServer()
		aliasreaderpb.RegisterAliasReaderServer(server, NewServer(w))
		serverCloser.Add(server)

		go grpcutils.Serve(listener, server)

		conn, err := grpcutils.Dial(listener.Addr().String())
		require.NoError(err)

		r := NewClient(aliasreaderpb.NewAliasReaderClient(conn))
		test(require, r, w)

		serverCloser.Stop()
		_ = conn.Close()
		_ = listener.Close()
	}
}
