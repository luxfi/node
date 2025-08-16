// Copyright (C) 2019-2023, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto/tls"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/peer"
	"github.com/luxfi/node/staking"
)

var (
	certLock   sync.Mutex
	tlsCerts   []*tls.Certificate
	tlsConfigs []*tls.Config
)

func getTLS(t *testing.T, index int) (ids.NodeID, *tls.Certificate, *tls.Config) {
	certLock.Lock()
	defer certLock.Unlock()

	for len(tlsCerts) <= index {
		cert, err := staking.NewTLSCert()
		require.NoError(t, err)
		tlsConfig := peer.TLSConfig(*cert, nil)

		tlsCerts = append(tlsCerts, cert)
		tlsConfigs = append(tlsConfigs, tlsConfig)
	}

	tlsCert := tlsCerts[index]
	nodeID := ids.NodeIDFromCert(&ids.Certificate{
		Raw:       tlsCert.Leaf.Raw,
		PublicKey: tlsCert.Leaf.PublicKey,
	})
	return nodeID, tlsCert, tlsConfigs[index]
}
