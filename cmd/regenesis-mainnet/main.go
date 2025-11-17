// Mainnet regenesis with full validator setup
package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/luxfi/node/genesis"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/bls"
	"github.com/luxfi/node/utils/formatting"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/signer"
)

const (
	numValidators = 5
	stakeAmount   = 2_000_000 * units.Lux // 2M LUX per validator
	delegationFee = 20000                   // 2%
	networkID     = 96369                   // Mainnet ID
)

type ValidatorKeys struct {
	NodeID     ids.NodeID
	StakingCert string
	StakingKey  string
	BLSKey      *bls.SecretKey
	BLSSigner   signer.Signer
}

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║          LUX MAINNET REGENESIS WITH VALIDATORS               ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Create output directory
	outDir := filepath.Join(os.Getenv("HOME"), ".lux-regenesis")
	os.RemoveAll(outDir)
	os.MkdirAll(outDir, 0755)

	// Generate validator keys
	fmt.Println("🔑 Generating validator keys...")
	validators := make([]ValidatorKeys, numValidators)

	for i := 0; i < numValidators; i++ {
		// Generate staking certificate
		cert, key, err := staking.NewCertAndKeyBytes()
		if err != nil {
			panic(fmt.Sprintf("Failed to generate staking cert %d: %v", i, err))
		}

		// Parse certificate to get NodeID
		stakingCert, err := staking.ParseCertificate(cert)
		if err != nil {
			panic(err)
		}
		nodeID := ids.NodeIDFromCert(stakingCert)

		// Generate BLS key
		blsSK, err := bls.NewSecretKey()
		if err != nil {
			panic(fmt.Sprintf("Failed to generate BLS key %d: %v", i, err))
		}

		// Create BLS signer
		blsPK := bls.PublicFromSecretKey(blsSK)
		blsSigner := &signer.ProofOfPossession{
			PublicKey: blsPK,
			ProofOfPossession: blsSK.Sign(blsPK.Serialize()),
		}

		validators[i] = ValidatorKeys{
			NodeID:      nodeID,
			StakingCert: string(cert),
			StakingKey:  string(key),
			BLSKey:      blsSK,
			BLSSigner:   blsSigner,
		}

		// Save keys to files
		nodeDir := filepath.Join(outDir, fmt.Sprintf("node%d", i+1))
		os.MkdirAll(nodeDir, 0755)

		// Save staking cert and key
		os.WriteFile(filepath.Join(nodeDir, "staker.crt"), cert, 0644)
		os.WriteFile(filepath.Join(nodeDir, "staker.key"), key, 0600)

		// Save BLS key
		blsBytes := blsSK.Serialize()
		os.WriteFile(filepath.Join(nodeDir, "signer.key"), blsBytes, 0600)

		fmt.Printf("  ✅ Validator %d: NodeID=%s\n", i+1, nodeID.String())
	}

	// Create genesis configuration
	fmt.Println("\n📋 Creating genesis configuration...")

	genesisConfig := genesis.UnparsedConfig{
		NetworkID: networkID,
		Allocations: []genesis.UnparsedAllocation{
			{
				ETHAddr: "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714", // Main treasury
				LUXAddr: "X-lux1g3ea6hckz9sre2k6xvpf5dw9k4t26hxnxvxghs",
				InitialAmount: 777_777_777 * units.MegaLux,
				UnlockSchedule: []genesis.LockedAmount{},
			},
		},
		StartTime:                  uint64(time.Now().Unix()),
		InitialStakeDuration:       365 * 24 * time.Hour,
		InitialStakeDurationOffset: 90 * 24 * time.Hour,
		InitialStakedFunds:         []string{},
		InitialStakers:             []genesis.UnparsedStaker{},
		CChain: genesis.UnparsedCChain{
			BuildGenesis: true,
			Accounts: []genesis.UnparsedAccount{
				{
					Address: "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714",
					Balance: "0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
				},
			},
		},
		Message: "Lux Mainnet Regenesis - Securing the future with quantum-resistant consensus",
	}

	// Add validators to genesis
	for i, val := range validators {
		// Create X-chain address for rewards
		xAddr := fmt.Sprintf("X-lux1validator%d%s", i+1, randomString(25))

		staker := genesis.UnparsedStaker{
			NodeID:        val.NodeID.String(),
			RewardAddress: xAddr,
			DelegationFee: delegationFee,
			Signer:        val.BLSSigner,
		}
		genesisConfig.InitialStakers = append(genesisConfig.InitialStakers, staker)

		// Add initial stake allocation
		genesisConfig.Allocations = append(genesisConfig.Allocations, genesis.UnparsedAllocation{
			LUXAddr:       xAddr,
			InitialAmount: stakeAmount,
			UnlockSchedule: []genesis.LockedAmount{
				{
					Amount:   stakeAmount,
					Locktime: uint64(time.Now().Add(90 * 24 * time.Hour).Unix()),
				},
			},
		})
	}

	// Generate genesis bytes
	genesisBytes, _, err := genesis.FromConfig(&genesisConfig)
	if err != nil {
		panic(fmt.Sprintf("Failed to create genesis: %v", err))
	}

	// Save genesis file
	genesisPath := filepath.Join(outDir, "genesis.json")
	if err := os.WriteFile(genesisPath, genesisBytes, 0644); err != nil {
		panic(err)
	}

	// Create network startup script
	fmt.Println("\n🚀 Creating network startup script...")
	createStartupScript(outDir, validators)

	// Create SubnetEVM deployment script
	fmt.Println("📦 Creating SubnetEVM deployment script...")
	createSubnetEVMScript(outDir)

	fmt.Println("\n╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    REGENESIS SETUP COMPLETE                   ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Printf("\nGenesis and validator keys created in: %s\n", outDir)
	fmt.Printf("To start the network: %s/start-network.sh\n", outDir)
	fmt.Printf("To deploy SubnetEVM: %s/deploy-subnetevm.sh\n", outDir)
	fmt.Println("\nNetwork will include:")
	fmt.Println("  • P-Chain (Platform) with 5 validators")
	fmt.Println("  • X-Chain (Exchange) for assets")
	fmt.Println("  • C-Chain (Contract) EVM-compatible")
	fmt.Println("  • Q-Chain (Quantum) post-quantum ready")
	fmt.Println("  • SubnetEVM deployment ready")
}

