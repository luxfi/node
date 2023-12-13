import json
import bech32

def parse_address(addr_str):
    # Split the address into chain ID and bech32 parts
    try:
        chain_id, raw_addr = addr_str.split('-', 1)
    except ValueError:
        raise ValueError("No separator found in address")

    # Decode the bech32 part
    hrp, data = bech32.bech32_decode(raw_addr)
    if hrp is None or data is None:
        raise ValueError("Invalid bech32 address")

    # Convert from 5-bit array to 8-bit array
    addr_bytes = bech32.convertbits(data, 5, 8, False)
    if addr_bytes is None:
        raise ValueError("Unable to convert address from 5-bit to 8-bit formatting")

    return chain_id, hrp, addr_bytes

def format_address(chain_id, hrp, addr_bytes):
    # Convert from 8-bit array to 5-bit array
    five_bits = bech32.convertbits(addr_bytes, 8, 5, True)
    if five_bits is None:
        raise ValueError("Unable to convert address from 8-bit to 5-bit formatting")

    # Encode the bech32 part
    addr_str = bech32.bech32_encode(hrp, five_bits)
    return f"{chain_id}-{addr_str}"

if __name__ == '__main__':
    with open('genesis/genesis_testnet.json','r') as file:
        genesis_mainnet_json = json.load(file)  # Parse the JSON content into a Python object

        # Output the updated genesis_mainnet.json content
        # print(json.dumps(genesis_mainnet_json, indent=2))

        # Simulate updating the addresses in the genesis_mainnet.json data
        # We'll need to iterate over each allocation and update the 'luxAddr' field
        for allocation in genesis_mainnet_json['allocations']:
            old_address = allocation['luxAddr'].replace('testnet1', 'fuji1')
            addr_bytes = parse_address(old_address)[2]
            new_address = format_address("X", 'testnet', addr_bytes)
            allocation['luxAddr'] = new_address
            print('old', old_address, 'new', new_address)

        with open('new_genesis_testnet.json','w') as file:
            file.write(json.dumps(genesis_mainnet_json, indent=2))