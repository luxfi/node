#!/usr/bin/env python3
import json
import os

bootstrappers_path = os.path.join(os.path.dirname(__file__), "../genesis/bootstrappers.json")

import json
import os

def read_json(file_path):
    with open(file_path, 'r') as file:
        return json.load(file)

def update_bootstrappers(bootstrappers_path, staking_dir, reward_address='X-lux1fg9dlhdtshtnxnhdtpwayvsyx2r8y8fujcvxd0'):
    bootstrappers_data = read_json(bootstrappers_path)

    # Reset genesis data
    bootstrappers_data['mainnet'] = []
    bootstrappers_data['testnet'] = []

    # Loop over staker-1.json to staker-99.json
    for i in range(0, 100):
        if i == 0:
            staker_json = os.path.join(staking_dir, f"staker.json")
        else:
            staker_json = os.path.join(staking_dir, f"staker-{i}.json")

        if os.path.exists(staker_json):
            staker_data = read_json(staker_json)

        node_id = staker_data['id']
        node_ip = f'127.0.0.1:{9650+(i*2)}'

        # Update the genesis data
        # This is an example update. Adjust the logic according to your needs.
        bootstrappers_data['mainnet'].append({
            "id": node_id,
            "ip": node_ip,
        })
        bootstrappers_data['testnet'].append({
            "id": node_id,
            "ip": node_ip,
        })

        # Write the updated data back to genesis_mainnet.json
        with open(bootstrappers_path, 'w') as file:
            json.dump(bootstrappers_data, file, indent=4)

if __name__ == "__main__":
    staking_dir = os.path.expanduser('~/.luxd/staking')
    update_bootstrappers(bootstrappers_path, staking_dir)
