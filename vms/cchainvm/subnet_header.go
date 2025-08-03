// SubnetEVM header handling
package cchainvm

import (
	"math/big"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
)

// SubnetEVMHeader includes the extra AcceptedTime field used by SubnetEVM
type SubnetEVMHeader struct {
	ParentHash  common.Hash      `json:"parentHash"       gencodec:"required"`
	UncleHash   common.Hash      `json:"sha3Uncles"       gencodec:"required"`
	Coinbase    common.Address   `json:"miner"`
	Root        common.Hash      `json:"stateRoot"        gencodec:"required"`
	TxHash      common.Hash      `json:"transactionsRoot" gencodec:"required"`
	ReceiptHash common.Hash      `json:"receiptsRoot"     gencodec:"required"`
	Bloom       types.Bloom      `json:"logsBloom"        gencodec:"required"`
	Difficulty  *big.Int         `json:"difficulty"       gencodec:"required"`
	Number      *big.Int         `json:"number"           gencodec:"required"`
	GasLimit    uint64           `json:"gasLimit"         gencodec:"required"`
	GasUsed     uint64           `json:"gasUsed"          gencodec:"required"`
	Time        uint64           `json:"timestamp"        gencodec:"required"`
	Extra       []byte           `json:"extraData"        gencodec:"required"`
	MixDigest   common.Hash      `json:"mixHash"`
	Nonce       types.BlockNonce `json:"nonce"`
	BaseFee     *big.Int         `json:"baseFeePerGas" rlp:"optional"`
	AcceptedTime *big.Int        `json:"acceptedTime" rlp:"optional"` // SubnetEVM specific field
}

// ToGethHeader converts SubnetEVM header to standard geth header
func (h *SubnetEVMHeader) ToGethHeader() *types.Header {
	return &types.Header{
		ParentHash:  h.ParentHash,
		UncleHash:   h.UncleHash,
		Coinbase:    h.Coinbase,
		Root:        h.Root,
		TxHash:      h.TxHash,
		ReceiptHash: h.ReceiptHash,
		Bloom:       h.Bloom,
		Difficulty:  h.Difficulty,
		Number:      h.Number,
		GasLimit:    h.GasLimit,
		GasUsed:     h.GasUsed,
		Time:        h.Time,
		Extra:       h.Extra,
		MixDigest:   h.MixDigest,
		Nonce:       h.Nonce,
		BaseFee:     h.BaseFee,
		// WithdrawalsHash is nil for pre-Shanghai blocks
	}
}