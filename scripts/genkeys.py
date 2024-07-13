#!/usr/bin/env python3
import os
import json
import subprocess
import bech32
from bip_utils import Bip39MnemonicGenerator, Bip39SeedGenerator, Bip44, Bip44Coins, Bip44Changes, Bip44Levels
from eth_utils import keccak, to_checksum_address

def parse_address(addr_str):
    # Split the address into chain ID and bech32 parts
    try:
        chain_id, raw_addr = addr_str.split('-', 1)
    except ValueError:
        raise ValueError('No separator found in address')

    # Decode the bech32 part
    hrp, data = bech32.bech32_decode(raw_addr)
    if hrp is None or data is None:
        raise ValueError('Invalid bech32 address')

    # Convert from 5-bit array to 8-bit array
    addr_bytes = bech32.convertbits(data, 5, 8, False)
    if addr_bytes is None:
        raise ValueError('Unable to convert address from 5-bit to 8-bit formatting')

    return chain_id, hrp, addr_bytes

def format_address(chain_id, hrp, addr_bytes):
    # Convert from 8-bit array to 5-bit array
    five_bits = bech32.convertbits(addr_bytes, 8, 5, True)
    if five_bits is None:
        raise ValueError('Unable to convert address from 8-bit to 5-bit formatting')

    # Encode the bech32 part
    addr_str = bech32.bech32_encode(hrp, five_bits)
    return f'{chain_id}-{addr_str}'

def genkey(addr_num=10, chain='lux'):
    # Generate a 24-word seed phrase
    mnemonic = Bip39MnemonicGenerator().FromWordsNumber(24)
    seed = Bip39SeedGenerator(mnemonic).Generate()

    # Convert mnemonic to string for JSON serialization
    mnemonic_str = str(mnemonic)

    # Create Lux X-chain wallet
    lux_wallet = Bip44.FromSeed(seed, Bip44Coins.LUX_X_CHAIN).DeriveDefaultPath()
    lux_private_key = lux_wallet.PrivateKey().Raw().ToHex()
    lux_address = lux_wallet.PublicKey().ToAddress()

    # Create Lux address
    addr_bytes = parse_address(lux_address)[2]
    lux_address = format_address('X', chain, addr_bytes)

    # Ethereum key generation
    eth_wallets = []
    bip44_mst_ctx = Bip44.FromSeed(seed, Bip44Coins.ETHEREUM)
    bip44_acc_ctx = bip44_mst_ctx.Purpose().Coin().Account(0).Change(Bip44Changes.CHAIN_EXT)
    for i in range(addr_num):
        bip44_addr_ctx = bip44_acc_ctx.AddressIndex(i)
        eth_address = bip44_addr_ctx.PublicKey().ToAddress()
        eth_public_key = bip44_addr_ctx.PublicKey().RawCompressed().ToHex()
        eth_private_key = bip44_addr_ctx.PrivateKey().Raw().ToHex()
        eth_wallets.append({
            'address': eth_address,
            'public_key': eth_public_key,
            'private_key': eth_private_key
        })

    return {
        'mnemonic': mnemonic_str,
        'lux_private_key': lux_private_key,
        'lux_address': lux_address,
        'lux_address': lux_address,
        'ethereum_wallets': eth_wallets
    }

def genkey_stdout(addr_num=10, chain='lux'):
    # Step 1: Generate a 24-word seed phrase
    mnemonic = Bip39MnemonicGenerator().FromWordsNumber(24)
    print(f'Mnemonic (Seed Phrase):\n{mnemonic}')

    # Step 2: Convert the mnemonic to a seed
    seed = Bip39SeedGenerator(mnemonic).Generate()

    # Step 3: Create Lux X-chain wallet
    # Assuming a common derivation path for Lux X-Chain (this needs to be verified)
    lux_wallet = Bip44.FromSeed(seed, Bip44Coins.LUX_X_CHAIN).DeriveDefaultPath()
    print(f'\nX-Chain Private Key:\n{lux_wallet.PrivateKey().Raw().ToHex()}')
    print(f'\nLux X-Chain Address:\n{lux_wallet.PublicKey().ToAddress()}')

    # Step 4: Create Lux address
    address = lux_wallet.PublicKey().ToAddress()
    addr_bytes = parse_address(address)[2]
    new_address = format_address('X', chain, addr_bytes)
    print(f'\nLux X-Chain Address:\n{new_address}')

    bip44_mst_ctx = Bip44.FromSeed(seed, Bip44Coins.ETHEREUM)
    print(f'\nMaster key (bytes):\n{bip44_mst_ctx.PrivateKey().Raw().ToHex()}')
    # Derive the account: m/44'/60'/0'
    bip44_acc_ctx = bip44_mst_ctx.Purpose().Coin().Account(0).Change(Bip44Changes.CHAIN_EXT)

    # Derive addresses: m/44'/60'/0'/0/i
    print('\nAccounts:')
    for i in range(addr_num):
        bip44_addr_ctx = bip44_acc_ctx.AddressIndex(i)
        print(f'  {i} address: {bip44_addr_ctx.PublicKey().ToAddress()}')
        print(f'  {i} public key (bytes): {bip44_addr_ctx.PublicKey().RawCompressed().ToHex()}')
        print(f'  {i} private key (bytes): {bip44_addr_ctx.PrivateKey().Raw().ToHex()}\n')

def generate_keys_and_certificates(staking_path='~/.luxd/staking', num=1, cert_days=36525):
    # Set the directory path for luxd staking keys
    staking_dir = os.path.expanduser(staking_path)
    os.makedirs(staking_dir, exist_ok=True)

    for i in range(num):
        print(f'Generating keys and certificate for execution {i}')

        # Generate the keys
        key_data = genkey()

        # Define file names based on iteration
        if i == 0:
            key_filename  = 'staker.key'
            crt_filename  = 'staker.crt'
            json_filename = 'staker.json'
        else:
            key_filename  = f'staker-{i}.key'
            crt_filename  = f'staker-{i}.crt'
            json_filename = f'staker-{i}.json'

        # Write the keys to a JSON file for reference
        with open(f'{staking_dir}/{json_filename}', 'w') as json_file:
            json.dump(key_data, json_file)

        # Generate certificate and key using openssl
        openssl_cmd_key = [
            'openssl', 'req', '-x509', '-newkey', 'rsa:4096',
            '-keyout', f'{staking_dir}/{key_filename}',
            '-out', f'{staking_dir}/{crt_filename}',
            '-days', str(cert_days), '-nodes',
            '-subj', '/CN=lux.partners', '-set_serial', str(i)
        ]
        subprocess.run(openssl_cmd_key)

        print(f'Saved keys to {staking_dir}/{json_filename}, certificate to {staking_dir}/{crt_filename}, and private key to {staking_dir}/{key_filename}')

if __name__ == '__main__':
    generate_keys_and_certificates(staking_path='./stakingkeys', num=1)
