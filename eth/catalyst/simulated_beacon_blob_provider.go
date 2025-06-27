package catalyst

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
)

// GetBlobs is a mimic of blobSidecars function (https://github.com/OffchainLabs/nitro/blob/a4b72abd63df46de920580685842491d550dfc20/util/headerreader/blob_client.go#L199)
// in nitro, a mel related system test uses simulated beacon as blob reader, if that were to fail we need to update this function to be similar to the above referenced function
func (c *SimulatedBeacon) GetBlobs(ctx context.Context, batchBlockHash common.Hash, versionedHashes []common.Hash) ([]kzg4844.Blob, error) {
	bundle, ok := c.blobsBundleProvider[batchBlockHash]
	if !ok {
		return nil, errors.New("blobs not found")
	}

	if len(bundle.Blobs) < len(versionedHashes) {
		return nil, fmt.Errorf("expected at least %d blobs but only got %d", len(versionedHashes), len(bundle.Blobs))
	}

	output := make([]kzg4844.Blob, len(versionedHashes))
	outputsFound := make([]bool, len(versionedHashes))

	for i := 0; i < len(bundle.Blobs); i++ {
		var commitment kzg4844.Commitment
		copy(commitment[:], bundle.Commitments[i])
		versionedHash := commitmentToVersionedHash(commitment)

		// The versioned hashes of the blob commitments are produced in the by HASH_OPCODE_BYTE,
		// presumably in the order they were added to the tx. The spec is unclear if the blobs
		// need to be returned in any particular order from the beacon API, so we put them back in
		// the order from the tx.
		var outputIdx int
		var found bool
		for outputIdx = range versionedHashes {
			if versionedHashes[outputIdx] == versionedHash {
				if outputsFound[outputIdx] {
					// Duplicate, skip this one
					break
				}
				found = true
				outputsFound[outputIdx] = true
				break
			}
		}
		if !found {
			continue
		}

		copy(output[outputIdx][:], bundle.Blobs[i])

		var proof kzg4844.Proof
		copy(proof[:], bundle.Proofs[i])

		if err := kzg4844.VerifyBlobProof(&output[outputIdx], commitment, proof); err != nil {
			return nil, fmt.Errorf("failed to verify blob proof for blob index(%d), blob(%s)", i, firstFewChars(bundle.Blobs[i].String()))
		}
	}

	for i, found := range outputsFound {
		if !found {
			return nil, fmt.Errorf("missing blob %v, can't reconstruct batch payload", versionedHashes[i])
		}
	}

	return output, nil
}

func (c *SimulatedBeacon) Initialize(ctx context.Context) error {
	c.blobsBundleProvider = make(map[common.Hash]*engine.BlobsBundleV1)
	return nil
}

func commitmentToVersionedHash(commitment kzg4844.Commitment) common.Hash {
	// As per the EIP-4844 spec, the versioned hash is the SHA-256 hash of the commitment with the first byte set to 1.
	hash := sha256.Sum256(commitment[:])
	hash[0] = 1
	return hash
}

func firstFewChars(s string) string {
	if len(s) < 9 {
		return fmt.Sprintf("\"%s\"", s)
	} else {
		return fmt.Sprintf("\"%s...\"", s[:8])
	}
}
