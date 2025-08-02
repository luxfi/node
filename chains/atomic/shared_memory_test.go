// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic_test

import (
	"fmt"
	"testing"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/chains/atomic/atomictest"

	. "github.com/luxfi/node/v2/chains/atomic"
)

func TestSharedMemory(t *testing.T) {
	for i, test := range atomictest.SharedMemoryTests {
		// Create fresh IDs for each test to avoid conflicts
		chainID0 := ids.GenerateTestID()
		chainID1 := ids.GenerateTestID()
		
		// Create fresh database instances for each test
		baseDB := memdb.New()

		memoryDB := prefixdb.New([]byte{0}, baseDB)
		testDB := prefixdb.New([]byte{1}, baseDB)

		m := NewMemory(memoryDB)

		sm0 := m.NewSharedMemory(chainID0)
		sm1 := m.NewSharedMemory(chainID1)

		// Run test with a subtest name for better test output
		testName := fmt.Sprintf("Test%d", i)
		t.Run(testName, func(t *testing.T) {
			test(t, chainID0, chainID1, sm0, sm1, testDB)
		})
	}
}
