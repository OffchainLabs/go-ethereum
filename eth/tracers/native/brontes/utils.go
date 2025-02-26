package brontes

import (
	"encoding/hex"
	"fmt"
)

type TraceStyle int

const (
	TraceStyleParity = iota
	TraceStyleGeth
)

func (ts TraceStyle) IsParity() bool {
	return ts == TraceStyleParity
}

// maybeRevertReason is a dummy helper that returns a revert reason if available.
func maybeRevertReason(data []byte) *string {
	if len(data) == 0 {
		return nil
	}
	// In a real implementation, decode the revert reason from data.
	reason := "revert"
	return &reason
}

// convertMemory converts a []byte into 32‐byte hex string chunks.
func convertMemory(mem []byte) []string {
	const chunkSize = 32
	var chunks []string
	for i := 0; i < len(mem); i += chunkSize {
		end := i + chunkSize
		if end > len(mem) {
			end = len(mem)
		}
		chunk := mem[i:end]
		chunks = append(chunks, fmt.Sprintf("0x%s", hex.EncodeToString(chunk)))
	}
	return chunks
}
