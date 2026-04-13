# Chains

The Lux Network consists of the Primary Chainwork and a collection of
blockchain networks.

## Chain Creation

Chains are created by issuing a *CreateChainTx*. After a *CreateChainTx* is
accepted, a new chain will exist with the *ChainID* equal to the *TxID* of the
*CreateChainTx*. The *CreateChainTx* creates a permissioned chain. The
*Owner* field in *CreateChainTx* specifies who can modify the state of the
chain.

## Permissioned Chains

A permissioned chain can be modified by a few different transactions.

- CreateChainTx
  - Creates a new chain that will be validated by all validators of the chain.
- AddChainValidatorTx
  - Adds a new validator to the chain with the specified *StartTime*,
    *EndTime*, and *Weight*.
- RemoveChainValidatorTx
  - Removes a validator from the chain.
- TransformChainTx
  - Converts the permissioned chain into a permissionless chain.
  - Specifies all of the staking parameters.
    - LUX is not allowed to be used as a staking token. In general, it is not
      advisable to have multiple chains using the same staking token.
  - After becoming a permissionless chain, previously added permissioned
    validators will remain to finish their staking period.
  - No more chains will be able to be added to the chain.
