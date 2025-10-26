package benchlist

import "github.com/luxfi/ids"
import "github.com/luxfi/consensus/validators"

type Config struct{}

type Manager interface {
	IsBenched(nodeID ids.NodeID, chainID ids.ID) bool
	GetBenched(chainID ids.ID) []ids.NodeID
	RegisterChain(chainID ids.ID, vdrs validators.Manager) error
	Benchable(chainID ids.ID, nodeID ids.NodeID) Benchable
}

type Benchable interface {
	Benched(chainID ids.ID, nodeID ids.NodeID)
	Unbenched(chainID ids.ID, nodeID ids.NodeID)
}
