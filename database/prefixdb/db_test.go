// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package prefixdb

import (
	"testing"

	"github.com/luxfi/node/database"
	"github.com/luxfi/node/database/memdb"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		db := memdb.New()
		test(t, New([]byte("hello"), db))
		test(t, New([]byte("world"), db))
		test(t, New([]byte("wor"), New([]byte("ld"), db)))
		test(t, New([]byte("ld"), New([]byte("wor"), db)))
		test(t, NewNested([]byte("wor"), New([]byte("ld"), db)))
		test(t, NewNested([]byte("ld"), New([]byte("wor"), db)))
	}
}

func FuzzKeyValue(f *testing.F) {
	database.FuzzKeyValue(f, New([]byte(""), memdb.New()))
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	database.FuzzNewIteratorWithPrefix(f, New([]byte(""), memdb.New()))
}

func FuzzNewIteratorWithStartAndPrefix(f *testing.F) {
	database.FuzzNewIteratorWithStartAndPrefix(f, New([]byte(""), memdb.New()))
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			db := New([]byte("hello"), memdb.New())
			bench(b, db, "prefixdb", keys, values)
		}
	}
}
