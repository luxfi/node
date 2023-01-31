// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/node/database/memdb"
	"github.com/luxdefi/node/database/versiondb"
	"github.com/luxdefi/node/utils/logging"
)

func TestHasIndexReset(t *testing.T) {
	a := require.New(t)

	db := memdb.New()
	vdb := versiondb.New(db)
	s := New(vdb)
	wasReset, err := s.HasIndexReset()
	a.NoError(err)
	a.False(wasReset)
	err = s.ResetHeightIndex(logging.NoLog{}, vdb)
	a.NoError(err)
	wasReset, err = s.HasIndexReset()
	a.NoError(err)
	a.True(wasReset)
}
