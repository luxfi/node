// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linkeddb

import (
	"testing"

	"github.com/luxfi/db/dbtest"
	"github.com/luxfi/db/memdb"
)

func TestInterface(t *testing.T) {
	for name, test := range dbtest.TestsBasic {
		t.Run(name, func(t *testing.T) {
			db := NewDefault(memdb.New())
			test(t, db)
		})
	}
}

func FuzzKeyValue(f *testing.F) {
	db := NewDefault(memdb.New())
	dbtest.FuzzKeyValue(f, db)
}
