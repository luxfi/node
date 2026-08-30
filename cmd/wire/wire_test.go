// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zap-proto/zip"

	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"

	apitypes "github.com/luxfi/api/types"
	utxo "github.com/luxfi/utxo"
	validators "github.com/luxfi/validators"

	"github.com/luxfi/node/indexer"
	"github.com/luxfi/node/upgrade"
	xsvm "github.com/luxfi/node/vms/example/xsvm/api"
	"github.com/luxfi/node/vms/platformvm"
	"github.com/luxfi/node/vms/xvm"
)

// wired is [zip.Wire]. A type implementing it answers for its own bytes at the
// root of Marshal and Unmarshal, and nothing below reflects.
var wired = reflect.TypeOf((*zip.Wire)(nil)).Elem()

// TestEveryMessageEitherStatesItsWireOrHasNone is the standing count. A message
// this node's RPC answers with is in exactly one of two states, and there is no
// third: it states its wire, or the derivation refuses its Go type outright and
// answering that is a change to the TYPE.
//
// Read it as the measurement it is. The refused list is the work left, and it
// shrinks only when a map or an interface in a reply becomes something with a
// layout.
func TestEveryMessageEitherStatesItsWireOrHasNone(t *testing.T) {
	var stated, refused []string
	for _, m := range messages {
		typ := reflect.TypeOf(m).Elem()
		name := typ.PkgPath() + "." + typ.Name()
		_, err := zip.LayoutOf(typ)
		switch {
		case reflect.PointerTo(typ).Implements(wired):
			stated = append(stated, name)
		case err != nil:
			refused = append(refused, name)
		default:
			t.Errorf("%s has a layout and states no wire: it can be generated and was not.\n"+
				"Run: go run ./cmd/wire", name)
		}
	}
	sort.Strings(refused)
	t.Logf("%d messages state their wire; %d have no layout to state:", len(stated), len(refused))
	for _, r := range refused {
		t.Log("  " + r)
	}
}

