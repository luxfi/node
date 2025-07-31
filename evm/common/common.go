package common

// Re-export commonly used types from github.com/ethereum/go-ethereum/common
import "github.com/ethereum/go-ethereum/common"

type Address = common.Address
type Hash = common.Hash

var (
	HexToAddress = common.HexToAddress
	HexToHash    = common.HexToHash
)