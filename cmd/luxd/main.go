// SPDX-License-Identifier: BSD-3-Clause-Eco
package main

import (
	"fmt"

	consensuschain "github.com/luxfi/consensus/engine/chain"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	p2ppb "github.com/luxfi/proto/node/zap/p2p"
	"github.com/luxfi/validators"
)

func main() {
	var pos consensuschain.VotePosition
	msg := consensuschain.CanonicalVoteMessage(pos)
	var id ids.ID
	var out validators.GetValidatorOutput
	m := &p2ppb.Message{}
	fmt.Println(len(msg), id, out.Light, m, bls.PublicKeyLen)
}