// TestAnIDCrossesThePlane is the end-to-end proof, and the only one that counts:
// a real zip app on a real unix socket, one op per message family, each carrying
// an ids.ID or an ids.NodeID. The plane encodes with the type's own codec, the
// bytes go through the kernel, and the id comes back the same 32 bytes.
//
// Before the codecs it did not: a fixed array is refused by the derived encoder,
// so every one of these calls answered with an encode failure instead of a reply.
func TestAnIDCrossesThePlane(t *testing.T) {
	id := ids.ID{0: 0xf0, 15: 0x5a, 31: 0x0d}
	node := ids.NodeID{0: 0xab, 19: 0xcd}

	app := zip.New(zip.Config{AppName: "wire", DisableStartupMessage: true})
	echo(app, "tx", func(in *apitypes.JSONTxID) *apitypes.JSONTxID { return in })
	echo(app, "block", func(in *apitypes.GetBlockArgs) *apitypes.GetBlockArgs { return in })
	echo(app, "utxoid", func(in *utxo.UTXOID) *utxo.UTXOID { return in })
	echo(app, "validator", func(in *validators.GetValidatorOutput) *validators.GetValidatorOutput { return in })
	echo(app, "stake", func(in *platformvm.GetStakeReply) *platformvm.GetStakeReply { return in })
	echo(app, "validators", func(in *platformvm.GetValidatorsAtReply) *platformvm.GetValidatorsAtReply { return in })
	echo(app, "container", func(in *indexer.FormattedContainer) *indexer.FormattedContainer { return in })
	echo(app, "upgrades", func(in *upgrade.Config) *upgrade.Config { return in })
	echo(app, "asset", func(in *xvm.FormattedAssetID) *xvm.FormattedAssetID { return in })
	echo(app, "network", func(in *xsvm.NetworkReply) *xsvm.NetworkReply { return in })

	sock := filepath.Join(t.TempDir(), "wire.sock")
	go func() { _ = app.Listen(sock) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	listening(t, sock)

	c, err := zip.Dial(sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()

	t.Run("JSONTxID", func(t *testing.T) {
		out, err := zip.Call[apitypes.JSONTxID, apitypes.JSONTxID](ctx, c, "tx", &apitypes.JSONTxID{TxID: id})
		require.NoError(t, err)
		require.Equal(t, id, out.TxID)
	})
	t.Run("GetBlockArgs", func(t *testing.T) {
		in := apitypes.GetBlockArgs{BlockID: id, Encoding: formatting.Hex}
		out, err := zip.Call[apitypes.GetBlockArgs, apitypes.GetBlockArgs](ctx, c, "block", &in)
		require.NoError(t, err)
		require.Equal(t, in, *out)
	})
	t.Run("UTXOID", func(t *testing.T) {
		in := utxo.UTXOID{TxID: id, OutputIndex: 7}
		out, err := zip.Call[utxo.UTXOID, utxo.UTXOID](ctx, c, "utxoid", &in)
		require.NoError(t, err)
		require.Equal(t, id, out.TxID)
		require.Equal(t, uint32(7), out.OutputIndex)
	})
	t.Run("GetValidatorOutput", func(t *testing.T) {
		in := validators.GetValidatorOutput{NodeID: node, PublicKey: []byte{1, 2}, Weight: 9, TxID: id}
		out, err := zip.Call[validators.GetValidatorOutput, validators.GetValidatorOutput](ctx, c, "validator", &in)
		require.NoError(t, err)
		require.Equal(t, in, *out)
	})
	t.Run("GetStakeReply", func(t *testing.T) {
		in := platformvm.GetStakeReply{
			Staked:   7,
			Stakeds:  platformvm.Amounts{{AssetID: id, Value: 7}},
			Outputs:  []string{"0xdead"},
			Encoding: formatting.Hex,
		}
		out, err := zip.Call[platformvm.GetStakeReply, platformvm.GetStakeReply](ctx, c, "stake", &in)
		require.NoError(t, err)
		require.Len(t, out.Stakeds, 1)
		require.Equal(t, id, out.Stakeds[0].AssetID)
		require.Equal(t, []string{"0xdead"}, out.Outputs)
	})
	t.Run("GetValidatorsAtReply", func(t *testing.T) {
		in := platformvm.GetValidatorsAtReply{Validators: platformvm.ValidatorSet{
			{NodeID: node, PublicKey: []byte{3, 4}, Weight: 5},
		}}
		out, err := zip.Call[platformvm.GetValidatorsAtReply, platformvm.GetValidatorsAtReply](ctx, c, "validators", &in)
		require.NoError(t, err)
		require.Len(t, out.Validators, 1)
		require.Equal(t, node, out.Validators[0].NodeID)
		require.Equal(t, []byte{3, 4}, []byte(out.Validators[0].PublicKey))
	})
	t.Run("FormattedContainer", func(t *testing.T) {
		in := indexer.FormattedContainer{ID: id, Bytes: "0xdead", Index: 3}
		out, err := zip.Call[indexer.FormattedContainer, indexer.FormattedContainer](ctx, c, "container", &in)
		require.NoError(t, err)
		require.Equal(t, id, out.ID)
		require.Equal(t, "0xdead", out.Bytes)
	})
	t.Run("upgrade.Config", func(t *testing.T) {
		in := upgrade.Config{XChainStopVertexID: id}
		out, err := zip.Call[upgrade.Config, upgrade.Config](ctx, c, "upgrades", &in)
		require.NoError(t, err)
		require.Equal(t, id, out.XChainStopVertexID)
	})
	t.Run("FormattedAssetID", func(t *testing.T) {
		in := xvm.FormattedAssetID{AssetID: id}
		out, err := zip.Call[xvm.FormattedAssetID, xvm.FormattedAssetID](ctx, c, "asset", &in)
		require.NoError(t, err)
		require.Equal(t, id, out.AssetID)
	})
	t.Run("xsvm.NetworkReply", func(t *testing.T) {
		in := xsvm.NetworkReply{NetworkID: 1, ChainID: id}
		out, err := zip.Call[xsvm.NetworkReply, xsvm.NetworkReply](ctx, c, "network", &in)
		require.NoError(t, err)
		require.Equal(t, id, out.ChainID)
	})
}

// echo declares one op that answers with what it was given, so the only thing
// between the two ends is the codec.
func echo[T any](app *zip.App, name string, fn func(*T) *T) {
	zip.Post(app, "/v1/echo/"+name, func(_ context.Context, in *T) (*T, error) {
		return fn(in), nil
	}, zip.WithOperationID(name))
}

func listening(t *testing.T, sock string) {
	t.Helper()
	for range 200 {
		if c, err := net.Dial("unix", sock); err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("%s never began listening: %v", sock, err)
	}
	t.Fatalf("%s never began listening", sock)
}
