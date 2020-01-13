package wrappers

import (
	"github.com/infinitete/neo-go-inf/pkg/crypto"
)

// ValidateAddressResponse represents response to validate address call.
type ValidateAddressResponse struct {
	Address interface{} `json:"address"`
	IsValid bool        `json:"isvalid"`
}

// ValidateAddress verifies that the address is a correct NEO address
// see https://docs.neo.org/en-us/node/cli/2.9.4/api/validateaddress.html
func ValidateAddress(address interface{}) ValidateAddressResponse {
	resp := ValidateAddressResponse{Address: address}
	if address, ok := address.(string); ok {
		_, err := crypto.Uint160DecodeAddress(address)
		resp.IsValid = err == nil
	}
	return resp
}
