// Database key format helpers for both SubnetEVM and geth formats
package cchainvm

import (
	"encoding/binary"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/ethdb"
)

// Database key prefixes
var (
	// SubnetEVM format prefixes
	subnetHeaderPrefix = []byte{0x68} // 'h' - header data
	subnetBodyPrefix   = []byte{0x62} // 'b' - block body
	subnetHashToNumPrefix = []byte{0x48} // 'H' - hash to number
	
	// Standard geth format prefixes
	gethHeaderPrefix       = []byte{0x30} // headerPrefix + num + hash -> header
	gethBodyPrefix         = []byte{0x31} // blockBodyPrefix + num + hash -> body
	gethHeaderHashSuffix   = []byte{0x32} // headerHashSuffix + num -> hash
	gethHeaderNumberPrefix = []byte{0x33} // headerNumberPrefix + hash -> num
	gethReceiptsPrefix     = []byte{0x34} // receiptsPrefix + num + hash -> receipts
	gethTDSuffix          = []byte{0x35} // headerTDSuffix + num + hash -> TD
)

// DatabaseFormat represents the database format type
type DatabaseFormat int

const (
	UnknownFormat DatabaseFormat = iota
	SubnetEVMFormat
	GethFormat
)

// makeSubnetHeaderKey creates a SubnetEVM format header key
func makeSubnetHeaderKey(number uint64, hash common.Hash) []byte {
	key := make([]byte, 41)
	key[0] = 0x68 // 'h'
	binary.BigEndian.PutUint64(key[1:9], number)
	copy(key[9:], hash[:])
	return key
}

// makeSubnetBodyKey creates a SubnetEVM format body key
func makeSubnetBodyKey(number uint64, hash common.Hash) []byte {
	key := make([]byte, 41)
	key[0] = 0x62 // 'b'
	binary.BigEndian.PutUint64(key[1:9], number)
	copy(key[9:], hash[:])
	return key
}

// makeGethHeaderKey creates a geth format header key
func makeGethHeaderKey(number uint64, hash common.Hash) []byte {
	key := make([]byte, len(gethHeaderPrefix)+8+32)
	copy(key, gethHeaderPrefix)
	binary.BigEndian.PutUint64(key[len(gethHeaderPrefix):], number)
	copy(key[len(gethHeaderPrefix)+8:], hash[:])
	return key
}

// makeGethBodyKey creates a geth format body key
func makeGethBodyKey(number uint64, hash common.Hash) []byte {
	key := make([]byte, len(gethBodyPrefix)+8+32)
	copy(key, gethBodyPrefix)
	binary.BigEndian.PutUint64(key[len(gethBodyPrefix):], number)
	copy(key[len(gethBodyPrefix)+8:], hash[:])
	return key
}

// makeGethCanonicalHashKey creates a geth format canonical hash key
func makeGethCanonicalHashKey(number uint64) []byte {
	key := make([]byte, len(gethHeaderHashSuffix)+8)
	copy(key, gethHeaderHashSuffix)
	binary.BigEndian.PutUint64(key[len(gethHeaderHashSuffix):], number)
	return key
}

// makeGethHashToNumberKey creates a geth format hash-to-number key
func makeGethHashToNumberKey(hash common.Hash) []byte {
	return append(gethHeaderNumberPrefix, hash[:]...)
}

// detectDatabaseFormat attempts to detect the database format
func detectDatabaseFormat(db ethdb.Database) DatabaseFormat {
	// Check for SubnetEVM format (hash-to-number with 'H' prefix)
	iter := db.NewIterator([]byte{0x48}, nil)
	hasSubnetFormat := iter.Next()
	iter.Release()
	
	if hasSubnetFormat {
		return SubnetEVMFormat
	}
	
	// Check for geth format (canonical hash mapping)
	iter = db.NewIterator(gethHeaderHashSuffix, nil)
	hasGethFormat := iter.Next()
	iter.Release()
	
	if hasGethFormat {
		return GethFormat
	}
	
	return UnknownFormat
}