package payload

import (
	"testing"

	"github.com/infinitete/neo-go-inf/pkg/crypto/hash"
	"github.com/infinitete/neo-go-inf/pkg/io"
	"github.com/infinitete/neo-go-inf/pkg/util"
	"github.com/stretchr/testify/assert"
)

func TestGetBlockEncodeDecode(t *testing.T) {
	start := []util.Uint256{
		hash.Sha256([]byte("a")),
		hash.Sha256([]byte("b")),
		hash.Sha256([]byte("c")),
		hash.Sha256([]byte("d")),
	}

	p := NewGetBlocks(start, util.Uint256{})
	buf := io.NewBufBinWriter()
	p.EncodeBinary(buf.BinWriter)
	assert.Nil(t, buf.Err)

	b := buf.Bytes()
	r := io.NewBinReaderFromBuf(b)
	pDecode := &GetBlocks{}
	pDecode.DecodeBinary(r)
	assert.Nil(t, r.Err)
	assert.Equal(t, p, pDecode)
}

func TestGetBlockEncodeDecodeWithHashStop(t *testing.T) {
	var (
		start = []util.Uint256{
			hash.Sha256([]byte("a")),
			hash.Sha256([]byte("b")),
			hash.Sha256([]byte("c")),
			hash.Sha256([]byte("d")),
		}
		stop = hash.Sha256([]byte("e"))
	)
	p := NewGetBlocks(start, stop)
	buf := io.NewBufBinWriter()
	p.EncodeBinary(buf.BinWriter)
	assert.Nil(t, buf.Err)

	b := buf.Bytes()
	r := io.NewBinReaderFromBuf(b)
	pDecode := &GetBlocks{}
	pDecode.DecodeBinary(r)
	assert.Nil(t, r.Err)
	assert.Equal(t, p, pDecode)
}
