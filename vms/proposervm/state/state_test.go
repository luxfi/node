// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/luxd/database/memdb"
	"github.com/luxdefi/luxd/database/versiondb"
)

func TestState(t *testing.T) {
	a := require.New(t)

	db := memdb.New()
	vdb := versiondb.New(db)
	s := New(vdb)

	testBlockState(a, s)
	testChainState(a, s)
}

func TestMeteredState(t *testing.T) {
	a := require.New(t)

	db := memdb.New()
	vdb := versiondb.New(db)
	s, err := NewMetered(vdb, "", prometheus.NewRegistry())
	a.NoError(err)

	testBlockState(a, s)
	testChainState(a, s)
}
