# Subnet Configs

It is possible to provide parameters for a Subnet. Parameters here apply to all
chains in the specified Subnet.

Lux looks for files specified with `{subnetID}.json` under
`--subnet-config-dir` as documented
[here](https://build.lux.network/docs/nodes/configure/configs-flags#subnet-configs).

Here is an example of Subnet config file:

```json
{
  "validatorOnly": false,

  // --- Consensus ---
  "consensusParameters": {
    "k": 12,              // sample 12 validators
    "alpha": 8,           // need 8 matching votes
    "beta": 10,           // Snowman commit threshold
    "concurrentRepolls": 16,
    "optimalProcessing": 2048,
    "maxOutstandingItems": 4096,
    "maxItemProcessingTime": "250ms",

    // DAG-specific fast path
    "betaVirtuous": 4,    // 4 rounds for conflict-free txs
    "betaRogue": 12,      // 12 for conflicts
    "batchSize": 40,
    "parentSize": 2
  },

  // --- Block production cadence ---
  "proposerMinBlockDelay": "200ms"   // see §5
}
```

## Parameters

### Private Subnet

#### `validatorOnly` (bool)

If `true` this node does not expose Subnet blockchain contents to non-validators
via P2P messages. Defaults to `false`.

Lux Subnets are public by default. It means that every node can sync and
listen ongoing transactions/blocks in Subnets, even they're not validating the
listened Subnet.

Subnet validators can choose not to publish contents of blockchains via this
configuration. If a node sets `validatorOnly` to true, the node exchanges
messages only with this Subnet's validators. Other peers will not be able to
learn contents of this Subnet from this node.

:::tip

This is a node-specific configuration. Every validator of this Subnet has to use
this configuration in order to create a full private Subnet.

:::

#### `allowedNodes` (string list)

If `validatorOnly=true` this allows explicitly specified NodeIDs to be allowed
to sync the Subnet regardless of validator status. Defaults to be empty.

:::tip

This is a node-specific configuration. Every validator of this Subnet has to use
this configuration in order to properly allow a node in the private Subnet.

:::

### Consensus Parameters

Subnet configs supports loading new consensus parameters. JSON keys are
different from their matching `CLI` keys. These parameters must be grouped under
`consensusParameters` key. The consensus parameters of a Subnet default to the
same values used for the Primary Network, which are given [CLI Snow Parameters](https://build.lux.network/docs/nodes/configure/configs-flags#snow-parameters).

| CLI Key                          | JSON Key              |
| :------------------------------- | :-------------------- |
| --snow-sample-size               | k                     |
| --snow-quorum-size               | alpha                 |
| --snow-commit-threshold          | `beta`                |
| --snow-concurrent-repolls        | concurrentRepolls     |
| --snow-optimal-processing        | `optimalProcessing`   |
| --snow-max-processing            | maxOutstandingItems   |
| --snow-max-time-processing       | maxItemProcessingTime |
| --snow-lux-batch-size      | `batchSize`           |
| --snow-lux-num-parents     | `parentSize`          |

#### `proposerMinBlockDelay` (duration)

The minimum delay performed when building linear++ blocks. Default is set to 1 second.

As one of the ways to control network congestion, Linear++ will only build a
block `proposerMinBlockDelay` after the parent block's timestamp. Some
high-performance custom VM may find this too strict. This flag allows tuning the
frequency at which blocks are built.

- The flag enforces `parent.Timestamp + delay` before the next block can be built; default is 1 second.
- 200 ms > 50 ms RTT × a 2–3× jitter guard; keeps uncle/orphan rate comfortably low while still allowing ≈4–5 blocks s⁻¹.
- Pushing to 100 ms is possible but expect a sharp rise in parallel proposals and wasted work.

### Gossip Configs

It's possible to define different Gossip configurations for each Subnet without
changing values for Primary Network. JSON keys of these
parameters are different from their matching `CLI` keys. These parameters
default to the same values used for the Primary Network. For more information
see [CLI Gossip Configs](https://build.lux.network/docs/nodes/configure/configs-flags#gossiping).

| CLI Key                                                 | JSON Key                               |
| :------------------------------------------------------ | :------------------------------------- |
| --consensus-accepted-frontier-gossip-validator-size     | gossipAcceptedFrontierValidatorSize    |
| --consensus-accepted-frontier-gossip-non-validator-size | gossipAcceptedFrontierNonValidatorSize |
| --consensus-accepted-frontier-gossip-peer-size          | gossipAcceptedFrontierPeerSize         |
| --consensus-on-accept-gossip-validator-size             | gossipOnAcceptValidatorSize            |
| --consensus-on-accept-gossip-non-validator-size         | gossipOnAcceptNonValidatorSize         |
| --consensus-on-accept-gossip-peer-size                  | gossipOnAcceptPeerSize                 |
