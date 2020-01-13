package core

import (
	"testing"

	"github.com/infinitete/neo-go-inf/pkg/core/storage"
	"github.com/infinitete/neo-go-inf/pkg/util"
	"github.com/stretchr/testify/assert"
)

func TestPutGetDeleteStorageItem(t *testing.T) {
	s := storage.NewMemoryStore()
	si := &StorageItem{
		Value: []byte("smth"),
	}
	key := []byte("key")
	cHash, err := util.Uint160DecodeBytes([]byte("abcdefghijklmnopqrst"))
	assert.Nil(t, err)
	assert.NoError(t, putStorageItemIntoStore(s, cHash, key, si))
	siRead := getStorageItemFromStore(s, cHash, key)
	assert.NotNil(t, siRead)
	assert.Equal(t, si, siRead)
	assert.NoError(t, deleteStorageItemInStore(s, cHash, key))
	siRead2 := getStorageItemFromStore(s, cHash, key)
	assert.Nil(t, siRead2)
}
