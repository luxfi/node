// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

/*
Package consensus provides node-side consensus integration for the Lux node.

The canonical consensus protocol implementation lives in the
github.com/luxfi/consensus module. This package only contains the node-side
glue (Acceptor callbacks, Quasar wiring) needed to wire the consensus engine
into the node's chain manager and indexer.

# Components

Acceptor: Callback interface invoked before blocks are committed as
accepted. Multiple acceptors can be registered per chain via AcceptorGroup.
This is the node-side hook that adapts consensus acceptance to chain-ID-keyed
runtime contexts (indexer, warp, IPC).

Quasar: Node-side wiring around github.com/luxfi/consensus/protocol/quasar.
The quasar subpackage adapts P-Chain validator state, BLS signers, and the
Ringtail threshold coordinator into the canonical consensus engine.

# Quasar Integration

The Quasar engine achieves hybrid finality by running two signature paths
in parallel:

  - BLS Path: Fast aggregate signatures from 2/3+ validators
  - Ringtail Path: Post-quantum threshold signatures (t-of-n)

Blocks achieve quantum finality only when both paths complete successfully.

See github.com/luxfi/consensus for the canonical protocol implementation.
*/
package consensus
