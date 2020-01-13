package transaction

import "github.com/infinitete/neo-go-inf/pkg/io"

// TXer is interface that can act as the underlying data of
// a transaction.
type TXer interface {
	io.Serializable
}
