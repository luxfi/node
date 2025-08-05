package factory

import (
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/database"
	"github.com/luxfi/node/utils/logging"
)

func TestFactory_AllDatabaseTypes(t *testing.T) {
	tests := []struct {
		name   string
		dbType string
	}{
		{
			name:   "leveldb",
			dbType: "leveldb",
		},
		{
			name:   "memdb",
			dbType: "memdb",
		},
		{
			name:   "pebbledb",
			dbType: "pebbledb",
		},
		{
			name:   "badgerdb",
			dbType: "badgerdb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory for file-based databases
			tmpDir := t.TempDir()
			
			// Create logger
			log := logging.NoLog{}
			
			// Create registry
			reg := prometheus.NewRegistry()
			
			// For memdb, we don't need a path
			path := tmpDir
			if tt.dbType == "memdb" {
				path = ""
			}
			
			// Create database
			db, err := New(tt.dbType, path, false, nil, reg, log, "test", "db")
			require.NoError(t, err)
			require.NotNil(t, db)
			
			// Test basic operations
			testKey := []byte("test-key")
			testValue := []byte("test-value")
			
			// Put
			err = db.Put(testKey, testValue)
			require.NoError(t, err)
			
			// Get
			value, err := db.Get(testKey)
			require.NoError(t, err)
			require.Equal(t, testValue, value)
			
			// Has
			has, err := db.Has(testKey)
			require.NoError(t, err)
			require.True(t, has)
			
			// Delete
			err = db.Delete(testKey)
			require.NoError(t, err)
			
			// Verify deleted
			has, err = db.Has(testKey)
			require.NoError(t, err)
			require.False(t, has)
			
			// Close
			err = db.Close()
			require.NoError(t, err)
		})
	}
}

func TestFactory_UnknownDatabase(t *testing.T) {
	log := logging.NoLog{}
	reg := prometheus.NewRegistry()
	
	db, err := New("unknowndb", "", false, nil, reg, log, "test", "db")
	require.Error(t, err)
	require.Nil(t, db)
	require.Contains(t, err.Error(), "unknown database type")
}

func TestFactory_PerChainConfiguration(t *testing.T) {
	// Test creating different database types for different chains
	tmpDir := t.TempDir()
	log := logging.NoLog{}
	reg := prometheus.NewRegistry()
	
	// Create P-Chain with leveldb
	pChainDB, err := New("leveldb", tmpDir+"/P", false, nil, reg, log, "P", "chain")
	require.NoError(t, err)
	require.NotNil(t, pChainDB)
	
	// Create X-Chain with badgerdb
	xChainDB, err := New("badgerdb", tmpDir+"/X", false, nil, reg, log, "X", "chain")
	require.NoError(t, err)
	require.NotNil(t, xChainDB)
	
	// Create C-Chain with pebbledb
	cChainDB, err := New("pebbledb", tmpDir+"/C", false, nil, reg, log, "C", "chain")
	require.NoError(t, err)
	require.NotNil(t, cChainDB)
	
	// Test operations on each
	chains := []struct {
		name string
		db   database.Database
	}{
		{"P-Chain", pChainDB},
		{"X-Chain", xChainDB},
		{"C-Chain", cChainDB},
	}
	
	for i, chain := range chains {
		key := []byte(chain.name + "-key")
		value := []byte(chain.name + "-value-" + string(rune('0'+i)))
		
		err = chain.db.Put(key, value)
		require.NoError(t, err)
		
		got, err := chain.db.Get(key)
		require.NoError(t, err)
		require.Equal(t, value, got)
	}
	
	// Close all databases
	for _, chain := range chains {
		err = chain.db.Close()
		require.NoError(t, err)
	}
}