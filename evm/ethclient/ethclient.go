package ethclient

// Re-export ethclient from github.com/ethereum/go-ethereum/ethclient
import "github.com/ethereum/go-ethereum/ethclient"

type Client = ethclient.Client

var Dial = ethclient.Dial