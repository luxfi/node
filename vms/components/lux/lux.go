package lux

import "github.com/luxfi/ids"

// UTXO represents an unspent transaction output
type UTXO struct {
    UTXOID
    Asset
    Out interface{}
}

// UTXOID uniquely identifies a UTXO
type UTXOID struct {
    TxID        ids.ID
    OutputIndex uint32
}

// Asset represents an asset
type Asset struct {
    ID ids.ID
}

// TransferableInput represents a transferable input
type TransferableInput struct {
    UTXOID
    Asset
    In interface{}
}

// TransferableOutput represents a transferable output
type TransferableOutput struct {
    Asset
    Out interface{}
}
