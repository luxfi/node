package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/luxfi/address"
)

func main() {
	// The 20-byte address from P-lux1c7wevm4667l4umtzh93r25wpxlpsadkhka6gv6
	// This is derived from the validator's P-chain address
	addrBytes, _ := hex.DecodeString("c7a6666dd7579fd79b165e5114a8a0df0c1d6db5")

	// Format with zoo HRP
	zooAddr, err := address.Format("P", "zoo", addrBytes)
	if err != nil {
		fmt.Printf("Error formatting address: %v\n", err)
		return
	}
	fmt.Printf("Zoo P-chain address: %s\n", zooAddr)

	// Generate genesis
	genesis := map[string]interface{}{
		"networkID": 200200,
		"allocations": []map[string]interface{}{
			{
				"evmAddr":       "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714",
				"utxoAddr":       zooAddr,
				"initialAmount": 0,
				"unlockSchedule": []map[string]interface{}{
					{
						"amount":   100000000000000000,
						"locktime": 0,
					},
				},
			},
		},
		"startTime":                  time.Now().Unix(),
		"initialStakeDuration":       31536000,
		"initialStakeDurationOffset": 5400,
		"initialStakedFunds":         []string{zooAddr},
		"message":                    "Zoo Mainnet",
	}

	// Load validators from Lux genesis
	luxGenesis, _ := os.ReadFile(os.ExpandEnv("$HOME/work/lux/mainnet/genesis_mainnet.json"))
	var lux map[string]interface{}
	json.Unmarshal(luxGenesis, &lux)

	// Copy stakers but change reward address
	stakers := lux["initialStakers"].([]interface{})
	zooStakers := make([]interface{}, len(stakers))
	for i, s := range stakers {
		staker := s.(map[string]interface{})
		newStaker := make(map[string]interface{})
		for k, v := range staker {
			newStaker[k] = v
		}
		newStaker["rewardAddress"] = zooAddr
		zooStakers[i] = newStaker
	}
	genesis["initialStakers"] = zooStakers

	// cChainGenesis
	cchain := lux["cChainGenesis"].(string)
	var cc map[string]interface{}
	json.Unmarshal([]byte(cchain), &cc)
	cc["config"].(map[string]interface{})["chainId"] = 200200
	delete(cc, "genesisHash")
	delete(cc, "stateRoot")
	ccBytes, _ := json.Marshal(cc)
	genesis["cChainGenesis"] = string(ccBytes)

	out, _ := json.MarshalIndent(genesis, "", "  ")
	os.WriteFile(os.ExpandEnv("$HOME/work/lux/mainnet/zoo_genesis_valid.json"), out, 0644)
	fmt.Println("Wrote zoo_genesis_valid.json")
}
