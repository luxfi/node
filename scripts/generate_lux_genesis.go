package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"time"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/formatting/address"
)

// Configuration for different networks
var networks = map[string]uint32{
	"mainnet": 1,      // MainnetID from constants
	"testnet": 5,      // FujiID/TestnetID from constants  
	"fuji":    5,      // FujiID from constants
	"local":   12345,  // LocalID from constants
}

// Genesis address that gets majority of LUX supply
const genesisETHAddr = "0xb3d82b1367d362de99ab59a658165aff520cbd4d"

// Validator ETH addresses - 11 validators total
var validatorETHAddrs = []string{
	"0x1B475A4C983DfE4f32bbA4dE8DA8fd2c37f3A2A6", // First validator
	"0xEAbCC110fAcBfebabC66Ad6f9E7B67288e720B59",
	"0x8d5081153aE1cfb41f5c932fe0b6Beb7E159cF84",
	"0xf8f12D0592e6d1bFe92ee16CaBCC4a6F26dAAe23",
	"0xFb66808f708e1d4D7D43a8c75596e84f94e06806",
	"0x313CF291c069C58D6bd61B0D672673462B8951bD",
	"0xf7f52257a6143cE6BbD12A98eF2B0a3d0C648079",
	"0xCA92ad0C91bd8DE640B9dAFfEB338ac908725142",
	"0xB5B325df519eB58B7223d85aaeac8b56aB05f3d6",
	"0xcf5288bEe8d8F63511C389D5015185FDEDe30e54",
	"0x16204223fe4470f4B1F1dA19A368dC815736a3d7",
}

// Total supply: 2 trillion LUX
// X-chain and P-chain use 9 decimals, C-chain uses 18 decimals
// Note: Go's uint64 max is 18,446,744,073,709,551,615
// 2T LUX with 9 decimals = 2,000,000,000,000,000,000,000 which overflows uint64
// We'll use max uint64 for simplicity
var totalSupplyXChain = uint64(18_446_744_073_709_551_615) // Max uint64 (about 18.4 quintillion units)
var validatorStakeAmount = uint64(1_000_000_000_000_000_000) // 1B LUX stake per validator on P-chain

// Local network development addresses (from avalanchego local stakers)
var localValidatorETHAddrs = []string{
	"0x8db97c7cece249c2b98bdc0226cc4c2a57bf52fc",
	"0x44294e59d0a7f1aae6ed405efd7a7b2ebadfd63f",
	"0x86e721b43d4ecbda96afb0e5e4b965db7e311e48",
	"0x7cf4ac414c16b9e2e04192845a2f91f089548d9f",
	"0x01f253be2ebf0bd64649fa468bf7b95ca933bde2",
}

// Validator nodes for bootstrap with their staking keys
type ValidatorInfo struct {
	NodeID    string
	PublicKey string
	PoP       string // Proof of Possession
	Weight    uint64
}

