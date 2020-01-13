package core

import (
	"testing"

	"github.com/infinitete/neo-go/pkg/core/storage"
	"github.com/infinitete/neo-go/pkg/core/transaction"
	"github.com/infinitete/neo-go/pkg/crypto/keys"
	"github.com/infinitete/neo-go/pkg/io"
	"github.com/infinitete/neo-go/pkg/util"
	"github.com/stretchr/testify/assert"
)

func TestEncodeDecodeAssetState(t *testing.T) {
	asset := &AssetState{
		ID:         randomUint256(),
		AssetType:  transaction.Token,
		Name:       "super cool token",
		Amount:     util.Fixed8(1000000),
		Available:  util.Fixed8(100),
		Precision:  0,
		FeeMode:    feeMode,
		Owner:      &keys.PublicKey{},
		Admin:      randomUint160(),
		Issuer:     randomUint160(),
		Expiration: 10,
		IsFrozen:   false,
	}

	buf := io.NewBufBinWriter()
	asset.EncodeBinary(buf.BinWriter)
	assert.Nil(t, buf.Err)
	assetDecode := &AssetState{}
	r := io.NewBinReaderFromBuf(buf.Bytes())
	assetDecode.DecodeBinary(r)
	assert.Nil(t, r.Err)
	assert.Equal(t, asset, assetDecode)
}

func TestPutGetAssetState(t *testing.T) {
	s := storage.NewMemoryStore()
	asset := &AssetState{
		ID:         randomUint256(),
		AssetType:  transaction.Token,
		Name:       "super cool token",
		Amount:     util.Fixed8(1000000),
		Available:  util.Fixed8(100),
		Precision:  8,
		FeeMode:    feeMode,
		Owner:      &keys.PublicKey{},
		Admin:      randomUint160(),
		Issuer:     randomUint160(),
		Expiration: 10,
		IsFrozen:   false,
	}
	assert.NoError(t, putAssetStateIntoStore(s, asset))
	asRead := getAssetStateFromStore(s, asset.ID)
	assert.NotNil(t, asRead)
	assert.Equal(t, asset, asRead)
}
