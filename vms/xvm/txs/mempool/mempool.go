// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"sync"

	"github.com/luxfi/consensus/config"
	"github.com/luxfi/ids"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/txs/auth"
	"github.com/luxfi/node/vms/xvm/txs"
	txmempool "github.com/luxfi/node/vms/txs/mempool"
)

type Mempool struct {
	txmempool.Mempool[*txs.Tx]

	// authMu guards the auth-policy fields. The policy is optional —
	// callers that haven't migrated their construction path get the
	// classical-compat default. Once a chain pins a non-nil profile
	// via SetAuthPolicy, Add enforces it.
	authMu       sync.RWMutex
	authPolicy   *config.ChainSecurityProfile
	authRegistry auth.ClassicalCompatRegistry
}

func New(namespace string, registerer metric.Registerer) (*Mempool, error) {
	metrics, err := txmempool.NewMetrics(namespace, registerer)
	if err != nil {
		return nil, err
	}
	pool := txmempool.New[*txs.Tx](
		metrics,
	)
	return &Mempool{Mempool: pool}, nil
}

// SetAuthPolicy installs the chain's credential-admission policy. See
// platformvm/txs/mempool.SetAuthPolicy for the contract; X-chain wraps
// every credential in fxs.FxCredential, so the policy gate runs over
// the unwrapped inner credentials.
func (m *Mempool) SetAuthPolicy(profile *config.ChainSecurityProfile, registry auth.ClassicalCompatRegistry) {
	m.authMu.Lock()
	m.authPolicy = profile
	m.authRegistry = registry
	m.authMu.Unlock()
}

func (m *Mempool) authPolicySnapshot() (*config.ChainSecurityProfile, auth.ClassicalCompatRegistry) {
	m.authMu.RLock()
	defer m.authMu.RUnlock()
	return m.authPolicy, m.authRegistry
}

func (m *Mempool) Add(tx *txs.Tx) error {
	profile, registry := m.authPolicySnapshot()
	if profile != nil {
		// X-chain credentials are wrapped in fxs.FxCredential; unwrap
		// so auth.EnforceCredentialPolicy can recognise the inner
		// *secp256k1fx.Credential type.
		unwrapped := make([]verify.Verifiable, len(tx.Creds))
		for i, c := range tx.Creds {
			if c != nil {
				unwrapped[i] = c.Credential
			}
		}
		if err := auth.EnforceCredentialPolicy(unwrapped, profile, registry, ids.ShortEmpty); err != nil {
			return err
		}
	}
	return m.Mempool.Add(tx)
}

func (m *Mempool) HasTxs() bool {
	return m.Len() > 0
}
