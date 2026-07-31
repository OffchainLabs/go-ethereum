// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package trie

import (
	"github.com/ethereum/go-ethereum/common"
)

func ForEachHashChild(path []byte, blob []byte, onChild func(childPath []byte, childHash common.Hash)) error {
	n, err := decodeNode(nil, blob)
	if err != nil {
		return err
	}
	forEachHashChild(path, n, onChild)
	return nil
}

func forEachHashChild(path []byte, n node, onChild func([]byte, common.Hash)) {
	switch n := n.(type) {
	case *shortNode:
		forEachHashChild(append(append([]byte(nil), path...), n.Key...), n.Val, onChild)
	case *fullNode:
		for i := byte(0); i < 16; i++ {
			if child := n.Children[i]; child != nil {
				forEachHashChild(append(append([]byte(nil), path...), i), child, onChild)
			}
		}
	case hashNode:
		onChild(path, common.BytesToHash(n))
	}
}