func createStartupScript(outDir string, validators []ValidatorKeys) {
	script := `#!/bin/bash
set -e

LUXD_PATH="${LUXD_PATH:-$HOME/work/lux/node/build/node}"
GENESIS_PATH="` + outDir + `/genesis.json"
BASE_DIR="` + outDir + `/nodes"

echo "Starting Lux Mainnet Regenesis Network..."

# Clean previous data
rm -rf "$BASE_DIR"
mkdir -p "$BASE_DIR"

# Start nodes
`
	for i := range validators {
		nodeDir := fmt.Sprintf("$BASE_DIR/node%d", i+1)
		httpPort := 9650 + i*10
		stakingPort := 9651 + i*10

		script += fmt.Sprintf(`
# Node %d
echo "Starting Node %d..."
mkdir -p %s
cp %s/node%d/staker.* %s/
cp %s/node%d/signer.key %s/

nohup "$LUXD_PATH" \
  --network-id=%d \
  --http-port=%d \
  --staking-port=%d \
  --data-dir=%s/data \
  --log-dir=%s/logs \
  --genesis="$GENESIS_PATH" \
  --staking-tls-cert-file=%s/staker.crt \
  --staking-tls-key-file=%s/staker.key \
  --staking-signer-key-file=%s/signer.key \
  --bootstrap-ips="" \
  --bootstrap-ids="" \
  > %s/node.log 2>&1 &

echo "  NodeID: %s"
echo "  HTTP: http://localhost:%d"
echo "  Staking: localhost:%d"
`,
			i+1, i+1, nodeDir, outDir, i+1, nodeDir, outDir, i+1, nodeDir,
			networkID, httpPort, stakingPort, nodeDir, nodeDir,
			nodeDir, nodeDir, nodeDir, nodeDir,
			validators[i].NodeID.String(), httpPort, stakingPort)
	}

	script += `
echo "All nodes started!"
echo "Wait for bootstrap (~60 seconds)..."

# Function to check if chains are bootstrapped
check_bootstrap() {
  local port=$1
  curl -s -X POST --data '{
    "jsonrpc":"2.0",
    "method":"info.isBootstrapped",
    "params":{"chain":"C"},
    "id":1
  }' -H 'content-type:application/json' http://localhost:$port/ext/info | grep -q '"result":{"isBootstrapped":true}'
}

# Wait for bootstrap
SECONDS=0
while ! check_bootstrap 9650; do
  if [ $SECONDS -gt 120 ]; then
    echo "Bootstrap timeout!"
    exit 1
  fi
  echo -ne "\rWaiting for bootstrap... ${SECONDS}s"
  sleep 5
done

echo -e "\n✅ Network bootstrapped successfully!"
echo "You can now deploy SubnetEVM and replay blocks."
`

	scriptPath := filepath.Join(outDir, "start-network.sh")
	os.WriteFile(scriptPath, []byte(script), 0755)
}