var validators = []ValidatorInfo{
	{
		NodeID:    "NodeID-Mp8JrhoLmrGznZoYsszM19W6dTdcR35NF",
		PublicKey: "0x900c9b119b5c82d781d4b49be78c3fc7ae65f2b435b7ed9e3a8b9a03e475edff86d8a64827fec8db23a6f236afbf127d",
		PoP:       "0x8bfd6d4d2086b2b8115d8f72f94095fefe5a6c07876b2accf51a811adf520f389e74a3d2152a6d90b521e2be58ffe468043dc5ea68b4c44410eb67f8dc24f13ed4f194000764c0e922cd254a3588a4962b1cb4db7de4bb9cda9d9d4d6b03f3d2",
		Weight:    1_000_000_000_000_000_000, // 1B LUX stake weight
	},
	{
		NodeID:    "NodeID-Nf5M5YoDN5CfR1wEmCPsf5zt2ojTZZj6j",
		PublicKey: "0x900c9b119b5c82d781d4b49be78c3fc7ae65f2b435b7ed9e3a8b9a03e475edff86d8a64827fec8db23a6f236afbf127d",
		PoP:       "0x8bfd6d4d2086b2b8115d8f72f94095fefe5a6c07876b2accf51a811adf520f389e74a3d2152a6d90b521e2be58ffe468043dc5ea68b4c44410eb67f8dc24f13ed4f194000764c0e922cd254a3588a4962b1cb4db7de4bb9cda9d9d4d6b03f3d2",
		Weight:    1_000_000_000_000_000_000,
	},
	{
		NodeID:    "NodeID-JCBCEeyRZdeDxEhwoztS55fsWx9SwJDVL",
		PublicKey: "0x900c9b119b5c82d781d4b49be78c3fc7ae65f2b435b7ed9e3a8b9a03e475edff86d8a64827fec8db23a6f236afbf127d",
		PoP:       "0x8bfd6d4d2086b2b8115d8f72f94095fefe5a6c07876b2accf51a811adf520f389e74a3d2152a6d90b521e2be58ffe468043dc5ea68b4c44410eb67f8dc24f13ed4f194000764c0e922cd254a3588a4962b1cb4db7de4bb9cda9d9d4d6b03f3d2",
		Weight:    1_000_000_000_000_000_000,
	},
	{
		NodeID:    "NodeID-JQvVo8DpzgyjhEDZKgqsFLVUPmN6JP3ig",
		PublicKey: "0x900c9b119b5c82d781d4b49be78c3fc7ae65f2b435b7ed9e3a8b9a03e475edff86d8a64827fec8db23a6f236afbf127d",
		PoP:       "0x8bfd6d4d2086b2b8115d8f72f94095fefe5a6c07876b2accf51a811adf520f389e74a3d2152a6d90b521e2be58ffe468043dc5ea68b4c44410eb67f8dc24f13ed4f194000764c0e922cd254a3588a4962b1cb4db7de4bb9cda9d9d4d6b03f3d2",
		Weight:    1_000_000_000_000_000_000,
	},
	{
		NodeID:    "NodeID-PKTUGFE6jnQbnskSDM3zvmQjnHKV3fxy4",
		PublicKey: "0x900c9b119b5c82d781d4b49be78c3fc7ae65f2b435b7ed9e3a8b9a03e475edff86d8a64827fec8db23a6f236afbf127d",
		PoP:       "0x8bfd6d4d2086b2b8115d8f72f94095fefe5a6c07876b2accf51a811adf520f389e74a3d2152a6d90b521e2be58ffe468043dc5ea68b4c44410eb67f8dc24f13ed4f194000764c0e922cd254a3588a4962b1cb4db7de4bb9cda9d9d4d6b03f3d2",
		Weight:    1_000_000_000_000_000_000,
	},
	{
		NodeID:    "NodeID-LtBrcgdgPW9Nj9JoU1AwGeCgi29R9JoQC",
		PublicKey: "0x900c9b119b5c82d781d4b49be78c3fc7ae65f2b435b7ed9e3a8b9a03e475edff86d8a64827fec8db23a6f236afbf127d",
		PoP:       "0x8bfd6d4d2086b2b8115d8f72f94095fefe5a6c07876b2accf51a811adf520f389e74a3d2152a6d90b521e2be58ffe468043dc5ea68b4c44410eb67f8dc24f13ed4f194000764c0e922cd254a3588a4962b1cb4db7de4bb9cda9d9d4d6b03f3d2",
		Weight:    1_000_000_000_000_000_000,
	},
	{
		NodeID:    "NodeID-962omv3YgJsqbcPvVR4yDHU8RPtaKCLt",
		PublicKey: "0x900c9b119b5c82d781d4b49be78c3fc7ae65f2b435b7ed9e3a8b9a03e475edff86d8a64827fec8db23a6f236afbf127d",
		PoP:       "0x8bfd6d4d2086b2b8115d8f72f94095fefe5a6c07876b2accf51a811adf520f389e74a3d2152a6d90b521e2be58ffe468043dc5ea68b4c44410eb67f8dc24f13ed4f194000764c0e922cd254a3588a4962b1cb4db7de4bb9cda9d9d4d6b03f3d2",
		Weight:    1_000_000_000_000_000_000,
	},
	{
		NodeID:    "NodeID-LPznW4BxjJaFYP5KEuJUenwVGTkH48XDe",
		PublicKey: "0x900c9b119b5c82d781d4b49be78c3fc7ae65f2b435b7ed9e3a8b9a03e475edff86d8a64827fec8db23a6f236afbf127d",
		PoP:       "0x8bfd6d4d2086b2b8115d8f72f94095fefe5a6c07876b2accf51a811adf520f389e74a3d2152a6d90b521e2be58ffe468043dc5ea68b4c44410eb67f8dc24f13ed4f194000764c0e922cd254a3588a4962b1cb4db7de4bb9cda9d9d4d6b03f3d2",
		Weight:    1_000_000_000_000_000_000,
	},
	{
		NodeID:    "NodeID-4nDStCMacNr5aadavMZxAxk9m9bfFf69F",
		PublicKey: "0x900c9b119b5c82d781d4b49be78c3fc7ae65f2b435b7ed9e3a8b9a03e475edff86d8a64827fec8db23a6f236afbf127d",
		PoP:       "0x8bfd6d4d2086b2b8115d8f72f94095fefe5a6c07876b2accf51a811adf520f389e74a3d2152a6d90b521e2be58ffe468043dc5ea68b4c44410eb67f8dc24f13ed4f194000764c0e922cd254a3588a4962b1cb4db7de4bb9cda9d9d4d6b03f3d2",
		Weight:    1_000_000_000_000_000_000,
	},
	{
		NodeID:    "NodeID-GGpbeWwfsZBaasex25ZPMkJFN713BXx7u",
		PublicKey: "0x900c9b119b5c82d781d4b49be78c3fc7ae65f2b435b7ed9e3a8b9a03e475edff86d8a64827fec8db23a6f236afbf127d",
		PoP:       "0x8bfd6d4d2086b2b8115d8f72f94095fefe5a6c07876b2accf51a811adf520f389e74a3d2152a6d90b521e2be58ffe468043dc5ea68b4c44410eb67f8dc24f13ed4f194000764c0e922cd254a3588a4962b1cb4db7de4bb9cda9d9d4d6b03f3d2",
		Weight:    1_000_000_000_000_000_000,
	},
	{
		NodeID:    "NodeID-Fh7dFdzt1QYQDTKJfZTVBLMyPipP99AmH",
		PublicKey: "0x900c9b119b5c82d781d4b49be78c3fc7ae65f2b435b7ed9e3a8b9a03e475edff86d8a64827fec8db23a6f236afbf127d",
		PoP:       "0x8bfd6d4d2086b2b8115d8f72f94095fefe5a6c07876b2accf51a811adf520f389e74a3d2152a6d90b521e2be58ffe468043dc5ea68b4c44410eb67f8dc24f13ed4f194000764c0e922cd254a3588a4962b1cb4db7de4bb9cda9d9d4d6b03f3d2",
		Weight:    1_000_000_000_000_000_000,
	},
}

