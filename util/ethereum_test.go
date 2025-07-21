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

func TestRecoverAddressFromSignature(t *testing.T) {
	tests := []struct {
		name      string
		node_id   string
		msg       string
		signature string
	}{
		{
			name:      "Example signature",
			node_id:   "0xEA28d002042fd9898D0Db016be9758eeAFE35C1E",
			msg:       "Rescue Node 1753055877",
			signature: "3b1ffab4b818e917b81a878265061660c05a5d9eb55bda623b184d1d8f0af9af58c366d487652a7017d994999ad344f72ea85f2c40ff6da7e0810f8a4a6e7b341c",
		},
		{
			name:      "Example signature with low v",
			node_id:   "0xEA28d002042fd9898D0Db016be9758eeAFE35C1E",
			msg:       "Rescue Node 1753055877",
			signature: "3b1ffab4b818e917b81a878265061660c05a5d9eb55bda623b184d1d8f0af9af58c366d487652a7017d994999ad344f72ea85f2c40ff6da7e0810f8a4a6e7b3401",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signature, err := hex.DecodeString(tt.signature)
			assert.NoError(t, err)
			address, err := RecoverAddressFromSignature([]byte(tt.msg), signature)
			assert.NoError(t, err)
			assert.Equal(t, common.HexToAddress(tt.node_id), *address)
		})
	}
}
