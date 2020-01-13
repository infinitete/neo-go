package wrappers

import (
	"github.com/infinitete/neo-go-inf/pkg/vm"
)

// InvokeResult is used as a wrapper to represent an invokation result.
type InvokeResult struct {
	State       string `json:"state"`
	GasConsumed string `json:"gas_consumed"`
	Script      string `json:"script"`
	Stack       *vm.Stack
}
