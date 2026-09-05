// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zap-proto/zip"

	"github.com/luxfi/constants"
	genesisconfigs "github.com/luxfi/genesis/configs"
)

// A chain is named by one alias however a caller capitalises it, and the
// endpoint its RPC sits at is theirs to omit.
//
// The measurement that prompted this: a running node answered
// [Chain]("", "C")+"/rpc" and answered NOTHING for the same address written in
// lower case, or written without the endpoint. Both name the same chain. A
// client that lowercases its URLs, or that drops an endpoint a directory
// listing once told it to write, is not asking for a favour.
//
// Asserted with a handler that reports the path it was reached at, so a 200
// that arrived at the WRONG place cannot pass as a 200 that arrived at the
// right one. Paths are built with [Chain] rather than spelled, which is what
// TestChainAddressBuiltOnlyHere requires of every file in the module.
func TestChainAliasFoldsAndEndpointIsOptional(t *testing.T) {
	require := require.New(t)
	s, serve := newServer(t, 0)

	seen := make(chan string, 16)
	spy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	// Registered ONCE, the way the chain manager registers a C-Chain: upper
	// case, with the RPC at its own endpoint.
	require.NoError(s.AddRoute(spy, constants.ChainAliasPrefix+"/C", rpcEndpoint))
	registered := Chain("", "C") + rpcEndpoint

	for _, asked := range []string{
		Chain("", "C") + rpcEndpoint,
		Chain("", "c") + rpcEndpoint,
		Chain("", "C"),
		Chain("", "c"),
	} {
		_, code := ask(t, serve, asked)
		require.Equal(http.StatusOK, code, asked)
		require.Equal(registered, <-seen, "asked %s", asked)
	}

	// An alias longer than a letter folds the same way — the fold is one rule,
	// not a table of the aliases someone remembered.
	require.NoError(s.AddRoute(spy, constants.ChainAliasPrefix+"/Zoo", rpcEndpoint))
	for _, alias := range []string{"Zoo", "zoo", "ZOO", "zOo"} {
		_, code := ask(t, serve, Chain("", alias))
		require.Equal(http.StatusOK, code, alias)
		require.Equal(Chain("", "Zoo")+rpcEndpoint, <-seen, "asked %s", alias)
	}
}

// The fold reaches a MOUNTED VM's own paths, not only the mount itself.
//
// A VM names what it serves relative to where it was mounted, so the rewritten
// request has to go through the same "route, then the endpoint it lives under"
// cascade an exactly-spelled one does. Folding only the exact match would leave
// every chain's sub-path answering in one case and 404ing in the other.
func TestAFoldedRequestReachesTheMountedVM(t *testing.T) {
	require := require.New(t)
	s, serve := newServer(t, 0)

	app := zip.New(zip.Config{AppName: "coreth"})
	zip.Get(app, "/height", func(context.Context, *struct{}) (*height, error) {
		return &height{Height: 7}, nil
	})
	handler, err := Mount(app)
	require.NoError(err)
	t.Cleanup(func() { _ = app.Shutdown() })
	require.NoError(s.AddRoute(handler, constants.ChainAliasPrefix+"/C", rpcEndpoint))

	for _, asked := range []string{
		Chain("", "C") + rpcEndpoint + "/height",
		Chain("", "c") + rpcEndpoint + "/height",
	} {
		body, code := ask(t, serve, asked)
		require.Equal(http.StatusOK, code, "%s: %s", asked, body)
		require.JSONEq(`{"height":7}`, body)
	}
}

// Folding a case is not opening a door. A chain this node does not serve is
// still absent however it is spelled, and the words AROUND the alias are
// literals — only the alias is the caller's to spell.
func TestFoldingAnAliasAddsNoRoute(t *testing.T) {
	require := require.New(t)
	s, serve := newServer(t, 0)

	spy := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	require.NoError(s.AddRoute(spy, constants.ChainAliasPrefix+"/C", rpcEndpoint))

	for _, dead := range []string{
		// A chain nobody registered, in either case.
		Chain("", "zzz") + rpcEndpoint,
		Chain("", "ZZZ"),
		// An empty alias is not an alias.
		Chain("", ""),
		// An endpoint this chain does not serve.
		Chain("", "c") + "/nowhere",
		Chain("", "C") + "/nowhere",
		// The prefix itself, upper-cased: a different path, not a different
		// capitalisation of this one.
		strings.ToUpper(chainPrefix) + "c" + rpcEndpoint,
	} {
		body, code := ask(t, serve, dead)
		require.Equal(http.StatusNotFound, code, "%s answers: %s", dead, body)
	}
}

