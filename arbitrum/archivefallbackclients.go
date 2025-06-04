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

func newArchiveFallbackClientsManager(archiveRedirects []BlockRedirectConfig) (*archiveFallbackClientsManager, error) {
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
		return manager.lastBlockAndClients[i].lastBlock < manager.lastBlockAndClients[j].lastBlock
	})
	return manager, nil
}

func (a *archiveFallbackClientsManager) lastAvailableBlock() uint64 {
	return a.lastBlockAndClients[len(a.lastBlockAndClients)-1].lastBlock
}

func (a *archiveFallbackClientsManager) fallbackClient(blockNum uint64) types.FallbackClient {
	var possibleClients []types.FallbackClient
	var chosenLastBlock uint64
	for _, lastBlockAndClient := range a.lastBlockAndClients {
		if chosenLastBlock == 0 && blockNum <= lastBlockAndClient.lastBlock {
			chosenLastBlock = lastBlockAndClient.lastBlock
		}
		if chosenLastBlock != 0 {
			if lastBlockAndClient.lastBlock == chosenLastBlock {
				possibleClients = append(possibleClients, lastBlockAndClient.client)
			} else {
				break
			}
		}
	}
	if len(possibleClients) != 0 {
		return possibleClients[rand.Intn(len(possibleClients))]
	}
	return nil
}
