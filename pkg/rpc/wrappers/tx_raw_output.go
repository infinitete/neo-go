package wrappers

import (
	"github.com/infinitete/neo-go-inf/pkg/core"
	"github.com/infinitete/neo-go-inf/pkg/core/transaction"
	"github.com/infinitete/neo-go-inf/pkg/io"
	"github.com/infinitete/neo-go-inf/pkg/util"
)

// TransactionOutputRaw is used as a wrapper to represents
// a Transaction.
type TransactionOutputRaw struct {
	*transaction.Transaction
	TxHash        util.Uint256 `json:"txid"`
	Size          int          `json:"size"`
	SysFee        util.Fixed8  `json:"sys_fee"`
	NetFee        util.Fixed8  `json:"net_fee"`
	Blockhash     util.Uint256 `json:"blockhash"`
	Confirmations int          `json:"confirmations"`
	Timestamp     uint32       `json:"blocktime"`
}

// NewTransactionOutputRaw returns a new ransactionOutputRaw object.
func NewTransactionOutputRaw(tx *transaction.Transaction, header *core.Header, chain core.Blockchainer) TransactionOutputRaw {
	// confirmations formula
	confirmations := int(chain.BlockHeight() - header.BlockBase.Index + 1)
	// set index position
	for i, o := range tx.Outputs {
		o.Position = i
	}
	return TransactionOutputRaw{
		Transaction:   tx,
		TxHash:        tx.Hash(),
		Size:          io.GetVarSize(tx),
		SysFee:        chain.SystemFee(tx),
		NetFee:        chain.NetworkFee(tx),
		Blockhash:     header.Hash(),
		Confirmations: confirmations,
		Timestamp:     header.Timestamp,
	}
}
