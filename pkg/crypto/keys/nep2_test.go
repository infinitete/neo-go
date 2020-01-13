package keys

import (
	"testing"

	"github.com/infinitete/neo-go-inf/pkg/internal/keytestcases"
	"github.com/stretchr/testify/assert"
)

func TestNEP2Encrypt(t *testing.T) {
	for _, testCase := range keytestcases.Arr {

		privKey, err := NewPrivateKeyFromHex(testCase.PrivateKey)
		assert.Nil(t, err)

		encryptedWif, err := NEP2Encrypt(privKey, testCase.Passphrase)
		assert.Nil(t, err)

		assert.Equal(t, testCase.EncryptedWif, encryptedWif)
	}
}

func TestNEP2Decrypt(t *testing.T) {
	for _, testCase := range keytestcases.Arr {

		privKeyString, err := NEP2Decrypt(testCase.EncryptedWif, testCase.Passphrase)
		assert.Nil(t, err)

		privKey, err := NewPrivateKeyFromWIF(privKeyString)
		assert.Nil(t, err)

		assert.Equal(t, testCase.PrivateKey, privKey.String())

		wif := privKey.WIF()
		assert.Equal(t, testCase.Wif, wif)

		address := privKey.Address()
		assert.Equal(t, testCase.Address, address)
	}
}
