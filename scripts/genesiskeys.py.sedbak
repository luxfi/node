#!/usr/bin/env python3

import json
import os

mainnet_genesis_path = os.path.join(os.path.dirname(__file__), "../genesis/genesis_mainnet.json")

import json
import os

def read_json(file_path):
    with open(file_path, 'r') as file:
        return json.load(file)

def update_genesis_file(genesis_file_path, staking_dir, reward_address='X-lux1fg9dlhdtshtnxnhdtpwayvsyx2r8y8fujcvxd0'):
    genesis_data = read_json(genesis_file_path)

    # Reset genesis data
    genesis_data['allocations']        = []
    genesis_data['initialStakedFunds'] = []
    genesis_data['initialStakers']     = []

    # Loop over staker-1.json to staker-99.json
    for i in range(0, 100):
        if i == 0:
            staker_json = os.path.join(staking_dir, f"staker.json")
        else:
            staker_json = os.path.join(staking_dir, f"staker-{i}.json")

        if os.path.exists(staker_json):
            staker_data = read_json(staker_json)

        lux_address = staker_data['lux_address']
        eth_address = staker_data['ethereum_wallets'][0]['address']  # This is an example, adjust according to your structure
        node_id     = staker_data['id']

        # Update the genesis data
        # This is an example update. Adjust the logic according to your needs.
        genesis_data['allocations'].append({
            "ethAddr": eth_address,
            "luxAddr": lux_address,
            "initialAmount": 10000000000000000,
            "unlockSchedule": [{"amount": 10000000000000000, "locktime": 4733510400}]
        })
        genesis_data['initialStakedFunds'].append(lux_address)
        genesis_data['initialStakers'].append({
            "nodeID": node_id,
            "rewardAddress": reward_address,
            "delegationFee": 80000,
        })

        # Write the updated data back to genesis_mainnet.json
        with open(genesis_file_path, 'w') as file:
            json.dump(genesis_data, file, indent=4)

if __name__ == "__main__":
    staking_dir = os.path.expanduser('~/.luxd/staking')
    update_genesis_file(mainnet_genesis_path, staking_dir)
