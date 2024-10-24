package util

import (
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestAccountsTextHash(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "Example message",
			input:    []byte("foo bar"),
			expected: "475d68f61c0282c2e194d37d855756f36f73a3f61d1ebbe1eaa42df9e2fe6934",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultBytes := accounts.TextHash(tt.input)
			resultHash := common.BytesToHash(resultBytes)

			assert.Equal(t, common.HexToHash(tt.expected), resultHash, "Hash mismatch")

			// Additional check: verify the length of the hash
			assert.Equal(t, 32, len(resultHash), "Hash length should be 32 bytes")

			// Print the hash in hexadecimal format for visual inspection
			t.Logf("Input: %s", string(tt.input))
			t.Logf("Resulting hash: 0x%s", hex.EncodeToString(resultHash.Bytes()))
		})
	}
}
