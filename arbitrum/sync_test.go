package arbitrum

import (
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestSimpleSync(t *testing.T) {
	const pivotBlockNum = 50
	const syncBlockNum = 70
	const extraBlocks = 200

	glogger := log.NewGlogHandler(log.StreamHandler(os.Stderr, log.TerminalFormat(false)))
	glogger.Verbosity(log.Lvl(log.LvlTrace))
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

	// source node
	sourceStackConf := node.DefaultConfig
	sourceStackConf.DataDir = t.TempDir()
	sourceStackConf.P2P.NoDiscovery = true
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
	testUser, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal("generate key err:", err)
	}
	testUserAddress := crypto.PubkeyToAddress(testUser.PublicKey)
	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  core.GenesisAlloc{testUserAddress: {Balance: new(big.Int).Lsh(big.NewInt(1), 250)}},
	}
	sourceChain, _ := core.NewBlockChain(sourceDb, nil, nil, gspec, nil, ethash.NewFaker(), vm.Config{}, nil, nil)
	signer := types.MakeSigner(sourceChain.Config(), big.NewInt(1), 0)

	_, blocks, _ := core.GenerateChainWithGenesis(gspec, ethash.NewFaker(), syncBlockNum+extraBlocks, func(i int, gen *core.BlockGen) {
		tx, err := types.SignNewTx(testUser, signer, &types.LegacyTx{
			Nonce:    gen.TxNonce(testUserAddress),
			GasPrice: gen.BaseFee(),
			Gas:      uint64(1000001),
		})
		if err != nil {
			t.Fatalf("failed to create tx: %v", err)
		}
		gen.AddTx(tx)
	})

	if _, err := sourceChain.InsertChain(blocks[:pivotBlockNum]); err != nil {
		t.Fatal(err)
	}

	pivotBlock := blocks[pivotBlockNum-1]
	syncBlock := blocks[syncBlockNum-1]

	testHasBlock(t, sourceChain, pivotBlock, true)
	sourceChain.TrieDB().Commit(pivotBlock.Root(), true)

	if _, err := sourceChain.InsertChain(blocks[pivotBlockNum:]); err != nil {
		t.Fatal(err)
	}

	// should have state of pivot but nothing around
	testHasBlock(t, sourceChain, blocks[pivotBlockNum-2], false)
	testHasBlock(t, sourceChain, blocks[pivotBlockNum-1], true)
	testHasBlock(t, sourceChain, blocks[pivotBlockNum], false)

	// source node
	sourceHandler := NewProtocolHandler(sourceDb, sourceChain)
	sourceStack.RegisterProtocols(sourceHandler.MakeProtocols(&dummyIterator{}))
	sourceStack.Start()

	// figure out port of the source node and create dummy iter that points to it
	sourceListenAddr := sourceStack.Server().Config.ListenAddr
	splitAddr := strings.Split(sourceListenAddr, ":")
	sourcePort, err := strconv.Atoi(splitAddr[len(splitAddr)-1])
	if err != nil {
		t.Fatal(err)
	}
	sourceEnode := enode.NewV4(&sourceKey.PublicKey, net.IPv4(127, 0, 0, 1), sourcePort, 0)
	iter := &dummyIterator{
		nodes: []*enode.Node{nil, sourceEnode},
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
	destChain, _ := core.NewBlockChain(destDb, nil, nil, gspec, nil, ethash.NewFaker(), vm.Config{}, nil, nil)
	destHandler := NewProtocolHandler(destDb, destChain)
	destStack.RegisterProtocols(destHandler.MakeProtocols(iter))
	destStack.Start()

	// start sync
	log.Info("dest listener", "address", destStack.Server().Config.ListenAddr)
	log.Info("initial source", "head", sourceChain.CurrentBlock())
	log.Info("initial dest", "head", destChain.CurrentBlock())
	log.Info("pivot", "head", pivotBlock.Header())
	err = destHandler.downloader.PivotSync(syncBlock.Header(), pivotBlock.Header())
	if err != nil {
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
