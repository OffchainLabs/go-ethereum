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
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/downloader"
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

func TestSimpleSync(t *testing.T) {
	const numBlocks = 100
	const oldBlock = 20

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

	// key for onchain user
	testUser, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal("generate key err:", err)
	}
	testUserAddress := crypto.PubkeyToAddress(testUser.PublicKey)

	sourceStackConf := node.DefaultConfig
	sourceStackConf.DataDir = ""
	sourceStackConf.P2P.NoDiscovery = true
	sourceStackConf.P2P.ListenAddr = "127.0.0.1:0"
	sourceStackConf.P2P.PrivateKey = sourceKey

	destStackConf := sourceStackConf
	destStackConf.P2P.PrivateKey = destKey

	sourceStack, err := node.New(&sourceStackConf)
	if err != nil {
		t.Fatal(err)
	}

	// create and populate chain
	sourceDb := rawdb.NewMemoryDatabase()
	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  core.GenesisAlloc{testUserAddress: {Balance: new(big.Int).Lsh(big.NewInt(1), 250)}},
	}
	sourceChain, _ := core.NewBlockChain(sourceDb, nil, nil, gspec, nil, ethash.NewFaker(), vm.Config{}, nil, nil)
	signer := types.MakeSigner(sourceChain.Config(), big.NewInt(1), 0)
	_, bs, _ := core.GenerateChainWithGenesis(gspec, ethash.NewFaker(), numBlocks, func(i int, gen *core.BlockGen) {
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
	if _, err := sourceChain.InsertChain(bs); err != nil {
		t.Fatal(err)
	}

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
	destDb := rawdb.NewMemoryDatabase()
	destChain, _ := core.NewBlockChain(destDb, nil, nil, gspec, nil, ethash.NewFaker(), vm.Config{}, nil, nil)
	destStack, err := node.New(&destStackConf)
	if err != nil {
		t.Fatal(err)
	}
	destHandler := NewProtocolHandler(destDb, destChain)
	destStack.RegisterProtocols(destHandler.MakeProtocols(iter))
	destStack.Start()

	// start sync
	log.Info("dest listener", "address", destStack.Server().Config.ListenAddr)
	log.Info("initial source", "head", sourceChain.CurrentBlock())
	log.Info("initial dest", "head", destChain.CurrentBlock())
	err = destHandler.downloader.BeaconSync(downloader.SnapSync, sourceChain.CurrentBlock(), sourceChain.CurrentBlock())
	if err != nil {
		t.Fatal(err)
	}
	<-time.After(time.Second * 5)

	// check sync
	if sourceChain.CurrentBlock().Hash() != destChain.CurrentBlock().Hash() {
		log.Info("final source", "head", sourceChain.CurrentBlock())
		log.Info("final dest", "head", destChain.CurrentBlock())
		t.Fatal("dest chain not synced to source")
	}

	oldDest := destChain.GetHeaderByNumber(oldBlock)
	if oldDest == nil {
		t.Fatal("old dest block nil")
	}
	oldSource := sourceChain.GetHeaderByNumber(oldBlock)
	if oldSource == nil {
		t.Fatal("old source block nil")
	}
	if oldDest.Hash() != oldSource.Hash() {
		log.Info("final source", "old", oldSource)
		log.Info("final dest", "old", oldDest)
		t.Fatal("dest and source differ")
	}
	_, err = sourceChain.StateAt(oldSource.Root)
	if err != nil {
		t.Fatal("source chain does not have state for old block")
	}
	_, err = destChain.StateAt(oldDest.Root)
	if err == nil {
		t.Fatal("dest chain does have state for old block, but should have been snap-synced")
	}
}
