package snow

import (
    "github.com/luxfi/ids"
    "github.com/luxfi/node/chain"
)

// Context provides snow protocol context
type Context struct {
    consensus.ExtendedContext
    
    // Snow-specific fields
    Epoch      uint32
    ChainTime  uint64
}

// Bootstrapper interface
type Bootstrapper interface {
    Start() error
    Stop() error
}
