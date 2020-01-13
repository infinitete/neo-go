package transaction

import "github.com/infinitete/neo-go-inf/pkg/util"

// Result represents the Result of a transaction.
type Result struct {
	AssetID util.Uint256
	Amount  util.Fixed8
}
