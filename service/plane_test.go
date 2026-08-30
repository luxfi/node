// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// The answers cross a real plane carrying their values.
//
// A ZAP field is an offset, and the failure this guards is not a refusal — it is
// a REPLY THAT ARRIVES EMPTY. A struct whose fields are all unexported derives a
// layout with nothing in it, so the call succeeds, reports no error, and the
// value is simply absent. GetNodeIPReply has one field and it was one of those:
// the whole answer was blank, and nothing said so.
//
// [zip.ZAPSchema]'s ledger cannot catch that, because a type with an empty
// layout HAS a layout. The only thing that can is sending a value through a
// socket and reading it back, which is what this does: a real app on a real unix
// socket, encoded by each type's own codec, through the kernel, and compared.
package service_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zap-proto/zip"

	apiadmin "github.com/luxfi/api/admin"
	apihealth "github.com/luxfi/api/health"
	apiinfo "github.com/luxfi/api/info"
	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/service/security"
)

// echo answers with what it was given, so the only thing between the two ends is
// the codec.
func echo[T any](app *zip.App, name string) {
	zip.Post(app, "/v1/echo/"+name, func(_ context.Context, in *T) (*T, error) {
		return in, nil
	}, zip.WithOperationID(name))
}

func plane(t *testing.T) *zip.Conn {
	t.Helper()
	app := zip.New(zip.Config{AppName: "plane", DisableStartupMessage: true})
	echo[apiinfo.GetNodeIPReply](app, "nodeIP")
	echo[apiinfo.GetNodeIDReply](app, "nodeID")
	echo[apiinfo.GetNodeVersionReply](app, "nodeVersion")
	echo[apiinfo.GetVMsReply](app, "vms")
	echo[apiinfo.LPsReply](app, "lps")
	echo[apiinfo.PeersReply](app, "peers")
	echo[apiadmin.ListVMsReply](app, "listVMs")
	echo[apiadmin.LoadVMsReply](app, "loadVMs")
	echo[apiadmin.LoggerLevelReply](app, "loggerLevels")
	echo[apihealth.APIReply](app, "health")
	echo[security.ProfileReply](app, "securityProfile")

	// A short directory, not t.TempDir(): a unix socket path is capped near 104
	// bytes and this test's name alone is most of that.
	dir, err := os.MkdirTemp("", "ops")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	go func() { _ = app.Listen(sock) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	for range 200 {
		if c, err := net.Dial("unix", sock); err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("%s never began listening: %v", sock, err)
	}
	conn, err := zip.Dial(sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestEveryAnswerThatWasBlockedNowCrossesCarryingItsValue is the whole claim of
// this conversion, made checkable. Each of these replies either could not cross
// at all or crossed empty; each is sent full and read back full.
func TestEveryAnswerThatWasBlockedNowCrossesCarryingItsValue(t *testing.T) {
	c := plane(t)
	ctx := context.Background()
	node := ids.NodeID{0: 0xab, 19: 0xcd}
	chain := ids.ID{0: 0xf0, 31: 0x0d}

	// The one that failed silently: netip.AddrPort has no exported field, so the
	// derived layout was empty and this arrived blank with no error.
	t.Run("node ip", func(t *testing.T) {
		in := apiinfo.GetNodeIPReply{IP: "203.0.113.9:9651"}
		out, err := zip.Call[apiinfo.GetNodeIPReply, apiinfo.GetNodeIPReply](ctx, c, "nodeIP", &in)
		require.NoError(t, err)
		require.Equal(t, apitypes.Addr("203.0.113.9:9651"), out.IP)
		require.Equal(t, "203.0.113.9:9651", out.IP.AddrPort().String())
	})

	t.Run("node id", func(t *testing.T) {
		in := apiinfo.GetNodeIDReply{NodeID: node, NodePOP: &apiinfo.ProofOfPossession{PublicKey: "0xaa", ProofOfPossession: "0xbb"}}
		out, err := zip.Call[apiinfo.GetNodeIDReply, apiinfo.GetNodeIDReply](ctx, c, "nodeID", &in)
		require.NoError(t, err)
		require.Equal(t, node, out.NodeID)
		require.NotNil(t, out.NodePOP)
		require.Equal(t, "0xaa", out.NodePOP.PublicKey)
	})

	// The four that were a map.
	t.Run("node version", func(t *testing.T) {
		in := apiinfo.GetNodeVersionReply{
			Version:    "luxd/1.36.178",
			VMVersions: apiinfo.VMVersions{{VM: "platformvm", Version: "v1.2.3"}},
		}
		out, err := zip.Call[apiinfo.GetNodeVersionReply, apiinfo.GetNodeVersionReply](ctx, c, "nodeVersion", &in)
		require.NoError(t, err)
		require.Equal(t, in.VMVersions, out.VMVersions)
	})

	t.Run("vms", func(t *testing.T) {
		in := apiinfo.GetVMsReply{
			VMs: apiinfo.VMAliases{{VM: chain, Aliases: []string{"platformvm"}}},
			Fxs: apiinfo.FxNames{{Fx: chain, Name: "secp256k1fx"}},
		}
		out, err := zip.Call[apiinfo.GetVMsReply, apiinfo.GetVMsReply](ctx, c, "vms", &in)
		require.NoError(t, err)
		require.Equal(t, in.VMs, out.VMs)
		require.Equal(t, in.Fxs, out.Fxs)
	})

	t.Run("lps", func(t *testing.T) {
		in := apiinfo.LPsReply{LPs: apiinfo.LPs{{
			Number: 23,
			LP:     apiinfo.LP{SupportWeight: 5, Supporters: []ids.NodeID{node}, AbstainWeight: 7},
		}}}
		out, err := zip.Call[apiinfo.LPsReply, apiinfo.LPsReply](ctx, c, "lps", &in)
		require.NoError(t, err)
		require.Equal(t, in.LPs, out.LPs)
	})

	// peers reached a map through PeerInfo, and carried two addresses and two
	// instants that were empty on the wire besides.
	t.Run("peers", func(t *testing.T) {
		when := apitypes.TimeOf(time.Unix(1753479996, 0).UTC())
		in := apiinfo.PeersReply{NumPeers: 1, Peers: []apiinfo.Peer{{
			PeerInfo: apiinfo.PeerInfo{
				IP:            "203.0.113.9:9651",
				PublicIP:      "198.51.100.4:9651",
				ID:            node,
				Version:       "luxd/1.36.178",
				LastSent:      when,
				LastReceived:  when,
				TrackedChains: []ids.ID{chain},
				SupportedLPs:  []uint32{23},
			},
			Benched: []string{"P"},
		}}}
		out, err := zip.Call[apiinfo.PeersReply, apiinfo.PeersReply](ctx, c, "peers", &in)
		require.NoError(t, err)
		require.Len(t, out.Peers, 1)
		got := out.Peers[0]
		require.Equal(t, apitypes.Addr("203.0.113.9:9651"), got.IP)
		require.Equal(t, apitypes.Addr("198.51.100.4:9651"), got.PublicIP)
		require.Equal(t, node, got.ID)
		require.True(t, got.LastSent.Time().Equal(when.Time()), "the instant arrived as %s", got.LastSent.Time())
		require.Equal(t, []ids.ID{chain}, got.TrackedChains)
		require.Equal(t, []uint32{23}, got.SupportedLPs)
		require.Equal(t, []string{"P"}, got.Benched)
	})

	// admin's three maps.
	t.Run("list vms", func(t *testing.T) {
		in := apiadmin.ListVMsReply{VMs: apiadmin.InstalledVMs{{ID: "vm1", Aliases: []string{"a"}, Path: "/plugins/vm1"}}}
		out, err := zip.Call[apiadmin.ListVMsReply, apiadmin.ListVMsReply](ctx, c, "listVMs", &in)
		require.NoError(t, err)
		require.Equal(t, in.VMs, out.VMs)
	})

	t.Run("load vms", func(t *testing.T) {
		in := apiadmin.LoadVMsReply{
			NewVMs:        apiadmin.LoadedVMs{{VM: chain, Aliases: []string{"a"}}},
			FailedVMs:     apiadmin.FailedVMs{{VM: chain, Error: "no plugin"}},
			ChainsRetried: 3,
		}
		out, err := zip.Call[apiadmin.LoadVMsReply, apiadmin.LoadVMsReply](ctx, c, "loadVMs", &in)
		require.NoError(t, err)
		require.Equal(t, in.NewVMs, out.NewVMs)
		require.Equal(t, in.FailedVMs, out.FailedVMs)
		require.Equal(t, 3, out.ChainsRetried)
	})

	t.Run("logger levels", func(t *testing.T) {
		in := apiadmin.LoggerLevelReply{LoggerLevels: apiadmin.LoggerLevels{{
			Logger: "C",
			Levels: apiadmin.LogAndDisplayLevels{LogLevel: "DEBUG", DisplayLevel: "INFO"},
		}}}
		out, err := zip.Call[apiadmin.LoggerLevelReply, apiadmin.LoggerLevelReply](ctx, c, "loggerLevels", &in)
		require.NoError(t, err)
		require.Equal(t, in.LoggerLevels, out.LoggerLevels)
	})

	// health was a map of results whose details were an `any` and whose stamps
	// were time.Time — three things that could not cross, in one reply.
	t.Run("health", func(t *testing.T) {
		when := apitypes.TimeOf(time.Unix(1753479996, 0).UTC())
		said, err := apitypes.RawOf(map[string]any{"availableDiskBytes": 12})
		require.NoError(t, err)
		why := "disk is nearly full"
		in := apihealth.APIReply{Healthy: false, Checks: apihealth.Checks{{
			Name: "diskspace",
			Result: apihealth.Result{
				Details:            said,
				Error:              &why,
				Timestamp:          when,
				Duration:           134597 * time.Nanosecond,
				ContiguousFailures: 4,
			},
		}}}
		out, err := zip.Call[apihealth.APIReply, apihealth.APIReply](ctx, c, "health", &in)
		require.NoError(t, err)
		require.False(t, out.Healthy)
		require.Len(t, out.Checks, 1)
		got := out.Checks[0].Result
		require.JSONEq(t, `{"availableDiskBytes":12}`, string(got.Details))
		require.Equal(t, &why, got.Error)
		require.True(t, got.Timestamp.Time().Equal(when.Time()))
		require.Equal(t, 134597*time.Nanosecond, got.Duration)
		require.Equal(t, int64(4), got.ContiguousFailures)
	})

	// security crossed before and must still, snake_case tags and all.
	t.Run("security profile", func(t *testing.T) {
		in := security.ProfileReply{ProfileID: 2, ProfileName: "STRICT", ProfileHash: "0xdead", PostQuantumEndToEnd: true, ForbidKZG: true}
		out, err := zip.Call[security.ProfileReply, security.ProfileReply](ctx, c, "securityProfile", &in)
		require.NoError(t, err)
		require.Equal(t, in, *out)
	})
}
