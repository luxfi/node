// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/luxd/database"
	"github.com/luxdefi/luxd/database/memdb"
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/choices"
)

func TestStatusState(t *testing.T) {
	require := require.New(t)
	id0 := ids.GenerateTestID()

	db := memdb.New()
	s := NewStatusState(db)

	_, err := s.GetStatus(id0)
	require.Equal(database.ErrNotFound, err)

	_, err = s.GetStatus(id0)
	require.Equal(database.ErrNotFound, err)

	err = s.PutStatus(id0, choices.Accepted)
	require.NoError(err)

	status, err := s.GetStatus(id0)
	require.NoError(err)
	require.Equal(choices.Accepted, status)

	err = s.DeleteStatus(id0)
	require.NoError(err)

	_, err = s.GetStatus(id0)
	require.Equal(database.ErrNotFound, err)

	err = s.PutStatus(id0, choices.Accepted)
	require.NoError(err)

	s = NewStatusState(db)

	status, err = s.GetStatus(id0)
	require.NoError(err)
	require.Equal(choices.Accepted, status)
}
