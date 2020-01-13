package rpc

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"

	"github.com/infinitete/neo-go-inf/config"
	"github.com/infinitete/neo-go-inf/pkg/core"
	"github.com/infinitete/neo-go-inf/pkg/core/transaction"
	"github.com/infinitete/neo-go-inf/pkg/crypto"
	"github.com/infinitete/neo-go-inf/pkg/io"
	"github.com/infinitete/neo-go-inf/pkg/network"
	"github.com/infinitete/neo-go-inf/pkg/rpc/result"
	"github.com/infinitete/neo-go-inf/pkg/rpc/wrappers"
	"github.com/infinitete/neo-go-inf/pkg/util"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type (
	// Server represents the JSON-RPC 2.0 server.
	Server struct {
		*http.Server
		chain      core.Blockchainer
		config     config.RPCConfig
		coreServer *network.Server
	}
)

var (
	invalidBlockHeightError = func(index int, height int) error {
		return errors.Errorf("Param at index %d should be greater than or equal to 0 and less then or equal to current block height, got: %d", index, height)
	}
)

// NewServer creates a new Server struct.
func NewServer(chain core.Blockchainer, conf config.RPCConfig, coreServer *network.Server) Server {
	httpServer := &http.Server{
		Addr: conf.Address + ":" + strconv.FormatUint(uint64(conf.Port), 10),
	}

	return Server{
		Server:     httpServer,
		chain:      chain,
		config:     conf,
		coreServer: coreServer,
	}
}

// Start creates a new JSON-RPC server
// listening on the configured port.
func (s *Server) Start(errChan chan error) {
	if !s.config.Enabled {
		log.Info("RPC server is not enabled")
		return
	}
	s.Handler = http.HandlerFunc(s.requestHandler)
	log.WithFields(log.Fields{
		"endpoint": s.Addr,
	}).Info("starting rpc-server")

	errChan <- s.ListenAndServe()
}

// Shutdown overrides the http.Server Shutdown
// method.
func (s *Server) Shutdown() error {
	log.WithFields(log.Fields{
		"endpoint": s.Addr,
	}).Info("shutting down rpc-server")
	return s.Server.Shutdown(context.Background())
}

func (s *Server) requestHandler(w http.ResponseWriter, httpRequest *http.Request) {
	req := NewRequest(s.config.EnableCORSWorkaround)

	if httpRequest.Method != "POST" {
		req.WriteErrorResponse(
			w,
			NewInvalidParamsError(
				fmt.Sprintf("Invalid method '%s', please retry with 'POST'", httpRequest.Method), nil,
			),
		)
		return
	}

	err := req.DecodeData(httpRequest.Body)
	if err != nil {
		req.WriteErrorResponse(w, NewParseError("Problem parsing JSON-RPC request body", err))
		return
	}

	reqParams, err := req.Params()
	if err != nil {
		req.WriteErrorResponse(w, NewInvalidParamsError("Problem parsing request parameters", err))
		return
	}

	s.methodHandler(w, req, *reqParams)
}

func (s *Server) methodHandler(w http.ResponseWriter, req *Request, reqParams Params) {
	log.WithFields(log.Fields{
		"method": req.Method,
		"params": fmt.Sprintf("%v", reqParams),
	}).Info("processing rpc request")

	var (
		results    interface{}
		resultsErr error
	)

Methods:
	switch req.Method {
	case "getbestblockhash":
		getbestblockhashCalled.Inc()
		results = "0x" + s.chain.CurrentBlockHash().ReverseString()

	case "getblock":
		getbestblockCalled.Inc()
		var hash util.Uint256

		param, err := reqParams.Value(0)
		if err != nil {
			resultsErr = err
			break Methods
		}

		switch param.Type {
		case "string":
			hash, err = util.Uint256DecodeReverseString(param.StringVal)
			if err != nil {
				resultsErr = errInvalidParams
				break Methods
			}
		case "number":
			if !s.validBlockHeight(param) {
				resultsErr = errInvalidParams
				break Methods
			}

			hash = s.chain.GetHeaderHash(param.IntVal)
		case "default":
			resultsErr = errInvalidParams
			break Methods
		}

		block, err := s.chain.GetBlock(hash)
		if err != nil {
			resultsErr = NewInternalServerError(fmt.Sprintf("Problem locating block with hash: %s", hash), err)
			break
		}

		results = wrappers.NewBlock(block, s.chain)
	case "getblockcount":
		getblockcountCalled.Inc()
		results = s.chain.BlockHeight() + 1

	case "getblockhash":
		getblockHashCalled.Inc()
		param, err := reqParams.ValueWithType(0, "number")
		if err != nil {
			resultsErr = err
			break Methods
		} else if !s.validBlockHeight(param) {
			resultsErr = invalidBlockHeightError(0, param.IntVal)
			break Methods
		}

		results = s.chain.GetHeaderHash(param.IntVal)

	case "getconnectioncount":
		getconnectioncountCalled.Inc()
		results = s.coreServer.PeerCount()

	case "getversion":
		getversionCalled.Inc()
		results = result.Version{
			Port:      s.coreServer.Port,
			Nonce:     s.coreServer.ID(),
			UserAgent: s.coreServer.UserAgent,
		}

	case "getpeers":
		getpeersCalled.Inc()
		peers := result.NewPeers()
		for _, addr := range s.coreServer.UnconnectedPeers() {
			peers.AddPeer("unconnected", addr)
		}

		for _, addr := range s.coreServer.BadPeers() {
			peers.AddPeer("bad", addr)
		}

		for addr := range s.coreServer.Peers() {
			peers.AddPeer("connected", addr.PeerAddr().String())
		}

		results = peers

	case "validateaddress":
		validateaddressCalled.Inc()
		param, err := reqParams.Value(0)
		if err != nil {
			resultsErr = err
			break Methods
		}
		results = wrappers.ValidateAddress(param.RawValue)

	case "getassetstate":
		getassetstateCalled.Inc()
		param, err := reqParams.ValueWithType(0, "string")
		if err != nil {
			resultsErr = err
			break Methods
		}

		paramAssetID, err := util.Uint256DecodeReverseString(param.StringVal)
		if err != nil {
			resultsErr = errInvalidParams
			break
		}

		as := s.chain.GetAssetState(paramAssetID)
		if as != nil {
			results = wrappers.NewAssetState(as)
		} else {
			results = "Invalid assetid"
		}

	case "getaccountstate":
		getaccountstateCalled.Inc()
		param, err := reqParams.ValueWithType(0, "string")
		if err != nil {
			resultsErr = err
		} else if scriptHash, err := crypto.Uint160DecodeAddress(param.StringVal); err != nil {
			resultsErr = errInvalidParams
		} else if as := s.chain.GetAccountState(scriptHash); as != nil {
			results = wrappers.NewAccountState(as)
		} else {
			results = "Invalid public account address"
		}
	case "getrawtransaction":
		getrawtransactionCalled.Inc()
		results, resultsErr = s.getrawtransaction(reqParams)

	case "invokescript":
		results, resultsErr = s.invokescript(reqParams)

	case "sendrawtransaction":
		sendrawtransactionCalled.Inc()
		results, resultsErr = s.sendrawtransaction(reqParams)

	default:
		resultsErr = NewMethodNotFoundError(fmt.Sprintf("Method '%s' not supported", req.Method), nil)
	}

	if resultsErr != nil {
		req.WriteErrorResponse(w, resultsErr)
		return
	}

	req.WriteResponse(w, results)
}

