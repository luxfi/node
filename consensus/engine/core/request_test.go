// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
)

func TestRequestJSONMarshal(t *testing.T) {
	requestMap := map[Request]ids.ID{
		{
			NodeID:    ids.GenerateTestNodeID(),
			RequestID: 12345,
		}: ids.GenerateTestID(),
	}
	_, err := json.Marshal(requestMap)
	require.NoError(t, err)
}
