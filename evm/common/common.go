package common

// Re-export commonly used types from github.com/luxfi/geth/common
import "github.com/luxfi/geth/common"

type Address = common.Address
type Hash = common.Hash

var (
	HexToAddress = common.HexToAddress
	HexToHash    = common.HexToHash
)