// Create 100-year unlock schedule starting from Jan 1, 2020
// 1% unlocked per year
func createUnlockSchedule(totalAmount uint64, startDate time.Time) []LockedAmount {
	schedule := make([]LockedAmount, 100)
	annualAmount := totalAmount / 100 // 1% per year
	
	for i := 0; i < 100; i++ {
		unlockTime := startDate.AddDate(i+1, 0, 0) // Add i+1 years to start date
		schedule[i] = LockedAmount{
			Amount:   annualAmount,
			Locktime: uint64(unlockTime.Unix()),
		}
	}
	
	return schedule
}

// Convert ETH address to Lux address (X-chain or P-chain)
func ethToLuxAddress(ethAddrHex string, chain string, networkID uint32) (string, error) {
	// Remove 0x prefix if present
	ethAddrHex = strings.TrimPrefix(ethAddrHex, "0x")
	
	// Decode hex to bytes
	ethAddrBytes, err := hex.DecodeString(ethAddrHex)
	if err != nil {
		return "", err
	}
	
	// Convert to ShortID
	ethAddr, err := ids.ToShortID(ethAddrBytes)
	if err != nil {
		return "", err
	}
	
	// Determine HRP based on network ID
	var hrp string
	switch networkID {
	case 1: // MainnetID
		hrp = "lux"
	case 5: // FujiID
		hrp = "fuji"
	case 12345: // LocalID
		hrp = "local"
	default:
		hrp = "custom"
	}
	
	// Format as Lux address (X-chain or P-chain)
	luxAddr, err := address.Format(chain, hrp, ethAddr.Bytes())
	if err != nil {
		return "", err
	}
	
	return luxAddr, nil
}

type Allocation struct {
	ETHAddr        string         `json:"ethAddr"`
	LUXAddr        string         `json:"luxAddr"`
	InitialAmount  uint64         `json:"initialAmount"`
	UnlockSchedule []LockedAmount `json:"unlockSchedule"`
}

