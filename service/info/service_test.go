// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/luxfi/mock/gomock"
	"github.com/stretchr/testify/require"

	apiinfo "github.com/luxfi/api/info"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
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
	expectedVMRegistry := map[ids.ID][]string{
		id1: []string{alias1},
		id2: []string{alias2},
	}

	req := httptest.NewRequest("POST", "/ext/info", nil)
	resources.mockVMManager.EXPECT().ListFactories(req.Context()).Times(1).Return(vmIDs, nil)
	resources.mockVMManager.EXPECT().PrimaryAlias(req.Context(), id1).Times(1).Return(alias1, nil)
	resources.mockVMManager.EXPECT().PrimaryAlias(req.Context(), id2).Times(1).Return(alias2, nil)

	reply := apiinfo.GetVMsReply{}
	require.NoError(resources.info.GetVMs(req, nil, &reply))
	require.Equal(expectedVMRegistry, reply.VMs)
}

// Tests GetVMs if we fail to list our vms.
func TestGetVMsVMsListFactoriesFails(t *testing.T) {
	resources := initGetVMsTest(t)

	req := httptest.NewRequest("POST", "/ext/info", nil)
	resources.mockVMManager.EXPECT().ListFactories(req.Context()).Times(1).Return(nil, errTest)

	reply := apiinfo.GetVMsReply{}
	err := resources.info.GetVMs(req, nil, &reply)
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

	req := httptest.NewRequest("POST", "/ext/info", nil)
	resources.mockVMManager.EXPECT().ListFactories(req.Context()).Times(1).Return(vmIDs, nil)
	resources.mockVMManager.EXPECT().PrimaryAlias(req.Context(), id1).Times(1).Return(alias1, nil)
	resources.mockVMManager.EXPECT().PrimaryAlias(req.Context(), id2).Times(1).Return("", errTest)

	reply := apiinfo.GetVMsReply{}
	err := resources.info.GetVMs(req, nil, &reply)
	require.NoError(err)
	require.Equal(map[ids.ID][]string{
		id1: []string{alias1},
		id2: nil,
	}, reply.VMs)
}