// A node serves its own network's chains and no other network's.
//
// The measurement that prompted this: five nodes run --network-id=36963 —
// Hanzo's network — and answer at [Chain]("", "c") while eth_chainId says
// 0x9063. C-Chain is the Lux primary network's EVM and no one else's, so both
// answers cannot be true, and the alias is the half that is wrong. A caller
// reading it cannot tell which network replied.
//
// Asserted on the WRITE side as well as the read: a foreign alias is REFUSED a
// route, so the 404 is the absence of a route rather than a second rule that
// could come to disagree with the first. A read-side check alone would let an
// exactly-spelled foreign alias through, because an exact path is matched
// before any of this is consulted.
func TestANodeServesItsOwnNetworksChains(t *testing.T) {
	_, lux := chainID(t, genesisconfigs.FamilyLux, genesisconfigs.MainnetID)
	_, luxTest := chainID(t, genesisconfigs.FamilyLux, genesisconfigs.TestnetID)
	hanzoID, hanzo := chainID(t, genesisconfigs.FamilyHanzo, genesisconfigs.MainnetID)
	_, zoo := chainID(t, genesisconfigs.FamilyZoo, genesisconfigs.MainnetID)

	for _, network := range []struct {
		name string
		// id is what the node was started with. On the Lux primary network
		// that is 1/2/3/1337; on a sovereign L1 it is the EVM chain id itself,
		// which is what lets a node's own id say which chains are its own.
		id uint32
		// own registers and answers, however it is spelled.
		own []string
		// foreign is refused a route and answers nothing.
		foreign []string
	}{
		{
			name: "hanzo",
			id:   hanzoID,
			own:  []string{"hanzo", hanzo},
			// Not just `c`: the name the chain carries, the letter, and the
			// other brand a shared binary could mint.
			foreign: []string{"c", "c-chain", "zoo", zoo, lux, luxTest},
		},
		{
			name: "lux mainnet",
			id:   constants.MainnetID,
			// P and X name a role every network's own primary network fills,
			// not a brand, so they are nobody's to withhold.
			own:     []string{"c", "p", "x", lux},
			foreign: []string{"hanzo", hanzo, "zoo", zoo, luxTest},
		},
	} {
		t.Run(network.name, func(t *testing.T) {
			require := require.New(t)
			s, serve := newServer(t, network.id)

			// Reports the path it was reached at, so a 200 that arrived at the
			// WRONG chain cannot pass as a 200 at the right one.
			spy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(r.URL.Path))
			})

			for _, alias := range network.own {
				require.NoError(s.AddRoute(spy, constants.ChainAliasPrefix+"/"+alias, rpcEndpoint), alias)

				// Owning a name does not change how it is spelled: the fold
				// and the optional endpoint hold here as everywhere.
				for _, asked := range []string{
					Chain("", alias) + rpcEndpoint,
					Chain("", strings.ToUpper(alias)) + rpcEndpoint,
					Chain("", alias),
					Chain("", strings.ToUpper(alias)),
				} {
					body, code := ask(t, serve, asked)
					require.Equal(http.StatusOK, code, "%s answers: %s", asked, body)
					require.Equal(Chain("", alias)+rpcEndpoint, body, "asked %s", asked)
				}
			}

			for _, alias := range network.foreign {
				require.ErrorIs(
					s.AddRoute(spy, constants.ChainAliasPrefix+"/"+alias, rpcEndpoint),
					errAnotherNetwork, alias,
				)

				for _, asked := range []string{
					Chain("", alias) + rpcEndpoint,
					Chain("", strings.ToUpper(alias)) + rpcEndpoint,
					Chain("", alias),
					Chain("", strings.ToUpper(alias)),
				} {
					body, code := ask(t, serve, asked)
					require.Equal(http.StatusNotFound, code, "%s answers: %s", asked, body)
				}
			}
		})
	}
}