type LockedAmount struct {
	Amount   uint64 `json:"amount"`
	Locktime uint64 `json:"locktime"`
}

type Staker struct {
	NodeID         string      `json:"nodeID"`
	RewardAddress  string      `json:"rewardAddress"`
	DelegationFee  uint32      `json:"delegationFee"`
	Weight         uint64      `json:"weight,omitempty"`
	Signer         *SignerInfo `json:"signer,omitempty"`
}

type SignerInfo struct {
	PublicKey         string `json:"publicKey"`
	ProofOfPossession string `json:"proofOfPossession"`
}

type Genesis struct {
	NetworkID                  uint32       `json:"networkID"`
	Allocations                []Allocation `json:"allocations"`
	StartTime                  uint64       `json:"startTime"`
	InitialStakeDuration       uint64       `json:"initialStakeDuration"`
	InitialStakeDurationOffset uint64       `json:"initialStakeDurationOffset"`
	InitialStakedFunds         []string     `json:"initialStakedFunds"`
	InitialStakers             []Staker     `json:"initialStakers"`
	CChainGenesis              string       `json:"cChainGenesis"`
	Message                    string       `json:"message"`
}

func generateGenesis(networkName string) error {
	networkID, ok := networks[networkName]
	if !ok {
		return fmt.Errorf("unknown network: %s", networkName)
	}
	
	// Convert genesis ETH address to Lux X-chain address
	genesisLuxAddr, err := ethToLuxAddress(genesisETHAddr, "X", networkID)
	if err != nil {
		return fmt.Errorf("failed to convert genesis address: %v", err)
	}
	
	fmt.Printf("\nNetwork: %s (ID: %d)\n", networkName, networkID)
	fmt.Printf("Genesis ETH address: %s\n", genesisETHAddr)
	fmt.Printf("Genesis LUX X-chain address: %s\n", genesisLuxAddr)
	
	// Create minimal C-Chain genesis for mainnet launch
	// We'll import the actual state from the existing subnet later
	var chainID uint64
	switch networkID {
	case 1: // MainnetID
		chainID = 1990 // Lux mainnet C-Chain ID
	case 5: // FujiID/TestnetID
		chainID = 1991 // Lux testnet C-Chain ID
	case 12345: // LocalID
		chainID = 1337 // Standard Ethereum dev chain ID for compatibility
	default:
		chainID = 1993 // Default
	}
	
	// Minimal C-Chain genesis with empty allocations
	cChainGenesisObj := map[string]interface{}{
		"config": map[string]interface{}{
			"chainId":                chainID,
			"homesteadBlock":        0,
			"eip150Block":           0,
			"eip150Hash":            "0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0",
			"eip155Block":           0,
			"eip158Block":           0,
			"byzantiumBlock":        0,
			"constantinopleBlock":   0,
			"petersburgBlock":       0,
			"istanbulBlock":         0,
			"muirGlacierBlock":      0,
			"subnetEVMTimestamp":    0,
			"feeConfig": map[string]interface{}{
				"gasLimit":                  8000000,
				"minBaseFee":               25000000000,
				"targetGas":                15000000,
				"baseFeeChangeDenominator": 36,
				"minBlockGasCost":          0,
				"maxBlockGasCost":          1000000,
				"targetBlockRate":          2,
				"blockGasCostStep":         200000,
			},
			"allowFeeRecipients": false,
		},
		"alloc": make(map[string]interface{}), // Empty initial allocation
		"nonce": "0x0",
		"timestamp": "0x0",
		"extraData": "0x00",
		"gasLimit": "0x7A1200",
		"difficulty": "0x0",
		"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"coinbase": "0x0000000000000000000000000000000000000000",
		"number": "0x0",
		"gasUsed": "0x0",
		"parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
	}
	
	cChainGenesisBytes, err := json.Marshal(cChainGenesisObj)
	if err != nil {
		return fmt.Errorf("failed to marshal C-chain genesis: %v", err)
	}
	cChainGenesis := string(cChainGenesisBytes)
	
	// Choose validator addresses based on network
	validatorAddrs := validatorETHAddrs
	if networkName == "local" {
		validatorAddrs = localValidatorETHAddrs
	}
	
	// Calculate allocations
	// Reserve 1B LUX per validator for staking (11B total)
	stakingReserve := validatorStakeAmount * 11 // 11B LUX with 9 decimals
	genesisAmount := totalSupplyXChain - stakingReserve
	
	// Create allocations
	allocations := []Allocation{
		{
			ETHAddr:        genesisETHAddr,
			LUXAddr:        genesisLuxAddr,
			InitialAmount:  genesisAmount,
			UnlockSchedule: []LockedAmount{},
		},
	}
	
	// Convert validator addresses and add allocations
	validatorLuxAddrsX := make([]string, len(validatorAddrs))
	validatorLuxAddrsP := make([]string, len(validatorAddrs))
	stakedFunds := []string{} // X-chain addresses that have staked funds
	
	for i, ethAddr := range validatorAddrs {
		// Get both X-chain and P-chain addresses
		luxAddrX, err := ethToLuxAddress(ethAddr, "X", networkID)
		if err != nil {
			return fmt.Errorf("failed to convert validator X address %s: %v", ethAddr, err)
		}
		luxAddrP, err := ethToLuxAddress(ethAddr, "P", networkID)
		if err != nil {
			return fmt.Errorf("failed to convert validator P address %s: %v", ethAddr, err)
		}
		
		validatorLuxAddrsX[i] = luxAddrX
		validatorLuxAddrsP[i] = luxAddrP
		
		// Add allocation for validator on X-chain with 100-year unlock schedule
		// Create unlock schedule starting from Jan 1, 2020
		startDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		unlockSchedule := createUnlockSchedule(validatorStakeAmount, startDate)
		
		allocations = append(allocations, Allocation{
			ETHAddr:        ethAddr,
			LUXAddr:        luxAddrX,
			InitialAmount:  0, // Initial amount is 0 because funds are locked
			UnlockSchedule: unlockSchedule,
		})
		
		// Add X-chain address to staked funds (not P-chain)
		stakedFunds = append(stakedFunds, luxAddrX)
		
		fmt.Printf("Validator %d:\n", i+1)
		fmt.Printf("  ETH: %s\n", ethAddr)
		fmt.Printf("  X-chain: %s (has staked funds)\n", luxAddrX)
		fmt.Printf("  P-chain: %s (receives rewards)\n", luxAddrP)
	}
	
	// Create genesis structure
	genesis := Genesis{
		NetworkID:                  networkID,
		Allocations:                allocations,
		StartTime:                  1737388800, // Monday Jan 20, 2025 UTC (launch date)
		InitialStakeDuration:       31536000,  // 365 days
		InitialStakeDurationOffset: 5400,       // 90 minutes
		InitialStakedFunds:         stakedFunds,
		InitialStakers:             []Staker{},
		CChainGenesis:              cChainGenesis,
		Message:                    "Lux Network Genesis - Monday Launch",
	}
	
	// Add initial stakers (validators)
	numValidators := len(validatorAddrs)
	for i := 0; i < numValidators && i < len(validators); i++ {
		genesis.InitialStakers = append(genesis.InitialStakers, Staker{
			NodeID:        validators[i].NodeID,
			RewardAddress: validatorLuxAddrsP[i], // Use P-chain address for rewards
			DelegationFee: 20000, // 2%
			Weight:        validators[i].Weight,
			Signer: &SignerInfo{
				PublicKey:         validators[i].PublicKey,
				ProofOfPossession: validators[i].PoP,
			},
		})
	}
	
	// Marshal to JSON
	output, err := json.MarshalIndent(genesis, "", "\t")
	if err != nil {
		return fmt.Errorf("failed to marshal genesis: %v", err)
	}
	
	// Write to file  
	filename := fmt.Sprintf("genesis/genesis_%s.json", networkName)
	if err := ioutil.WriteFile(filename, output, 0644); err != nil {
		return fmt.Errorf("failed to write file: %v", err)
	}
	
	fmt.Printf("Generated %s\n", filename)
	fmt.Printf("Total supply: %.0f LUX\n", float64(totalSupplyXChain)/1e9)
	fmt.Printf("Genesis allocation: %.0f LUX\n", float64(genesisAmount)/1e9) 
	fmt.Printf("Validator allocations: %.0f LUX (1B each, 100-year unlock)\n", float64(stakingReserve)/1e9)
	
	return nil
}

func main() {
	for network := range networks {
		if err := generateGenesis(network); err != nil {
			log.Printf("Error generating genesis for %s: %v", network, err)
		}
	}
}