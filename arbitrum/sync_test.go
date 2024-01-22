package arbitrum

import (
	"encoding/hex"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
)

type dummyIterator struct {
	lock  sync.Mutex
	nodes []*enode.Node //first one is never used
}

func (i *dummyIterator) Next() bool { // moves to next node
	i.lock.Lock()
	defer i.lock.Unlock()

	if len(i.nodes) == 0 {
		log.Info("dummy iterator: done")
		return false
	}
	i.nodes = i.nodes[1:]
	return len(i.nodes) > 0
}

func (i *dummyIterator) Node() *enode.Node { // returns current node
	i.lock.Lock()
	defer i.lock.Unlock()
	if len(i.nodes) == 0 {
		return nil
	}
	if i.nodes[0] != nil {
		log.Info("dummy iterator: emit", "id", i.nodes[0].ID(), "ip", i.nodes[0].IP(), "tcp", i.nodes[0].TCP(), "udp", i.nodes[0].UDP())
	}
	return i.nodes[0]
}

func (i *dummyIterator) Close() { // ends the iterator
	i.nodes = nil
}

type dummySyncHelper struct {
	confirmed  *types.Header
	checkpoint *types.Header
}

func (d *dummySyncHelper) LastConfirmed() (*types.Header, uint64, error) {
	return d.confirmed, 0, nil
}

func (d *dummySyncHelper) LastCheckpoint() (*types.Header, error) {
	if d.confirmed == nil {
		return nil, nil
	}
	return d.checkpoint, nil
}

func (d *dummySyncHelper) CheckpointSupported(*types.Header) (bool, error) {
	return true, nil
}

func (d *dummySyncHelper) ValidateConfirmed(header *types.Header, node uint64) (bool, error) {
	if d.confirmed == nil {
		return true, nil
	}
	if header == nil {
		return false, nil
	}
	if d.confirmed.Hash() == header.Hash() {
		return true, nil
	}
	return false, nil
}

func testHasBlock(t *testing.T, chain *core.BlockChain, block *types.Block, shouldHaveState bool) {
	t.Helper()
	hasHeader := chain.GetHeaderByNumber(block.NumberU64())
	if hasHeader == nil {
		t.Fatal("block not found")
	}
	if hasHeader.Hash() != block.Hash() {
		t.Fatal("wrong block in blockchain")
	}
	_, err := chain.StateAt(hasHeader.Root)
	if err != nil && shouldHaveState {
		t.Fatal("should have state, but doesn't")
	}
	if err == nil && !shouldHaveState {
		t.Fatal("should not have state, but does")
	}
}

func portFromAddress(address string) (int, error) {
	splitAddr := strings.Split(address, ":")
	return strconv.Atoi(splitAddr[len(splitAddr)-1])
}

