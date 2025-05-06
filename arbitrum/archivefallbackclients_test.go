package arbitrum

import (
	"sort"
	"testing"
)

func TestArchiveFallbackClientsManager(t *testing.T) {
	manager := archiveFallbackClientsManager{}
	lastBlocks := []uint64{100, 150, 200, 250, 500}
	for i := 0; i < 5; i++ {
		manager.lastBlockAndClients = append(manager.lastBlockAndClients, &lastBlockAndClient{lastBlock: lastBlocks[i]})
	}
	sort.Slice(manager.lastBlockAndClients, func(i, j int) bool {
		return manager.lastBlockAndClients[i].lastBlock > manager.lastBlockAndClients[j].lastBlock
	})

	lastAvailableBlock := manager.lastAvailableBlock()
	if lastAvailableBlock != lastBlocks[len(lastBlocks)-1] {
		t.Fatalf("unexpected lastAvailableBlock. Want: %d, Got: %d", lastBlocks[len(lastBlocks)-1], lastAvailableBlock)
	}

	// Clients are sorted in the descending order of their last-block values
	// Every client should be in the valid pool
	pos := manager.validPool(100)
	if pos != 4 {
		t.Fatalf("unexpected number of clients in valid pool. Want: 4, Got: %d", pos)
	}

	// Last client wont be in the valid pool
	pos = manager.validPool(140)
	if pos != 3 {
		t.Fatalf("unexpected number of clients in valid pool. Want: 3, Got: %d", pos)
	}

	// Only first two will be in the valid pool
	pos = manager.validPool(240)
	if pos != 1 {
		t.Fatalf("unexpected number of clients in valid pool. Want: 1, Got: %d", pos)
	}

	// Only first will be in the valid pool
	pos = manager.validPool(440)
	if pos != 0 {
		t.Fatalf("unexpected number of clients in valid pool. Want: 0, Got: %d", pos)
	}

	// No client is in the valid pool
	pos = manager.validPool(540)
	if pos != -1 {
		t.Fatalf("unexpected number of clients in valid pool. Want: -1, Got: %d", pos)
	}
}
