package attemptlog

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBlockChainHashPrefixSharing pins the core property of the chained hash:
// two byte sequences that share a common prefix share the first K block hashes,
// so the training pipeline can walk chains to find the longest common prefix
// without knowing block boundaries in advance.
func TestBlockChainHashPrefixSharing(t *testing.T) {
	short := strings.Repeat("a", BlockBytes)    // exactly 1 block
	medium := strings.Repeat("a", BlockBytes*3) // 3 blocks, same prefix
	long := strings.Repeat("a", BlockBytes*5)   // 5 blocks, same prefix

	hShort := blockChainHash([]byte(short))
	hMedium := blockChainHash([]byte(medium))
	hLong := blockChainHash([]byte(long))

	// Each block hash is 16 hex chars; the chain for N blocks is N*16 chars.
	assert.Equal(t, 16, len(hShort))
	assert.Equal(t, 16*3, len(hMedium))
	assert.Equal(t, 16*5, len(hLong))

	// The first block hash is the same across all three because block 0 is
	// identical.
	assert.Equal(t, hShort, hMedium[:16])
	assert.Equal(t, hShort, hLong[:16])

	// The first 3 block hashes of medium and long are identical.
	assert.Equal(t, hMedium, hLong[:16*3])
}

// TestBlockChainHashByteExact guards the user's hard requirement: even a
// single-byte difference (extra whitespace) must produce a different hash,
// because upstream KV cache is byte-exact.
func TestBlockChainHashByteExact(t *testing.T) {
	a := blockChainHash([]byte(`{"system":"hello"}`))
	b := blockChainHash([]byte(`{"system":"hello "}`)) // trailing space
	assert.NotEqual(t, a, b, "trailing space must change the hash")

	c := blockChainHash([]byte(`{"system":"hello"}`)) // identical to a
	assert.Equal(t, a, c, "identical input must hash the same")
}

func TestBlockChainHashEmpty(t *testing.T) {
	assert.Equal(t, "", blockChainHash(nil))
	assert.Equal(t, "", blockChainHash([]byte{}))
}

// TestBlockChainHashPartialBlock confirms that a partial last block is included
// (not dropped), so identical full prompts always produce identical chains.
func TestBlockChainHashPartialBlock(t *testing.T) {
	// 300 bytes = 1 full block (256) + 1 partial block (44).
	data := strings.Repeat("x", 300)
	h := blockChainHash([]byte(data))
	assert.Equal(t, 16*2, len(h), "300 bytes at 256/block should produce 2 hashes")

	// A different partial block (different last 44 bytes) changes only the
	// last hash, not the first.
	data2 := strings.Repeat("x", 256) + strings.Repeat("y", 44)
	h2 := blockChainHash([]byte(data2))
	assert.Equal(t, h[:16], h2[:16], "first block must match")
	assert.NotEqual(t, h[16:], h2[16:], "partial block must differ")
}

func TestHashHexEmpty(t *testing.T) {
	assert.Equal(t, "", hashHex(nil))
	assert.Equal(t, "", hashHex([]byte{}))
}

func TestHashHexStable(t *testing.T) {
	h1 := hashHex([]byte("hello"))
	h2 := hashHex([]byte("hello"))
	assert.Equal(t, h1, h2)
	assert.Equal(t, 16, len(h1))
	assert.NotEqual(t, h1, hashHex([]byte("hello!")))
}
