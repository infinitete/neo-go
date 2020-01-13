package core

import (
	"testing"
	"time"

	"github.com/infinitete/neo-go/pkg/core/transaction"
	"github.com/infinitete/neo-go/pkg/crypto/hash"
	"github.com/infinitete/neo-go/pkg/io"
	"github.com/infinitete/neo-go/pkg/util"
	"github.com/stretchr/testify/assert"
)

func TestHeaderEncodeDecode(t *testing.T) {
	header := Header{BlockBase: BlockBase{
		Version:       0,
		PrevHash:      hash.Sha256([]byte("prevhash")),
		MerkleRoot:    hash.Sha256([]byte("merkleroot")),
		Timestamp:     uint32(time.Now().UTC().Unix()),
		Index:         3445,
		ConsensusData: 394949,
		NextConsensus: util.Uint160{},
		Script: &transaction.Witness{
			InvocationScript:   []byte{0x10},
			VerificationScript: []byte{0x11},
		},
	}}

	buf := io.NewBufBinWriter()
	header.EncodeBinary(buf.BinWriter)
	assert.Nil(t, buf.Err)

	headerDecode := &Header{}
	r := io.NewBinReaderFromBuf(buf.Bytes())
	headerDecode.DecodeBinary(r)
	assert.Nil(t, r.Err)
	assert.Equal(t, header.Version, headerDecode.Version, "expected both versions to be equal")
	assert.Equal(t, header.PrevHash, headerDecode.PrevHash, "expected both prev hashes to be equal")
	assert.Equal(t, header.MerkleRoot, headerDecode.MerkleRoot, "expected both merkle roots to be equal")
	assert.Equal(t, header.Index, headerDecode.Index, "expected both indexes to be equal")
	assert.Equal(t, header.ConsensusData, headerDecode.ConsensusData, "expected both consensus data fields to be equal")
	assert.Equal(t, header.NextConsensus, headerDecode.NextConsensus, "expected both next consensus fields to be equal")
	assert.Equal(t, header.Script.InvocationScript, headerDecode.Script.InvocationScript, "expected equal invocation scripts")
	assert.Equal(t, header.Script.VerificationScript, headerDecode.Script.VerificationScript, "expected equal verification scripts")
}
