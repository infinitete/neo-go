package core

import (
	"time"

	"github.com/infinitete/neo-go-inf/config"
	"github.com/infinitete/neo-go-inf/pkg/core/storage"
	"github.com/infinitete/neo-go-inf/pkg/core/transaction"
	"github.com/infinitete/neo-go-inf/pkg/crypto/hash"
	"github.com/infinitete/neo-go-inf/pkg/crypto/keys"
	"github.com/infinitete/neo-go-inf/pkg/io"
	"github.com/infinitete/neo-go-inf/pkg/smartcontract"
	"github.com/infinitete/neo-go-inf/pkg/util"
	"github.com/infinitete/neo-go-inf/pkg/vm"
)

// createGenesisBlock creates a genesis block based on the given configuration.
func createGenesisBlock(cfg config.ProtocolConfiguration) (*Block, error) {
	validators, err := getValidators(cfg)
	if err != nil {
		return nil, err
	}

	nextConsensus, err := getNextConsensusAddress(validators)
	if err != nil {
		return nil, err
	}

	base := BlockBase{
		Version:       0,
		PrevHash:      util.Uint256{},
		Timestamp:     uint32(time.Date(2016, 7, 15, 15, 8, 21, 0, time.UTC).Unix()),
		Index:         0,
		ConsensusData: 2083236893,
		NextConsensus: nextConsensus,
		Script: &transaction.Witness{
			InvocationScript:   []byte{},
			VerificationScript: []byte{byte(vm.PUSHT)},
		},
	}

	governingTX := governingTokenTX()
	utilityTX := utilityTokenTX()
	rawScript, err := smartcontract.CreateMultiSigRedeemScript(
		len(cfg.StandbyValidators)/2+1,
		validators,
	)
	if err != nil {
		return nil, err
	}
	scriptOut := hash.Hash160(rawScript)

	block := &Block{
		BlockBase: base,
		Transactions: []*transaction.Transaction{
			{
				Type: transaction.MinerType,
				Data: &transaction.MinerTX{
					Nonce: 2083236893,
				},
				Attributes: []*transaction.Attribute{},
				Inputs:     []*transaction.Input{},
				Outputs:    []*transaction.Output{},
				Scripts:    []*transaction.Witness{},
			},
			governingTX,
			utilityTX,
			{
				Type:   transaction.IssueType,
				Data:   &transaction.IssueTX{}, // no fields.
				Inputs: []*transaction.Input{},
				Outputs: []*transaction.Output{
					{
						AssetID:    governingTX.Hash(),
						Amount:     governingTX.Data.(*transaction.RegisterTX).Amount,
						ScriptHash: scriptOut,
					},
				},
				Scripts: []*transaction.Witness{
					{
						InvocationScript:   []byte{},
						VerificationScript: []byte{byte(vm.PUSHT)},
					},
				},
			},
		},
	}

	if err = block.rebuildMerkleRoot(); err != nil {
		return nil, err
	}

	return block, nil
}

func governingTokenTX() *transaction.Transaction {
	admin := hash.Hash160([]byte{byte(vm.PUSHT)})
	registerTX := &transaction.RegisterTX{
		AssetType: transaction.GoverningToken,
		Name:      "[{\"lang\":\"zh-CN\",\"name\":\"小蚁股\"},{\"lang\":\"en\",\"name\":\"AntShare\"}]",
		Amount:    util.Fixed8FromInt64(100000000),
		Precision: 0,
		Owner:     &keys.PublicKey{},
		Admin:     admin,
	}

	tx := &transaction.Transaction{
		Type:       transaction.RegisterType,
		Data:       registerTX,
		Attributes: []*transaction.Attribute{},
		Inputs:     []*transaction.Input{},
		Outputs:    []*transaction.Output{},
		Scripts:    []*transaction.Witness{},
	}

	return tx
}

func utilityTokenTX() *transaction.Transaction {
	admin := hash.Hash160([]byte{byte(vm.PUSHF)})
	registerTX := &transaction.RegisterTX{
		AssetType: transaction.UtilityToken,
		Name:      "[{\"lang\":\"zh-CN\",\"name\":\"小蚁币\"},{\"lang\":\"en\",\"name\":\"AntCoin\"}]",
		Amount:    calculateUtilityAmount(),
		Precision: 8,
		Owner:     &keys.PublicKey{},
		Admin:     admin,
	}
	tx := &transaction.Transaction{
		Type:       transaction.RegisterType,
		Data:       registerTX,
		Attributes: []*transaction.Attribute{},
		Inputs:     []*transaction.Input{},
		Outputs:    []*transaction.Output{},
		Scripts:    []*transaction.Witness{},
	}

	return tx
}

func getValidators(cfg config.ProtocolConfiguration) ([]*keys.PublicKey, error) {
	validators := make([]*keys.PublicKey, len(cfg.StandbyValidators))
	for i, pubKeyStr := range cfg.StandbyValidators {
		pubKey, err := keys.NewPublicKeyFromString(pubKeyStr)
		if err != nil {
			return nil, err
		}
		validators[i] = pubKey
	}
	return validators, nil
}

func getNextConsensusAddress(validators []*keys.PublicKey) (val util.Uint160, err error) {
	vlen := len(validators)
	raw, err := smartcontract.CreateMultiSigRedeemScript(
		vlen-(vlen-1)/3,
		validators,
	)
	if err != nil {
		return val, err
	}
	return hash.Hash160(raw), nil
}

func calculateUtilityAmount() util.Fixed8 {
	sum := 0
	for i := 0; i < len(genAmount); i++ {
		sum += genAmount[i]
	}
	return util.Fixed8FromInt64(int64(sum * decrementInterval))
}

// headerSliceReverse reverses the given slice of *Header.
func headerSliceReverse(dest []*Header) {
	for i, j := 0, len(dest)-1; i < j; i, j = i+1, j-1 {
		dest[i], dest[j] = dest[j], dest[i]
	}
}

// storeAsCurrentBlock stores the given block witch prefix
// SYSCurrentBlock.
func storeAsCurrentBlock(store storage.Store, block *Block) error {
	buf := io.NewBufBinWriter()
	buf.WriteLE(block.Hash().BytesReverse())
	buf.WriteLE(block.Index)
	return store.Put(storage.SYSCurrentBlock.Bytes(), buf.Bytes())
}

// storeAsBlock stores the given block as DataBlock.
func storeAsBlock(store storage.Store, block *Block, sysFee uint32) error {
	var (
		key = storage.AppendPrefix(storage.DataBlock, block.Hash().BytesReverse())
		buf = io.NewBufBinWriter()
	)
	// sysFee needs to be handled somehow
	//	buf.WriteLE(sysFee)
	b, err := block.Trim()
	if err != nil {
		return err
	}
	buf.WriteLE(b)
	if buf.Err != nil {
		return buf.Err
	}
	return store.Put(key, buf.Bytes())
}

// storeAsTransaction stores the given TX as DataTransaction.
func storeAsTransaction(store storage.Store, tx *transaction.Transaction, index uint32) error {
	key := storage.AppendPrefix(storage.DataTransaction, tx.Hash().BytesReverse())
	buf := io.NewBufBinWriter()
	buf.WriteLE(index)
	tx.EncodeBinary(buf.BinWriter)
	if buf.Err != nil {
		return buf.Err
	}
	return store.Put(key, buf.Bytes())
}
