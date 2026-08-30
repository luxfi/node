// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"errors"
	"slices"
	"testing"

	"github.com/go-json-experiment/json"
	"github.com/luxfi/mock/gomock"
	"github.com/stretchr/testify/require"

	apiinfo "github.com/luxfi/api/info"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/vmsmock"
)

var errTest = errors.New("non-nil error")

type getVMsTest struct {
	info          *Info
	mockVMManager *vmsmock.Manager
}

func initGetVMsTest(t *testing.T) *getVMsTest {
	ctrl := gomock.NewController(t)
	mockVMManager := vmsmock.NewManager(ctrl)
	return &getVMsTest{
		info: &Info{
			Parameters: Parameters{
				VMManager: mockVMManager,
			},
			log: log.NewNoOpLogger(),
		},
		mockVMManager: mockVMManager,
	}
}

// TestGetNodeVersionConsensusRoundtrip verifies the consensus subobject is
// surfaced via info.getNodeVersion when wired through Parameters and is
// omitted entirely (omitempty) when left unset.
func TestGetNodeVersionConsensusRoundtrip(t *testing.T) {
	require := require.New(t)

	consensus := &apiinfo.ConsensusInfo{
		Mode:       "triple",
		BLS:        true,
		Corona:   true,
		MLDSA:      true,
		PlatformVM: true,
	}

	app := &version.Application{Name: "lux", Major: 1, Minor: 0, Patch: 0}

	info := &Info{
		Parameters: Parameters{
			Version:   app,
			Consensus: consensus,
		},
		log: log.NewNoOpLogger(),
	}

	reply, err := info.nodeVersion(t.Context(), nil)
	require.NoError(err)
	require.NotNil(reply.Consensus)
	require.Equal(*consensus, *reply.Consensus)

	// Mutating the reply value must not bleed back into Parameters — confirms
	// the handler returns a copy, not a shared pointer.
	reply.Consensus.Mode = "classical"
	require.Equal("triple", info.Consensus.Mode)

	// JSON omitempty: when Consensus is unset, it must not appear in the
	// serialised reply at all (legacy clients ignore it; new clients can
	// detect "not advertised" vs "classical" mode).
	bare := &Info{
		Parameters: Parameters{Version: app},
		log:        log.NewNoOpLogger(),
	}
	bareReply, err := bare.nodeVersion(t.Context(), nil)
	require.NoError(err)
	require.Nil(bareReply.Consensus)

	encoded, err := json.Marshal(bareReply)
	require.NoError(err)
	require.NotContains(string(encoded), "consensus")

	encoded, err = json.Marshal(reply)
	require.NoError(err)
	require.Contains(string(encoded), `"consensus"`)
	require.Contains(string(encoded), `"mode":"classical"`)
}

// Tests GetVMs in the happy-case
func TestGetVMsSuccess(t *testing.T) {
	require := require.New(t)

	resources := initGetVMsTest(t)

	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()

	vmIDs := []ids.ID{id1, id2}
	alias1 := "vm1-alias-1"
	alias2 := "vm2-alias-1"
	// Primary alias should be the only returned alias.
	expected := apiinfo.VMAliases{
		{VM: id1, Aliases: []string{alias1}},
		{VM: id2, Aliases: []string{alias2}},
	}
	slices.SortFunc(expected, func(x, y apiinfo.VMAlias) int { return x.VM.Compare(y.VM) })

	ctx := t.Context()
	resources.mockVMManager.EXPECT().ListFactories(ctx).Times(1).Return(vmIDs, nil)
	resources.mockVMManager.EXPECT().PrimaryAlias(ctx, id1).Times(1).Return(alias1, nil)
	resources.mockVMManager.EXPECT().PrimaryAlias(ctx, id2).Times(1).Return(alias2, nil)

	reply, err := resources.info.vms(ctx, nil)
	require.NoError(err)
	require.Equal(expected, reply.VMs)
}

// Tests GetVMs if we fail to list our vms.
func TestGetVMsVMsListFactoriesFails(t *testing.T) {
	resources := initGetVMsTest(t)

	ctx := t.Context()
	resources.mockVMManager.EXPECT().ListFactories(ctx).Times(1).Return(nil, errTest)

	_, err := resources.info.vms(ctx, nil)
	require.ErrorIs(t, err, errTest)
}

// Tests GetVMs when a VM alias lookup fails.
func TestGetVMsGetAliasesFails(t *testing.T) {
	require := require.New(t)
	resources := initGetVMsTest(t)

	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()
	vmIDs := []ids.ID{id1, id2}
	alias1 := "vm1-alias-1"

	ctx := t.Context()
	resources.mockVMManager.EXPECT().ListFactories(ctx).Times(1).Return(vmIDs, nil)
	resources.mockVMManager.EXPECT().PrimaryAlias(ctx, id1).Times(1).Return(alias1, nil)
	resources.mockVMManager.EXPECT().PrimaryAlias(ctx, id2).Times(1).Return("", errTest)

	reply, err := resources.info.vms(ctx, nil)
	require.NoError(err)
	expected := apiinfo.VMAliases{
		{VM: id1, Aliases: []string{alias1}},
		{VM: id2},
	}
	slices.SortFunc(expected, func(x, y apiinfo.VMAlias) int { return x.VM.Compare(y.VM) })
	require.Equal(expected, reply.VMs)
}
