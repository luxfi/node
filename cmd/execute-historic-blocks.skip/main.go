// Execute all historic blocks to rebuild state
package main

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/consensus/clique"
	"github.com/luxfi/geth/core"
	"github.com/luxfi/geth/core/rawdb"
	"github.com/luxfi/geth/core/state"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/geth/core/vm"
	"github.com/luxfi/geth/ethdb/badgerdb"
	"github.com/luxfi/geth/ethdb/pebble"
	"github.com/luxfi/geth/params"
	"github.com/luxfi/geth/rlp"
	"github.com/luxfi/geth/trie"
)

func main() {
	home, _ := os.UserHomeDir()
	source := home + "/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"
	target := home + "/.lux/db/C-executed"

	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║        EXECUTE HISTORIC BLOCKS - REBUILD STATE               ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("Source: %s\n", source)
	fmt.Printf("Target: %s\n\n", target)

	// Open source (old blocks)
	srcDB, _ := pebble.New(source, 0, 0, "", true)
	defer srcDB.Close()

	// Create fresh target
	os.RemoveAll(target)
	os.MkdirAll(target, 0755)
	tgtDB, _ := badgerdb.New(target, 0, 0, "", false)
	defer tgtDB.Close()

	// Create genesis state
	genesis := createGenesis()
	genesisBlock := genesis.MustCommit(tgtDB, nil)

	fmt.Printf("Genesis block: %s\n", genesisBlock.Hash().Hex())
	fmt.Printf("Genesis root: %s\n\n", genesisBlock.Root().Hex())

	// Create blockchain
	chainConfig := &params.ChainConfig{
		ChainID:             big.NewInt(96369),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
	}

	engine := clique.New(chainConfig.Clique, tgtDB)
	triedb := trie.NewDatabase(tgtDB, nil)

	chain, err := core.NewBlockChain(tgtDB, nil, genesis, nil, engine, vm.Config{}, nil, nil)
	if err != nil {
		panic(err)
	}
	defer chain.Stop()

	fmt.Println("Executing blocks...")
	start := time.Now()

	namespace := common.FromHex("337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1")
	nsLen := len(namespace)

	for blockNum := uint64(1); blockNum <= 1082474; blockNum++ {
		// Read block from source
		block, err := readBlock(srcDB, namespace, nsLen, blockNum)
		if err != nil {
			if blockNum%10000 == 0 {
				fmt.Printf("Block %d not found, stopping\n", blockNum)
			}
			break
		}

		// Execute block
		_, _, err = chain.InsertChain(types.Blocks{block})
		if err != nil {
			fmt.Printf("Error at block %d: %v\n", blockNum, err)
			break
		}

		if blockNum%10000 == 0 {
			elapsed := time.Since(start)
			bps := float64(blockNum) / elapsed.Seconds()
			fmt.Printf("Block %d (%.0f blocks/sec)\n", blockNum, bps)
		}
	}

	// Query final balance
	stateDB, _ := chain.StateAt(chain.CurrentBlock().Root)
	addr := common.HexToAddress("0x9011E888251AB053B7bD1cdB598Db4f9DEd94714")
	balance := stateDB.GetBalance(addr)

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                  EXECUTION COMPLETE                          ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("Final block: %d\n", chain.CurrentBlock().Number.Uint64())
	fmt.Printf("Address: %s\n", addr.Hex())
	fmt.Printf("Balance: %s wei\n", balance.ToBig().String())

	balanceLUX := new(big.Float).SetInt(balance.ToBig())
	balanceLUX.Quo(balanceLUX, big.NewFloat(1e18))
	fmt.Printf("Balance: %s LUX\n", balanceLUX.Text('f', 2))

	balanceT := new(big.Float).SetInt(balance.ToBig())
	balanceT.Quo(balanceT, big.NewFloat(1e30))
	fmt.Printf("Balance: %s trillion LUX\n", balanceT.Text('f', 2))
}

func createGenesis() *core.Genesis {
	return &core.Genesis{
		Config: &params.ChainConfig{
			ChainID:             big.NewInt(96369),
			HomesteadBlock:      big.NewInt(0),
			EIP150Block:         big.NewInt(0),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
			BerlinBlock:         big.NewInt(0),
			LondonBlock:         big.NewInt(0),
		},
		Alloc: core.GenesisAlloc{
			common.HexToAddress("0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"): {
				Balance: new(big.Int).SetBytes(common.FromHex("0x33b2e3c9fd0803ce8000000")), // 1B
			},
		},
		GasLimit: 8000000,
	}
}

func readBlock(db *pebble.Database, namespace []byte, nsLen int, num uint64) (*types.Block, error) {
	// Read canonical hash
	canonKey := append(namespace, canonicalKey(num)...)
	hashBytes, err := db.Get(canonKey)
	if err != nil {
		return nil, err
	}

	blockHash := common.BytesToHash(hashBytes)

	// Read header (old format - 17 fields, not 21)
	headerKey := append(namespace, makeHeaderKey(num, blockHash)...)
	headerData, err := db.Get(headerKey)
	if err != nil {
		return nil, err
	}

	// Try decode as new format first
	var header types.Header
	err = rlp.DecodeBytes(headerData, &header)
	if err != nil {
		// Failed - try legacy format (17 fields)
		var legacy struct {
			ParentHash  common.Hash
			UncleHash   common.Hash
			Coinbase    common.Address
			Root        common.Hash
			TxHash      common.Hash
			ReceiptHash common.Hash
			Bloom       types.Bloom
			Difficulty  *big.Int
			Number      *big.Int
			GasLimit    uint64
			GasUsed     uint64
			Time        uint64
			Extra       []byte
			MixDigest   common.Hash
			Nonce       types.BlockNonce
			BaseFee     *big.Int
			ExtDataHash common.Hash
		}

		if err := rlp.DecodeBytes(headerData, &legacy); err != nil {
			return nil, fmt.Errorf("decode error: %w", err)
		}

		// Convert to new format
		header = types.Header{
			ParentHash:  legacy.ParentHash,
			UncleHash:   legacy.UncleHash,
			Coinbase:    legacy.Coinbase,
			Root:        legacy.Root,
			TxHash:      legacy.TxHash,
			ReceiptHash: legacy.ReceiptHash,
			Bloom:       legacy.Bloom,
			Difficulty:  legacy.Difficulty,
			Number:      legacy.Number,
			GasLimit:    legacy.GasLimit,
			GasUsed:     legacy.GasUsed,
			Time:        legacy.Time,
			Extra:       legacy.Extra,
			MixDigest:   legacy.MixDigest,
			Nonce:       legacy.Nonce,
			BaseFee:     legacy.BaseFee,
			// Cancun fields default to nil/zero
		}
	}

	// Read body
	bodyKey := append(namespace, makeBodyKey(num, blockHash)...)
	bodyData, err := db.Get(bodyKey)
	if err != nil {
		// No body = empty block
		return types.NewBlockWithHeader(&header), nil
	}

	var body types.Body
	if err := rlp.DecodeBytes(bodyData, &body); err != nil {
		return types.NewBlockWithHeader(&header), nil
	}

	return types.NewBlockWithHeader(&header).WithBody(body.Transactions, body.Uncles), nil
}

func canonicalKey(num uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, num)
	return append(append([]byte("h"), b...), 'n')
}

func makeHeaderKey(num uint64, hash common.Hash) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, num)
	return append(append([]byte("h"), b...), hash.Bytes()...)
}

func makeBodyKey(num uint64, hash common.Hash) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, num)
	return append(append([]byte("b"), b...), hash.Bytes()...)
}
