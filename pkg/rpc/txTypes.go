package rpc

/*
	Definition of types, interfaces and variables
	required for raw transaction composing.
*/

import (
	"github.com/infinitete/neo-go-inf/pkg/core/transaction"
	"github.com/infinitete/neo-go-inf/pkg/crypto/keys"
	"github.com/infinitete/neo-go-inf/pkg/util"
)

type (
	// ContractTxParams contains parameters for tx to transfer assets;
	// includes parameters duplication `sendtoaddress` RPC call params
	// and also some utility data;
	ContractTxParams struct {
		assetID util.Uint256
		address string
		value   util.Fixed8
		wif     keys.WIF // a WIF to send the transaction
		// since there are many ways to provide unspents,
		// transaction composer stays agnostic to that how
		// unspents was got;
		balancer BalanceGetter
	}

	// BalanceGetter is an interface supporting CalculateInputs() method.
	BalanceGetter interface {
		// 		parameters
		// address: 	base58-encoded address assets would be transferred from
		// assetID: 	asset identifier
		// amount: 		an asset amount to spend
		// 		return values
		// inputs: 		UTXO's for the preparing transaction
		// total: 		summarized asset amount from all the `inputs`
		// error: 		error would be considered in the caller function
		CalculateInputs(address string, assetID util.Uint256, amount util.Fixed8) (inputs []transaction.Input, total util.Fixed8, err error)
	}
)
