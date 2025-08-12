# LUX Bootstrap Solution

## Problem Solved
The LUX mainnet node with 1,082,781 pre-loaded blocks wasn't bootstrapping properly with a single validator due to Avalanche consensus requiring peer communication.

## Key Findings

### 1. Bootstrap Issue
- **Root Cause**: A single node cannot complete bootstrap by talking to itself in Snowman/Avalanche consensus
- **Why**: Bootstrap phase completes when the node receives an accepted-frontier from a peer
- **Solution**: Use `--skip-bootstrap` flag OR run two nodes minimum

### 2. Instant Bootstrap (<1 second)
With consensus parameters K=1, sample-size=1, quorum-size=1:
- Nodes can validate with minimum participants
- Bootstrap should complete instantly when properly configured
- Use `--skip-bootstrap` for guaranteed instant startup

### 3. Database Issue  
- **Problem**: C-Chain only shows blocks 0-100 instead of 1,082,781
- **Cause**: Incomplete canonical chain mappings in database
- **Current Genesis**: `0x3f4fa2a0b0ce089f52bf0ae9199c75ffdd76ecafc987794050cb0d286f1ec61e`
- **Status**: Database repair needed to access full chain

## Solutions Implemented

### 1. Solo Validator (Instant)
```bash
./fix_solo_validator.sh
```
- Uses `--skip-bootstrap` flag
- Single node operational instantly
- Ready for transactions immediately

### 2. Two Validators (Proper Bootstrap)
```bash
./instant_bootstrap.sh
```
- Two nodes with `--skip-bootstrap`
- Both operational in <1 second
- Nodes can discover each other

### 3. Validator & Bootstrapper
```bash
./validator_bootstrapper.sh
```
- Primary node acts as validator and bootstrapper
- Other nodes can connect using its ID
- Proper staking certificates used

### 4. Auto-Discovery Network
```bash
./auto_discovery_setup.sh
```
- NATS-based auto-discovery
- Nodes announce themselves every 5 seconds
- New nodes can join automatically
- 3-node network setup

## Configuration Details

### Network Parameters
- **Network ID**: 96369 (LUX Mainnet)
- **Chain ID**: 96369
- **Consensus**: K=1, sample-size=1, quorum-size=1
- **Validator Address**: 0x9011E888251AB053B7bD1cdB598Db4f9DEd94714

### Stake Requirements
- **P-Chain Staked**: 1,000,000,000 LUX (1B LUX)
- **Minimum Validator Stake**: 1,000,000 LUX (1M LUX)

### Ports
- **HTTP RPC**: 9630 (primary), 9632+ (additional nodes)
- **Staking**: 9651 (primary), 9653+ (additional nodes)
- **NATS Discovery**: 4222

## Next Steps

### 1. Fix Database for Full Chain Access
The database needs repair to access all 1,082,781 blocks:
- Verify canonical mappings for all blocks
- Rebuild missing h<num>n entries
- Set proper head pointers

### 2. Production Deployment
For production with multiple validators:
1. Generate unique staking certificates per validator
2. Configure P-Chain genesis with all validator stakes
3. Use proper public IPs (not 127.0.0.1)
4. Set consensus params based on validator count

### 3. Local Development Network
For chain ID 31337 testing:
- Create new genesis with test accounts
- Prefund address from mnemonic "light light light..." (x11) + "energy"
- Use for smart contract development

## Files Created
- `/home/z/work/lux/node/fix_solo_validator.sh` - Single node with skip-bootstrap
- `/home/z/work/lux/node/instant_bootstrap.sh` - Two nodes instant setup
- `/home/z/work/lux/node/validator_bootstrapper.sh` - Primary validator/bootstrapper
- `/home/z/work/lux/node/auto_discovery_setup.sh` - NATS-based auto-discovery network
- `/home/z/work/lux/node/two_validators_proper.sh` - Standard two validator setup

## Commands Reference

### Check Bootstrap Status
```bash
curl -s -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"info.isBootstrapped","params":{"chain":"C"}}' \
  http://127.0.0.1:9630/ext/info
```

### Get Node ID
```bash
curl -s http://127.0.0.1:9630/ext/info -X POST \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID","params":{}}'
```

### Check Block Number
```bash
curl -s -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
  http://127.0.0.1:9630/ext/bc/C/rpc
```

### Check Balance
```bash
curl -s -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"eth_getBalance","params":["0x9011E888251AB053B7bD1cdB598Db4f9DEd94714","latest"]}' \
  http://127.0.0.1:9630/ext/bc/C/rpc
```

## Summary
The bootstrap issue is resolved using either `--skip-bootstrap` for instant startup or running multiple nodes for proper consensus. The database still needs repair to access the full 1,082,781 blocks of migrated chain data.