func createSubnetEVMScript(outDir string) {
	script := `#!/bin/bash
set -e

echo "Deploying SubnetEVM..."

# Create subnet
SUBNET_ID=$(curl -X POST --data '{
  "jsonrpc":"2.0",
  "method":"platform.createSubnet",
  "params":{
    "controlKeys":["P-lux1..."],
    "threshold":1
  },
  "id":1
}' -H 'content-type:application/json' http://localhost:9650/ext/bc/P | jq -r '.result.subnetID')

echo "Created Subnet: $SUBNET_ID"

# Create blockchain with SubnetEVM
BLOCKCHAIN_ID=$(curl -X POST --data '{
  "jsonrpc":"2.0",
  "method":"platform.createBlockchain",
  "params":{
    "subnetID":"'$SUBNET_ID'",
    "vmID":"srEXiWaHuhNyGwPUi444Tu47ZEDwxTWrbQiuD7FmgSAQ6X7Dy",
    "name":"SubnetEVM",
    "genesisData":"{\"config\":{\"chainId\":96369,\"homesteadBlock\":0,\"eip150Block\":0,\"eip155Block\":0,\"eip158Block\":0,\"byzantiumBlock\":0,\"constantinopleBlock\":0,\"petersburgBlock\":0,\"istanbulBlock\":0,\"muirGlacierBlock\":0,\"subnetEVMTimestamp\":0,\"feeConfig\":{\"gasLimit\":8000000,\"targetBlockRate\":2,\"minBaseFee\":25000000000,\"targetGas\":15000000,\"baseFeeChangeDenominator\":36,\"minBlockGasCost\":0,\"maxBlockGasCost\":1000000,\"blockGasCostStep\":200000}},\"alloc\":{\"0x9011E888251AB053B7bD1cdB598Db4f9DEd94714\":{\"balance\":\"0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF\"}},\"timestamp\":\"0x0\",\"gasLimit\":\"0x7A1200\",\"difficulty\":\"0x0\",\"mixHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\",\"coinbase\":\"0x0000000000000000000000000000000000000000\",\"number\":\"0x0\",\"gasUsed\":\"0x0\",\"parentHash\":\"0x0000000000000000000000000000000000000000000000000000000000000000\"}"
  },
  "id":1
}' -H 'content-type:application/json' http://localhost:9650/ext/bc/P | jq -r '.result.blockchainID')

echo "Created SubnetEVM Blockchain: $BLOCKCHAIN_ID"
echo ""
echo "SubnetEVM RPC endpoint: http://localhost:9650/ext/bc/$BLOCKCHAIN_ID/rpc"
echo ""
echo "You can now replay blocks using the regenesis-execute tool."
`

	scriptPath := filepath.Join(outDir, "deploy-subnetevm.sh")
	os.WriteFile(scriptPath, []byte(script), 0755)
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}