func TestSimpleSync(t *testing.T) {
	const pivotBlockNum = 50
	const syncBlockNum = 70
	const extraBlocks = 200

	glogger := log.NewGlogHandler(log.StreamHandler(os.Stderr, log.TerminalFormat(false)))
	glogger.Verbosity(log.LvlTrace)
	log.Root().SetHandler(glogger)

	// key for source node p2p
	sourceKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal("generate key err:", err)
	}

	// key for dest node p2p
	destKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal("generate key err:", err)
	}

	// key for bad node p2p
	badNodeKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal("generate key err:", err)
	}

	// source node
	sourceStackConf := node.DefaultConfig
	sourceStackConf.DataDir = t.TempDir()
	sourceStackConf.P2P.DiscoveryV4 = false
	sourceStackConf.P2P.DiscoveryV5 = false
	sourceStackConf.P2P.ListenAddr = "127.0.0.1:0"
	sourceStackConf.P2P.PrivateKey = sourceKey

	sourceStack, err := node.New(&sourceStackConf)
	if err != nil {
		t.Fatal(err)
	}
	sourceDb, err := sourceStack.OpenDatabaseWithFreezer("l2chaindata", 2048, 512, "", "", false)
	if err != nil {
		t.Fatal(err)
	}

	// create and populate chain

	// code for contractcodehex below:
	// pragma solidity ^0.8.20;
	//
	// contract Temmp {
	// uint256[0x10000] private store;
	//
	// fallback(bytes calldata data) external payable returns (bytes memory) {
	//     uint16 index = uint16(uint256(bytes32(data[0:32])));
	//     store[index] += 1;
	//     return "";
	// }
	// }
	contractCodeHex := "608060405234801561001057600080fd5b50610218806100206000396000f3fe608060405260003660606000838360009060209261001f9392919061008a565b9061002a91906100e7565b60001c9050600160008261ffff1662010000811061004b5761004a610146565b5b01600082825461005b91906101ae565b9250508190555060405180602001604052806000815250915050915050805190602001f35b600080fd5b600080fd5b6000808585111561009e5761009d610080565b5b838611156100af576100ae610085565b5b6001850283019150848603905094509492505050565b600082905092915050565b6000819050919050565b600082821b905092915050565b60006100f383836100c5565b826100fe81356100d0565b9250602082101561013e576101397fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff836020036008026100da565b831692505b505092915050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052603260045260246000fd5b6000819050919050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052601160045260246000fd5b60006101b982610175565b91506101c483610175565b92508282019050808211156101dc576101db61017f565b5b9291505056fea26469706673582212202777d6cb94519b9aa7026cf6dad162739731e124c6379b15c343ff1c6e84a5f264736f6c63430008150033"
	contractCode, err := hex.DecodeString(contractCodeHex)
	if err != nil {
		t.Fatal("decode contract error:", err)
	}
	testUser, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal("generate key err:", err)
	}
	testUserAddress := crypto.PubkeyToAddress(testUser.PublicKey)

	testUser2, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal("generate key err:", err)
	}
	testUser2Address := crypto.PubkeyToAddress(testUser2.PublicKey)

	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc: core.GenesisAlloc{
			testUserAddress:  {Balance: new(big.Int).Lsh(big.NewInt(1), 250)},
			testUser2Address: {Balance: new(big.Int).Lsh(big.NewInt(1), 250)},
		},
	}
	sourceChain, _ := core.NewBlockChain(sourceDb, nil, nil, gspec, nil, ethash.NewFaker(), vm.Config{}, nil, nil, nil)
	signer := types.MakeSigner(sourceChain.Config(), big.NewInt(1), 0)

	firstAddress := common.Address{}
	_, blocks, allReceipts := core.GenerateChainWithGenesis(gspec, ethash.NewFaker(), syncBlockNum+extraBlocks, func(i int, gen *core.BlockGen) {
		creationNonce := gen.TxNonce(testUser2Address)
		tx, err := types.SignTx(types.NewContractCreation(creationNonce, new(big.Int), 1000000, gen.BaseFee(), contractCode), signer, testUser2)
		if err != nil {
			t.Fatalf("failed to create contract: %v", err)
		}
		gen.AddTx(tx)

		contractAddress := crypto.CreateAddress(testUser2Address, creationNonce)

		nonce := gen.TxNonce(testUserAddress)
		tx, err = types.SignNewTx(testUser, signer, &types.LegacyTx{
			Nonce:    nonce,
			GasPrice: gen.BaseFee(),
			Gas:      uint64(1000001),
		})
		if err != nil {
			t.Fatalf("failed to create tx: %v", err)
		}
		gen.AddTx(tx)

		iterHash := common.BigToHash(big.NewInt(int64(i)))
		tx, err = types.SignNewTx(testUser, signer, &types.LegacyTx{
			To:       &contractAddress,
			Nonce:    nonce + 1,
			GasPrice: gen.BaseFee(),
			Gas:      uint64(1000001),
			Data:     iterHash[:],
		})
		if err != nil {
			t.Fatalf("failed to create tx: %v", err)
		}
		gen.AddTx(tx)

		if firstAddress == (common.Address{}) {
			firstAddress = contractAddress
		}

		tx, err = types.SignNewTx(testUser, signer, &types.LegacyTx{
			To:       &firstAddress,
			Nonce:    nonce + 2,
			GasPrice: gen.BaseFee(),
			Gas:      uint64(1000001),
			Data:     iterHash[:],
		})
		if err != nil {
			t.Fatalf("failed to create tx: %v", err)
		}
		gen.AddTx(tx)
	})

	for _, receipts := range allReceipts {
		if len(receipts) < 3 {
			t.Fatal("missing receipts")
		}
		for _, receipt := range receipts {
			if receipt.Status == 0 {
				t.Fatal("failed transaction")
			}
		}
	}
	pivotBlock := blocks[pivotBlockNum-1]
	syncBlock := blocks[syncBlockNum-1]
	if _, err := sourceChain.InsertChain(blocks[:pivotBlockNum]); err != nil {
		t.Fatal(err)
	}
	sourceChain.TrieDB().Commit(blocks[pivotBlockNum-1].Root(), true)
	if _, err := sourceChain.InsertChain(blocks[pivotBlockNum:]); err != nil {
		t.Fatal(err)
	}

	// should have state of pivot but nothing around
	testHasBlock(t, sourceChain, blocks[pivotBlockNum-2], false)
	testHasBlock(t, sourceChain, blocks[pivotBlockNum-1], true)
	testHasBlock(t, sourceChain, blocks[pivotBlockNum], false)

	// source node
	sourceHandler := NewProtocolHandler(sourceDb, sourceChain, &dummySyncHelper{syncBlock.Header(), pivotBlock.Header()}, false)
	sourceStack.RegisterProtocols(sourceHandler.MakeProtocols(&dummyIterator{}))
	if err := sourceStack.Start(); err != nil {
		t.Fatal(err)
	}

	// bad node (on wrong blockchain)
	_, badBlocks, _ := core.GenerateChainWithGenesis(gspec, ethash.NewFaker(), syncBlockNum+extraBlocks, func(i int, gen *core.BlockGen) {
		creationNonce := gen.TxNonce(testUser2Address)
		tx, err := types.SignTx(types.NewContractCreation(creationNonce, new(big.Int), 1000000, gen.BaseFee(), contractCode), signer, testUser2)
		if err != nil {
			t.Fatalf("failed to create contract: %v", err)
		}
		gen.AddTx(tx)
	})
	badStackConf := sourceStackConf
	badStackConf.DataDir = t.TempDir()
	badStackConf.P2P.PrivateKey = badNodeKey
	badStack, err := node.New(&badStackConf)
	if err != nil {
		t.Fatal(err)
	}

	badDb, err := badStack.OpenDatabaseWithFreezer("l2chaindata", 2048, 512, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	badChain, _ := core.NewBlockChain(badDb, nil, nil, gspec, nil, ethash.NewFaker(), vm.Config{}, nil, nil, nil)
	if _, err := badChain.InsertChain(badBlocks[:pivotBlockNum]); err != nil {
		t.Fatal(err)
	}
	badChain.TrieDB().Commit(badBlocks[pivotBlockNum-1].Root(), true)
	if _, err := badChain.InsertChain(badBlocks[pivotBlockNum:]); err != nil {
		t.Fatal(err)
	}
	badHandler := NewProtocolHandler(badDb, badChain, &dummySyncHelper{blocks[syncBlockNum-1].Header(), badBlocks[pivotBlockNum-1].Header()}, false)
	badStack.RegisterProtocols(badHandler.MakeProtocols(&dummyIterator{}))
	if err := badStack.Start(); err != nil {
		t.Fatal(err)
	}

	// figure out port of the source node and create dummy iter that points to it
	sourcePort, err := portFromAddress(sourceStack.Server().Config.ListenAddr)
	if err != nil {
		t.Fatal(err)
	}
	badNodePort, err := portFromAddress(badStack.Server().Config.ListenAddr)
	if err != nil {
		t.Fatal(err)
	}
	badEnode := enode.NewV4(&badNodeKey.PublicKey, net.IPv4(127, 0, 0, 1), badNodePort, 0)
	sourceEnode := enode.NewV4(&sourceKey.PublicKey, net.IPv4(127, 0, 0, 1), sourcePort, 0)
	iter := &dummyIterator{
		nodes: []*enode.Node{nil, badEnode, sourceEnode},
	}

	// dest node
	destStackConf := sourceStackConf
	destStackConf.DataDir = t.TempDir()
	destStackConf.P2P.PrivateKey = destKey
	destStack, err := node.New(&destStackConf)
	if err != nil {
		t.Fatal(err)
	}

	destDb, err := destStack.OpenDatabaseWithFreezer("l2chaindata", 2048, 512, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	destChain, _ := core.NewBlockChain(destDb, nil, nil, gspec, nil, ethash.NewFaker(), vm.Config{}, nil, nil, nil)
	destHandler := NewProtocolHandler(destDb, destChain, &dummySyncHelper{syncBlock.Header(), nil}, true)
	destStack.RegisterProtocols(destHandler.MakeProtocols(iter))

	// start sync
	log.Info("dest listener", "address", destStack.Server().Config.ListenAddr)
	log.Info("initial source", "head", sourceChain.CurrentBlock())
	log.Info("initial dest", "head", destChain.CurrentBlock())
	log.Info("pivot", "head", pivotBlock.Header())
	if err := destStack.Start(); err != nil {
		t.Fatal(err)
	}

	<-time.After(time.Second * 5)

	log.Info("final source", "head", sourceChain.CurrentBlock())
	log.Info("final dest", "head", destChain.CurrentBlock())
	log.Info("sync block", "header", syncBlock.Header())

	// check sync
	if destChain.CurrentBlock().Number.Cmp(syncBlock.Number()) != 0 {
		t.Fatal("did not sync to sync block")
	}

	testHasBlock(t, destChain, syncBlock, true)
	testHasBlock(t, destChain, pivotBlock, true)
	testHasBlock(t, destChain, blocks[pivotBlockNum-2], false)
}
