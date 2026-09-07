package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/vms/platformvm/txs"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// queue records what CreateChain hands the chain manager. Nothing else on the
// manager is reached, so the embedded interface stays nil.
type queue struct {
	chains.Manager
	got []chains.ChainParameters
}

func (q *queue) QueueChainCreation(p chains.ChainParameters) { q.got = append(q.got, p) }

func createChainTx(t *testing.T, net ids.ID) *txs.CreateChainTx {
	tx, err := txs.NewCreateChainTx(&lux.BaseTx{}, net, "c", ids.GenerateTestID(), nil, []byte("{}"),
		&secp256k1fx.Input{SigIndices: []uint32{0}})
	require.NoError(t, err)
	return tx
}

func TestCreateChainTracking(t *testing.T) {
	other := ids.GenerateTestID()
	blockchain := ids.GenerateTestID()

	tests := []struct {
		name    string
		sybil   bool
		all     bool
		tracked []ids.ID
		net     ids.ID
		created bool
	}{
		{"primary network is never gated", true, false, nil, constants.PrimaryNetworkID, true},
		{"another net needs tracking", true, false, nil, other, false},
		{"tracked by net id", true, false, []ids.ID{other}, other, true},
		{"tracked by blockchain id", true, false, []ids.ID{blockchain}, other, true},
		{"track all", true, true, nil, other, true},
		{"sybil protection off", false, false, nil, other, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracked := set.NewSet[ids.ID](len(tt.tracked))
			for _, id := range tt.tracked {
				tracked.Add(id)
			}
			q := &queue{}
			c := &Internal{Chains: q, SybilProtectionEnabled: tt.sybil, TrackAllChains: tt.all, TrackedChains: tracked}
			c.CreateChain(blockchain, createChainTx(t, tt.net))
			require.Equal(t, tt.created, len(q.got) == 1)
			if tt.created {
				require.Equal(t, tt.net, q.got[0].ChainID)
				require.Equal(t, blockchain, q.got[0].ID)
			}
		})
	}
}
