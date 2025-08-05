#!/usr/bin/env python3

import json
import sys
import time

def create_single_node_genesis(node_id, bls_public_key, bls_proof):
    """Create a genesis.json for single-node mainnet with proper BLS validation"""
    
    # Base mainnet configuration
    genesis = {
        "networkID": 96369,
        "allocations": [
            {
                "ethAddr": "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC",
                "luxAddr": "X-lux18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u",
                "initialAmount": 500000000000000000,
                "unlockSchedule": [
                    {
                        "amount": 500000000000000000,
                        "locktime": 0
                    }
                ]
            }
        ],
        "startTime": int(time.time()),
        "initialStakeDuration": 31536000,  # 365 days
        "initialStakeDurationOffset": 5400,  # 90 minutes
        "initialStakedFunds": [
            "X-lux18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u"
        ],
        "initialStakers": [
            {
                "nodeID": node_id,
                "rewardAddress": "X-lux18jma8ppw3nhx5r4ap8clazz0dps7rv5u00z96u",
                "delegationFee": 20000,
                "signer": {
                    "publicKey": bls_public_key,
                    "proofOfPossession": bls_proof
                }
            }
        ],
        "cChainGenesis": json.dumps({
            "config": {
                "chainId": 96369,
                "homesteadBlock": 0,
                "eip150Block": 0,
                "eip155Block": 0,
                "eip158Block": 0,
                "byzantiumBlock": 0,
                "constantinopleBlock": 0,
                "petersburgBlock": 0,
                "istanbulBlock": 0,
                "muirGlacierBlock": 0,
                "berlinBlock": 0,
                "londonBlock": 0
            },
            "nonce": "0x0",
            "timestamp": "0x0",
            "extraData": "0x00",
            "gasLimit": "0x5f5e100",
            "difficulty": "0x0",
            "mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
            "coinbase": "0x0000000000000000000000000000000000000000",
            "alloc": {
                "0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC": {
                    "balance": "0x21e19e0c9bab2400000"  # 10000 ETH
                }
            }
        }),
        "message": "Single node mainnet with BLS validation"
    }
    
    return genesis

if __name__ == "__main__":
    if len(sys.argv) != 4:
        print("Usage: create-single-node-genesis.py <node_id> <bls_public_key> <bls_proof>")
        sys.exit(1)
    
    node_id = sys.argv[1]
    bls_public_key = sys.argv[2]
    bls_proof = sys.argv[3]
    
    genesis = create_single_node_genesis(node_id, bls_public_key, bls_proof)
    
    print(json.dumps(genesis, indent=2))