func (s *Server) getrawtransaction(reqParams Params) (interface{}, error) {
	var resultsErr error
	var results interface{}

	param0, err := reqParams.ValueWithType(0, "string")
	if err != nil {
		resultsErr = err
	} else if txHash, err := util.Uint256DecodeReverseString(param0.StringVal); err != nil {
		resultsErr = errInvalidParams
	} else if tx, height, err := s.chain.GetTransaction(txHash); err != nil {
		err = errors.Wrapf(err, "Invalid transaction hash: %s", txHash)
		resultsErr = NewInvalidParamsError(err.Error(), err)
	} else if len(reqParams) >= 2 {
		_header := s.chain.GetHeaderHash(int(height))
		header, err := s.chain.GetHeader(_header)
		if err != nil {
			resultsErr = NewInvalidParamsError(err.Error(), err)
		}

		param1, _ := reqParams.ValueAt(1)
		switch v := param1.RawValue.(type) {

		case int, float64, bool, string:
			if v == 0 || v == "0" || v == 0.0 || v == false || v == "false" {
				results = hex.EncodeToString(tx.Bytes())
			} else {
				results = wrappers.NewTransactionOutputRaw(tx, header, s.chain)
			}
		default:
			results = wrappers.NewTransactionOutputRaw(tx, header, s.chain)
		}
	} else {
		results = hex.EncodeToString(tx.Bytes())
	}

	return results, resultsErr
}

// invokescript implements the `invokescript` RPC call.
func (s *Server) invokescript(reqParams Params) (interface{}, error) {
	hexScript, err := reqParams.ValueWithType(0, "string")
	if err != nil {
		return nil, err
	}
	script, err := hex.DecodeString(hexScript.StringVal)
	if err != nil {
		return nil, err
	}
	vm, _ := s.chain.GetTestVM()
	vm.LoadScript(script)
	_ = vm.Run()
	result := &wrappers.InvokeResult{
		State:       vm.State(),
		GasConsumed: "0.1",
		Script:      hexScript.StringVal,
		Stack:       vm.Estack(),
	}
	return result, nil
}

func (s *Server) sendrawtransaction(reqParams Params) (interface{}, error) {
	var resultsErr error
	var results interface{}

	param, err := reqParams.ValueWithType(0, "string")
	if err != nil {
		resultsErr = err
	} else if byteTx, err := hex.DecodeString(param.StringVal); err != nil {
		resultsErr = errInvalidParams
	} else {
		r := io.NewBinReaderFromBuf(byteTx)
		tx := &transaction.Transaction{}
		tx.DecodeBinary(r)
		if r.Err != nil {
			err = errors.Wrap(r.Err, "transaction DecodeBinary failed")
		} else {
			relayReason := s.coreServer.RelayTxn(tx)
			switch relayReason {
			case network.RelaySucceed:
				results = true
			case network.RelayAlreadyExists:
				err = errors.New("block or transaction already exists and cannot be sent repeatedly")
			case network.RelayOutOfMemory:
				err = errors.New("the memory pool is full and no more transactions can be sent")
			case network.RelayUnableToVerify:
				err = errors.New("the block cannot be validated")
			case network.RelayInvalid:
				err = errors.New("block or transaction validation failed")
			case network.RelayPolicyFail:
				err = errors.New("one of the Policy filters failed")
			default:
				err = errors.New("unknown error")
			}
		}
		if err != nil {
			resultsErr = NewInternalServerError(err.Error(), err)
		}
	}

	return results, resultsErr
}

func (s Server) validBlockHeight(param *Param) bool {
	return param.IntVal >= 0 && param.IntVal <= int(s.chain.BlockHeight())
}
