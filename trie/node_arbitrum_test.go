// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package trie

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

type hashChild struct {
	path []byte
	hash common.Hash
}

func collectHashChildren(t *testing.T, path []byte, blob []byte) []hashChild {
	t.Helper()
	var children []hashChild
	if err := ForEachHashChild(path, blob, func(childPath []byte, childHash common.Hash) {
		children = append(children, hashChild{path: childPath, hash: childHash})
	}); err != nil {
		t.Fatal(err)
	}
	return children
}

func testChildHash(b byte) common.Hash {
	return common.BytesToHash(bytes.Repeat([]byte{b}, common.HashLength))
}

func TestForEachHashChildFullNode(t *testing.T) {
	n := &fullNode{}
	n.Children[3] = hashNode(testChildHash(1).Bytes())
	n.Children[10] = hashNode(testChildHash(2).Bytes())
	// An embedded leaf must not be reported.
	n.Children[7] = &shortNode{Key: hexToCompact([]byte{5, 16}), Val: valueNode([]byte{0xaa})}

	children := collectHashChildren(t, []byte{1, 2}, nodeToBytes(n))
	want := []hashChild{
		{path: []byte{1, 2, 3}, hash: testChildHash(1)},
		{path: []byte{1, 2, 10}, hash: testChildHash(2)},
	}
	if !reflect.DeepEqual(children, want) {
		t.Fatalf("unexpected children: got %v want %v", children, want)
	}
}

func TestForEachHashChildExtensionNode(t *testing.T) {
	n := &shortNode{
		Key: hexToCompact([]byte{4, 5, 6}),
		Val: hashNode(testChildHash(3).Bytes()),
	}
	children := collectHashChildren(t, []byte{9}, nodeToBytes(n))
	want := []hashChild{{path: []byte{9, 4, 5, 6}, hash: testChildHash(3)}}
	if !reflect.DeepEqual(children, want) {
		t.Fatalf("unexpected children: got %v want %v", children, want)
	}
}

func TestForEachHashChildLeafNode(t *testing.T) {
	n := &shortNode{Key: hexToCompact([]byte{1, 2, 16}), Val: valueNode([]byte{0xbb})}
	if children := collectHashChildren(t, nil, nodeToBytes(n)); len(children) != 0 {
		t.Fatalf("expected no hash children for a leaf, got %v", children)
	}
}

func TestForEachHashChildMalformedNode(t *testing.T) {
	if err := ForEachHashChild(nil, []byte{0xde, 0xad, 0xbe, 0xef}, func([]byte, common.Hash) {
		t.Fatal("callback invoked for malformed node")
	}); err == nil {
		t.Fatal("expected error for malformed node")
	}
}

func TestForEachHashChildCommittedTrie(t *testing.T) {
	tr := NewEmpty(newTestDatabase(rawdb.NewMemoryDatabase(), rawdb.HashScheme))
	val := bytes.Repeat([]byte{0xcc}, 64) // large values force hash references
	for i := byte(0); i < 32; i++ {
		tr.MustUpdate([]byte{i, i + 1, i + 2}, append(val, i))
	}
	rootHash, nodes := tr.Commit(false)

	blobs := make(map[common.Hash][]byte)
	paths := make(map[common.Hash][]byte)
	for path, n := range nodes.Nodes {
		blobs[n.Hash] = n.Blob
		paths[n.Hash] = []byte(path)
	}

	visited := map[common.Hash]bool{rootHash: true}
	queue := []common.Hash{rootHash}
	for len(queue) > 0 {
		hash := queue[0]
		queue = queue[1:]
		if err := ForEachHashChild(paths[hash], blobs[hash], func(childPath []byte, childHash common.Hash) {
			blob, ok := blobs[childHash]
			if !ok || blob == nil {
				t.Fatalf("reported child %s not in committed node set", childHash)
			}
			if !bytes.Equal(paths[childHash], childPath) {
				t.Fatalf("reported child path %v, committed at %v", childPath, paths[childHash])
			}
			if !visited[childHash] {
				visited[childHash] = true
				queue = append(queue, childHash)
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(visited) != len(blobs) {
		t.Fatalf("walk visited %d of %d committed nodes", len(visited), len(blobs))
	}
}
