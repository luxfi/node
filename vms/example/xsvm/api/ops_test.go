// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	server "github.com/luxfi/node/server/http"
	"github.com/luxfi/node/vms/example/xsvm/genesis"
	"github.com/luxfi/node/vms/example/xsvm/state"
	"github.com/luxfi/runtime"
)

// TestClientReadsWhatTheServerAnswers drives the shipped client against the
// shipped ops over real HTTP, mounted where the node mounts them. The two
// halves are written apart — one names the paths, the other builds the query —
// so what proves they still agree is a round trip, not either one alone.
//
// Balance is the one that matters: its argument is two ids of different widths,
// so a reply carrying the seeded number is the whole path working — the query
// written from the op In, read back into it, and the answer decoded.
func TestClientReadsWhatTheServerAnswers(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	address := ids.ShortID{1, 2, 3}
	asset := ids.ID{4, 5, 6}
	require.NoError(state.SetBalance(db, address, asset, 12345))

	chainID := ids.ID{7, 8, 9}
	handler, err := server.Mount(NewServer(
		&runtime.Runtime{NetworkID: 42, ChainID: chainID},
		&genesis.Genesis{},
		db,
		nil,
		nil,
	).Ops(log.NoLog{}))
	require.NoError(err)

	const alias = "X"
	at := server.Chain("", alias) + server.Ops
	mux := http.NewServeMux()
	mux.Handle(at+"/", http.StripPrefix(at, handler))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := NewClient(ts.URL, alias)
	ctx := context.Background()

	networkID, answeredChain, _, err := client.Network(ctx)
	require.NoError(err)
	require.Equal(uint32(42), networkID)
	require.Equal(chainID, answeredChain)

	balance, err := client.Balance(ctx, address, asset)
	require.NoError(err)
	require.Equal(uint64(12345), balance)

	// An address the chain has never seen holds nothing, and says so rather
	// than refusing.
	balance, err = client.Balance(ctx, ids.ShortID{9, 9, 9}, asset)
	require.NoError(err)
	require.Zero(balance)

	// The one write is a POST at server.Relay, carrying the op In as the body.
	// Parsing refuses these bytes long before a builder is reached, so what
	// this pins is the route and the binding: a 404 would mean the address
	// moved out from under the client.
	err = client.send(ctx, server.Relay, &IssueTxArgs{Tx: []byte("not a transaction")}, new(IssueTxReply))
	require.Error(err)
	require.NotContains(err.Error(), "404")
}