// Root is this network's own chain, so a wallet given the bare host reaches the
// EVM the node runs. It was the C-Chain's, on every network, which is the same
// mistake as the alias: a Hanzo node handed an ethers client a chain named for
// the network it is not on.
func TestRootServesTheNodesOwnChain(t *testing.T) {
	hanzoID, _ := chainID(t, genesisconfigs.FamilyHanzo, genesisconfigs.MainnetID)

	for _, network := range []struct {
		name, alias string
		id          uint32
	}{
		{name: "hanzo", alias: "hanzo", id: hanzoID},
		{name: "lux mainnet", alias: "c", id: constants.MainnetID},
	} {
		t.Run(network.name, func(t *testing.T) {
			require := require.New(t)
			s, serve := newServer(t, network.id)

			spy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(r.URL.Path))
			})
			require.NoError(s.AddRoute(spy, constants.ChainAliasPrefix+"/"+network.alias, rpcEndpoint))

			body, code := post(t, serve, "/")
			require.Equal(http.StatusOK, code, body)
			require.Equal(Chain("", network.alias)+rpcEndpoint, body)
		})
	}
}

// A node with no chain of its own says so in the language the caller is
// speaking, rather than handing a wallet a 404 page to parse.
func TestRootWithoutTheOwnChainAnswersJSONRPC(t *testing.T) {
	require := require.New(t)
	_, serve := newServer(t, constants.MainnetID)

	body, code := post(t, serve, "/")
	require.Equal(http.StatusServiceUnavailable, code)
	require.JSONEq(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"chain not available"}}`, body)
}

// chainID is the EVM chain id a brand runs in an environment, as the node id a
// sovereign L1 is started with and as the decimal a caller would type. Read
// from the table rather than copied out of it, so a test cannot agree with a
// second copy of the map while disagreeing with the one the node reads.
func chainID(t *testing.T, family genesisconfigs.NetworkFamily, env uint32) (uint32, string) {
	t.Helper()
	id, ok := genesisconfigs.EVMChainID(family, env)
	require.True(t, ok, "%s runs no EVM on network %d", family, env)
	return uint32(id), strconv.FormatUint(id, 10)
}

func post(t *testing.T, handler http.Handler, path string) (string, int) {
	t.Helper()
	rec := httptest.NewRecorder()
	call := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_chainId"}`)
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, call))
	body, err := io.ReadAll(rec.Result().Body)
	require.NoError(t, err)
	return string(body), rec.Code
}

// An alias for another network's chain is refused, and leaves nothing behind.
//
// An alias is not applied where it is asked for: it is remembered, and applied
// again every time the chain it names registers another endpoint. Recording one
// that can never be served would turn a single refusal into an error the node
// repeats for as long as it runs — and would reserve the name against the chain
// that legitimately wants it.
//
// This is the path the boot-time alias table and the admin alias call both take.
func TestAnAliasForAnotherNetworkIsRefusedWholesale(t *testing.T) {
	require := require.New(t)
	s, serve := newServer(t, constants.MainnetID)

	spy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.Path))
	})

	require.ErrorIs(
		s.AddAliases(constants.ChainAliasPrefix+"/c", constants.ChainAliasPrefix+"/zoo"),
		errAnotherNetwork,
	)

	// Nothing was reserved, so the chain that owns the name it was asked for
	// still registers, and the refused one still answers nothing.
	require.NoError(s.AddRoute(spy, constants.ChainAliasPrefix+"/c", rpcEndpoint))

	body, code := ask(t, serve, Chain("", "c")+rpcEndpoint)
	require.Equal(http.StatusOK, code, body)
	require.Equal(Chain("", "c")+rpcEndpoint, body)

	body, code = ask(t, serve, Chain("", "zoo")+rpcEndpoint)
	require.Equal(http.StatusNotFound, code, "answers: %s", body)
}
