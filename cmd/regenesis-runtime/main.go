// Runtime regenesis tool for replaying SubnetEVM to C-Chain
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/luxfi/node/genesis"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/units"
)

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║          LUX RUNTIME REGENESIS REPLAY TOOL                   ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Create output directory
	outDir := filepath.Join(os.Getenv("HOME"), ".lux-runtime-replay")
	os.RemoveAll(outDir)
	os.MkdirAll(outDir, 0755)

	fmt.Println("📋 Creating runtime replay configuration...")
	fmt.Println()
	fmt.Println("Configuration for runtime replay:")
	fmt.Println("  • Source: SubnetEVM with 1,074,616 blocks")
	fmt.Println("  • Target: C-Chain (fresh state)")
	fmt.Println("  • Treasury: 61.5 billion LUX to be replayed")
	fmt.Println()

	// Create basic genesis for C-Chain
	// This will be used as the starting point for replay
	genesisBytes, _, err := genesis.FromConfig(&genesis.Config{
		NetworkID: constants.MainnetID,
		Allocations: []genesis.Allocation{
			{
				InitialAmount: 61_500_000_000 * units.Lux, // 61.5 billion LUX
				ETHAddr:       "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714",
			},
		},
		StartTime:                  genesis.MainnetParams.StartTime,
		InitialStakeDuration:       genesis.MainnetParams.InitialStakeDuration,
		InitialStakeDurationOffset: genesis.MainnetParams.InitialStakeDurationOffset,
		InitialStakedFunds:         []ids.ShortID{},
		InitialStakers:             []genesis.Staker{},
		CChainGenesis:              "{\"config\":{\"chainId\":96369}}",
		Message:                    "Lux Runtime Regenesis Replay",
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to create genesis: %v", err))
	}

	// Save genesis file
	genesisPath := filepath.Join(outDir, "genesis.json")
	if err := os.WriteFile(genesisPath, genesisBytes, 0644); err != nil {
		panic(fmt.Sprintf("Failed to write genesis: %v", err))
	}

	fmt.Println("✅ Runtime replay configuration created!")
	fmt.Println()
	fmt.Println("📁 Output directory:", outDir)
	fmt.Println("   • genesis.json - Starting genesis for C-Chain")
	fmt.Println()
	fmt.Println("🚀 Next steps:")
	fmt.Println("   1. Deploy multi-node network using lux-cli")
	fmt.Println("   2. Configure SubnetEVM with existing database")
	fmt.Println("   3. Start C-Chain with this genesis")
	fmt.Println("   4. Begin runtime replay from SubnetEVM to C-Chain")
	fmt.Println("   5. Verify C-Chain shows 1,074,616 blocks after replay")
	fmt.Println("   6. Confirm treasury balance of ~61.5 billion LUX")
	fmt.Println()
	fmt.Println("📖 For detailed instructions, see:")
	fmt.Println("   https://docs.lux.network/runtime-replay")
}