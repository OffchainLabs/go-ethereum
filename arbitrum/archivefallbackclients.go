package arbitrum

import (
	"math/rand"
	"sort"

	"github.com/ethereum/go-ethereum/core/types"
)

type lastBlockAndClient struct {
	lastBlock uint64
	client    types.FallbackClient
}

type archiveFallbackClientsManager struct {
	lastBlockAndClients []*lastBlockAndClient
}

func newArchiveFallbackClientsManager(archiveRedirects []ArchiveRedirectConfig) (*archiveFallbackClientsManager, error) {
	manager := &archiveFallbackClientsManager{}
	for _, archiveConfig := range archiveRedirects {
		fallbackClient, err := CreateFallbackClient(archiveConfig.URL, archiveConfig.Timeout, true)
		if err != nil {
			return nil, err
		}
		if fallbackClient == nil {
			continue
		}
		manager.lastBlockAndClients = append(manager.lastBlockAndClients, &lastBlockAndClient{
			lastBlock: archiveConfig.LastBlock,
			client:    fallbackClient,
		})
	}
	if len(manager.lastBlockAndClients) == 0 {
		return nil, nil
	}
	sort.Slice(manager.lastBlockAndClients, func(i, j int) bool {
		return manager.lastBlockAndClients[i].lastBlock > manager.lastBlockAndClients[j].lastBlock
	})
	return manager, nil
}

func (a *archiveFallbackClientsManager) lastAvailableBlock() uint64 {
	return a.lastBlockAndClients[0].lastBlock
}

func (a *archiveFallbackClientsManager) validPool(blockNum uint64) int {
	// First find which clients have this block using binary search
	s, f := 0, len(a.lastBlockAndClients)-1
	for s < f {
		m := (s + f + 1) / 2
		if blockNum <= a.lastBlockAndClients[m].lastBlock {
			s = m
		} else {
			f = m - 1
		}
	}
	if blockNum > a.lastBlockAndClients[s].lastBlock {
		return -1
	}
	return s
}

func (a *archiveFallbackClientsManager) fallbackClient(blockNum uint64) types.FallbackClient {
	pos := a.validPool(blockNum)
	if pos == -1 {
		return nil
	}
	// Pick a random client form the pool
	return a.lastBlockAndClients[rand.Intn(pos+1)].client
}
