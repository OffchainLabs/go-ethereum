package types

import (
	"bytes"
	"io"

	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/rlp"
)

type BlobTxWithBlobs struct {
	*Transaction
	Blobs       []kzg4844.Blob
	Commitments []kzg4844.Commitment
	Proofs      []kzg4844.Proof
}

func NewBlobTxWithBlobs(tx *Transaction, blobs []kzg4844.Blob, commitments []kzg4844.Commitment, proofs []kzg4844.Proof) *BlobTxWithBlobs {
	if tx == nil {
		return nil
	}
	return &BlobTxWithBlobs{
		Transaction: tx,
		Blobs:       blobs,
		Commitments: commitments,
		Proofs:      proofs,
	}
}

type innerType struct {
	BlobTx      *BlobTx
	Blobs       []kzg4844.Blob
	Commitments []kzg4844.Commitment
	Proofs      []kzg4844.Proof
}

func (tx *BlobTxWithBlobs) DecodeRLP(s *rlp.Stream) error {
	tx.Transaction = new(Transaction)
	kind, _, err := s.Kind()
	switch {
	case err != nil:
		return err
	case kind == rlp.List:
		return tx.Transaction.DecodeRLP(s)
	default:
		b, err := s.Bytes()
		if err != nil {
			return err
		}
		return tx.UnmarshalBinary(b)
	}
}

func (tx *BlobTxWithBlobs) EncodeRLP(w io.Writer) error {
	blobTx, ok := tx.Transaction.inner.(*BlobTx)
	if !ok {
		// For non-blob transactions, the encoding is just the transaction.
		return tx.Transaction.EncodeRLP(w)
	}

	// For blob transactions, the encoding is the transaction together with the blobs.
	// Use temporary buffer from pool.
	buf := encodeBufferPool.Get().(*bytes.Buffer)
	defer encodeBufferPool.Put(buf)
	buf.Reset()

	buf.WriteByte(BlobTxType)
	innerValue := &innerType{
		BlobTx:      blobTx,
		Blobs:       tx.Blobs,
		Commitments: tx.Commitments,
		Proofs:      tx.Proofs,
	}
	err := rlp.Encode(buf, innerValue)
	if err != nil {
		return err
	}
	return rlp.Encode(w, buf.Bytes())
}

func (tx *BlobTxWithBlobs) UnmarshalBinary(b []byte) error {
	tx.Transaction = new(Transaction)
	if len(b) < 1 {
		return errShortTypedTx
	}
	if b[0] == BlobTxType {
		var blobTypedTx innerType
		if err := rlp.DecodeBytes(b[1:], &blobTypedTx); err != nil {
			return err
		}
		tx.Transaction = NewTx(blobTypedTx.BlobTx)
		tx.Blobs = blobTypedTx.Blobs
		tx.Commitments = blobTypedTx.Commitments
		tx.Proofs = blobTypedTx.Proofs
		return nil
	}
	return tx.Transaction.UnmarshalBinary(b)
}

func (tx *BlobTxWithBlobs) MarshalBinary() ([]byte, error) {
	blobTx, ok := tx.Transaction.inner.(*BlobTx)
	if !ok {
		// For non-blob transactions, the encoding is just the transaction.
		return tx.Transaction.MarshalBinary()
	}
	var buf bytes.Buffer
	buf.WriteByte(BlobTxType)
	innerValue := &innerType{
		BlobTx:      blobTx,
		Blobs:       tx.Blobs,
		Commitments: tx.Commitments,
		Proofs:      tx.Proofs,
	}
	err := rlp.Encode(&buf, innerValue)
	return buf.Bytes(), err
}
