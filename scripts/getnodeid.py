#!/usr/bin/env python3
import argparse
import os
import re
import json
import subprocess

default_luxd_path = os.path.join(os.path.dirname(__file__), "../build/luxd")

def run_luxd(luxd_path, staker_key, staker_cert):
    # Construct the command to run luxd
    cmd = [
        luxd_path,
        "--staking-tls-key-file", staker_key,
        "--staking-tls-cert-file", staker_cert,
    ]

    # Start luxd and capture its output
    process = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)

    # Regex pattern to match NodeID
    pattern = r'"nodeID": "NodeID-([A-Za-z0-9]+)"'

    try:
        while True:
            line = process.stdout.readline()
            if not line:
                break
            match = re.search(pattern, line)
            if match:
                node_id = match.group(1)
                print(f"NodeID: NodeID-{node_id}")
                break
    except KeyboardInterrupt:
        # Handle Ctrl+C if user wants to stop the script
        pass
    finally:
        process.terminate()
    return f"NodeID-{node_id}"

def update_staker_json():
    def update_json(staker_json, node_id):
        with open(staker_json, 'r+') as file:
            data = json.load(file)
            data['id'] = node_id
            file.seek(0)
            json.dump(data, file, indent=4)
            file.truncate()

    staking_dir = os.path.expanduser('~/.luxd/staking')

    for i in range(1, 100):
        staker_key  = os.path.join(staking_dir, f"staker-{i}.key")
        staker_cert = os.path.join(staking_dir, f"staker-{i}.crt")
        staker_json = os.path.join(staking_dir, f"staker-{i}.json")

        if os.path.exists(staker_key) and os.path.exists(staker_cert) and os.path.exists(staker_json):
            node_id = run_luxd(luxd_path, staker_key, staker_cert)
            if node_id:
                update_json(staker_json, node_id)
                print(f"Updated {staker_json} with NodeID {node_id}")
            else:
                print(f"Failed to get NodeID for {staker_key} and {staker_cert}")
        else:
            print(f"Missing file for set {i}: {staker_key}, {staker_cert}, or {staker_json}")

if __name__ == "__main__":
    # Setup command line argument parsing
    parser = argparse.ArgumentParser(description='Run avalanchego with staking key and certificate.')

    default_key  = "~/.luxd/staking/staker.key"
    default_cert = "~/.luxd/staking/staker.crt"

    parser.add_argument('--staker-key', type=str, default=default_key, help='Path to the staking key file')
    parser.add_argument('--staker-cert', type=str, default=default_cert, help='Path to the staking certificate file')
    parser.add_argument('--luxd-path', type=str, default=default_luxd_path, help='Path to the luxd executable')

    # Parse arguments
    args = parser.parse_args()

    # Paths to the luxd executable and staking files
    staker_key  = os.path.expanduser(args.staker_key)
    staker_cert = os.path.expanduser(args.staker_cert)
    luxd_path    = os.path.expanduser(args.luxd_path)

    # Check if the provided files exist
    if not os.path.exists(staker_key):
        print(f"Staking key file not found: {args.staker_key}")
        exit(1)

    if not os.path.exists(staker_cert):
        print(f"Staking certificate file not found: {args.staker_cert}")
        exit(1)

    # Start luxd and get the Node ID
    run_luxd(luxd_path, staker_key, staker_cert)
