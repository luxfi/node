# Genesis

The Genesis package converts formatted JSON files into the genesis of the Primary Network. For the simplest example, see the [Local Genesis](./genesis_local.json) JSON file.

The genesis JSON file contains the following properties:

- `networkID`: A unique identifier for the blockchain, must be a number in the range [0, 2^32).
- `allocations`: The list of initial addresses, their initial balances and the unlock schedule for each.
- `startTime`: The time of the beginning of the blockchain, it must be a Unix
  timestamp and it can't be a time in the future.
- `initialStakeDuration`: The stake duration, in seconds, of the validators that exist at network genesis.
- `initialStakeDurationOffset`: The offset, in seconds, between the end times
  of the validators that exist at genesis.
- `initialStakedFunds`: A list of addresses that own the funds staked at genesis
  (each address must be present in `allocations` as well)
- `initialStakers`: The validators that exist at genesis. Each element contains
  the `rewardAddress`, NodeID and the `delegationFee` of the validator.
- `cChainGenesis`: The genesis info to be passed to the C-Chain.
- `message`: A message to include in the genesis. Not required.

## Allocations and Genesis Stakers

Each allocation contains the following fields:

- `ethAddr`: Annotation of corresponding Ethereum address holding an ERC-20 token
- `luxAddr`: X/P Chain address to receive the allocation
- `initialAmount`: Initial unlocked amount minted to the `luxAddr` on the X-Chain
- `unlockSchedule`: List of locked, stakeable UTXOs minted to `luxAddr` on the P-Chain

Note: if an `luxAddr` from allocations is included in `initialStakers`, the genesis includes
all the UTXOs specified in the `unlockSchedule` as part of the locked stake of the corresponding
genesis validator. Otherwise, the locked UTXO is created directly on the P-Chain.

## Subnet Consensus Overrides

To bake custom DAG or Linear consensus defaults into your Primary Network genesis,
provide a Subnet config file named `<subnetID>.json` under `configs/subnets/` alongside this genesis.
When the node starts, it merges these overrides on top of its built-in defaults.

Example `configs/subnets/<YourSubnetID>.json`:
```json
{
  "validatorOnly": false,

  // --- Consensus ---
  "consensusParameters": {
    "k": 12,              // sample 12 validators
    "alpha": 8,           // need 8 matching votes
    "beta": 10,           // commit threshold
    "concurrentRepolls": 16,
    "optimalProcessing": 2048,
    "maxOutstandingItems": 4096,
    "maxItemProcessingTime": "250ms",

    // DAG-specific fast path
    "betaVirtuous": 4,
    "betaRogue": 12,
    "batchSize": 40,
    "parentSize": 2
  },

  // --- Block production cadence ---
  "proposerMinBlockDelay": "200ms"   // see consensus docs
}
```
