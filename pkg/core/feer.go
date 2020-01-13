package core

import (
	"github.com/infinitete/neo-go-inf/pkg/core/transaction"
	"github.com/infinitete/neo-go-inf/pkg/util"
)

// Feer is an interface that abstract the implementation of the fee calculation.
type Feer interface {
	NetworkFee(t *transaction.Transaction) util.Fixed8
	IsLowPriority(t *transaction.Transaction) bool
	FeePerByte(t *transaction.Transaction) util.Fixed8
	SystemFee(t *transaction.Transaction) util.Fixed8
}
