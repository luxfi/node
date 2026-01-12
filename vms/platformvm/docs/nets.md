# Nets

The Lux network consists of the Primary Network and a collection of
sub-networks (nets).

## Net Creation

Nets are created by issuing a *CreateNetTx*. After a *CreateNetTx* is
accepted, a new net will exist with the *NetID* equal to the *TxID* of the
*CreateNetTx*. The *CreateNetTx* creates a permissioned net. The
*Owner* field in *CreateNetTx* specifies who can modify the state of the
net.

## Permissioned Nets

A permissioned net can be modified by a few different transactions.

- CreateChainTx
  - Creates a new chain that will be validated by all validators of the net.
- AddNetValidatorTx
  - Adds a new validator to the net with the specified *StartTime*,
    *EndTime*, and *Weight*.
- RemoveNetValidatorTx
  - Removes a validator from the net.
- TransformNetTx
  - Converts the permissioned net into a permissionless net.
  - Specifies all of the staking parameters.
    - LUX is not allowed to be used as a staking token. In general, it is not
      advisable to have multiple nets using the same staking token.
  - After becoming a permissionless net, previously added permissioned
    validators will remain to finish their staking period.
  - No more chains will be able to be added to the net.

### Permissionless Nets

Currently, nothing can be performed on a permissionless net.
