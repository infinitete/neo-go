package core

import (
	"github.com/infinitete/neo-go-inf/pkg/core/storage"
	"github.com/infinitete/neo-go-inf/pkg/crypto/hash"
	"github.com/infinitete/neo-go-inf/pkg/io"
	"github.com/infinitete/neo-go-inf/pkg/smartcontract"
	"github.com/infinitete/neo-go-inf/pkg/util"
)

// Contracts is a mapping between scripthash and ContractState.
type Contracts map[util.Uint160]*ContractState

// ContractState holds information about a smart contract in the NEO blockchain.
type ContractState struct {
	Script      []byte
	ParamList   []smartcontract.ParamType
	ReturnType  smartcontract.ParamType
	Properties  smartcontract.PropertyState
	Name        string
	CodeVersion string
	Author      string
	Email       string
	Description string

	scriptHash util.Uint160
}

// commit flushes all contracts to the given storage.Batch.
func (a Contracts) commit(store storage.Store) error {
	for _, contract := range a {
		if err := putContractStateIntoStore(store, contract); err != nil {
			return err
		}
	}
	return nil
}

// DecodeBinary implements Serializable interface.
func (cs *ContractState) DecodeBinary(br *io.BinReader) {
	cs.Script = br.ReadBytes()
	paramBytes := br.ReadBytes()
	cs.ParamList = make([]smartcontract.ParamType, len(paramBytes))
	for k := range paramBytes {
		cs.ParamList[k] = smartcontract.ParamType(paramBytes[k])
	}
	br.ReadLE(&cs.ReturnType)
	br.ReadLE(&cs.Properties)
	cs.Name = br.ReadString()
	cs.CodeVersion = br.ReadString()
	cs.Author = br.ReadString()
	cs.Email = br.ReadString()
	cs.Description = br.ReadString()
	cs.createHash()
}

// EncodeBinary implements Serializable interface.
func (cs *ContractState) EncodeBinary(bw *io.BinWriter) {
	bw.WriteBytes(cs.Script)
	bw.WriteVarUint(uint64(len(cs.ParamList)))
	for k := range cs.ParamList {
		bw.WriteLE(cs.ParamList[k])
	}
	bw.WriteLE(cs.ReturnType)
	bw.WriteLE(cs.Properties)
	bw.WriteString(cs.Name)
	bw.WriteString(cs.CodeVersion)
	bw.WriteString(cs.Author)
	bw.WriteString(cs.Email)
	bw.WriteString(cs.Description)
}

// putContractStateIntoStore puts given contract state into the given store.
func putContractStateIntoStore(s storage.Store, cs *ContractState) error {
	buf := io.NewBufBinWriter()
	cs.EncodeBinary(buf.BinWriter)
	if buf.Err != nil {
		return buf.Err
	}
	key := storage.AppendPrefix(storage.STContract, cs.ScriptHash().Bytes())
	return s.Put(key, buf.Bytes())
}

// deleteContractStateInStore deletes given contract state in the given store.
func deleteContractStateInStore(s storage.Store, hash util.Uint160) error {
	key := storage.AppendPrefix(storage.STContract, hash.Bytes())
	return s.Delete(key)
}

// ScriptHash returns a contract script hash.
func (cs *ContractState) ScriptHash() util.Uint160 {
	if cs.scriptHash.Equals(util.Uint160{}) {
		cs.createHash()
	}
	return cs.scriptHash
}

// createHash creates contract script hash.
func (cs *ContractState) createHash() {
	cs.scriptHash = hash.Hash160(cs.Script)
}

// HasStorage checks whether the contract has storage property set.
func (cs *ContractState) HasStorage() bool {
	return (cs.Properties & smartcontract.HasStorage) != 0
}

// HasDynamicInvoke checks whether the contract has dynamic invoke property set.
func (cs *ContractState) HasDynamicInvoke() bool {
	return (cs.Properties & smartcontract.HasDynamicInvoke) != 0
}

// IsPayable checks whether the contract has payable property set.
func (cs *ContractState) IsPayable() bool {
	return (cs.Properties & smartcontract.IsPayable) != 0
}
