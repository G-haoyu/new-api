package attemptlog

import (
	"bytes"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// firstBlockSize is the size of the first block in the sparse schedule.
const firstBlockSize = 256

// chainOf hashes a single contiguous buffer through the span implementation,
// preserving the original single-buffer semantics in the tests below.
func chainOf(data []byte) string {
	return sparseChainHashSpans([][]byte{data})
}

// TestSparseChainHashPrefixSharing pins the core property: two byte sequences
// sharing a common prefix share the initial block hashes, so the training
// pipeline can walk chains to find the longest common prefix. The schedule is
// fixed, so absolute positions are consistent regardless of total length.
func TestSparseChainHashPrefixSharing(t *testing.T) {
	short := strings.Repeat("a", firstBlockSize)
	medium := strings.Repeat("a", firstBlockSize*3)
	long := strings.Repeat("a", firstBlockSize+sparseBlockSizes[1]+sparseBlockSizes[2])

	hShort := chainOf([]byte(short))
	hMedium := chainOf([]byte(medium))
	hLong := chainOf([]byte(long))

	assert.Equal(t, 16, len(hShort))
	assert.True(t, len(hMedium) >= 16)
	assert.True(t, len(hLong) >= 16)

	assert.Equal(t, hShort, hMedium[:16], "first block hash must match")
	assert.Equal(t, hShort, hLong[:16], "first block hash must match")
}

// TestSparseChainHashAbsolutePositionConsistency verifies that a 200KB body
// and a 1.2MB body sharing the first 200KB produce the same block hashes for
// all blocks that fit ENTIRELY within the shared prefix. The boundary block
// (partial in the short body, full in the long body) naturally differs.
func TestSparseChainHashAbsolutePositionConsistency(t *testing.T) {
	sharedPrefix := strings.Repeat("x", 200*1024)
	body1 := []byte(sharedPrefix)
	body2 := []byte(sharedPrefix + strings.Repeat("y", 1024*1024))

	h1 := chainOf(body1)
	h2 := chainOf(body2)

	// Count how many blocks fit entirely within the 200KB shared prefix.
	fullCoverage := 0
	for _, bs := range sparseBlockSizes {
		if fullCoverage+bs > 200*1024 {
			break
		}
		fullCoverage += bs
	}
	fullBlocks := fullCoverage / 256 // approximate; just need the count
	// Recount properly by walking the schedule.
	fullBlocks = 0
	covered := 0
	for _, bs := range sparseBlockSizes {
		if covered+bs > 200*1024 {
			break
		}
		covered += bs
		fullBlocks++
	}
	assert.True(t, fullBlocks >= 8, "200KB should have at least 8 full blocks")
	sharedHex := fullBlocks * 16
	assert.Equal(t, h1[:sharedHex], h2[:sharedHex],
		"blocks fitting entirely within the shared prefix must match")
}

func TestSparseChainHashByteExact(t *testing.T) {
	a := chainOf([]byte(`{"system":"hello"}`))
	b := chainOf([]byte(`{"system":"hello "}`))
	assert.NotEqual(t, a, b, "trailing space must change the hash")

	c := chainOf([]byte(`{"system":"hello"}`))
	assert.Equal(t, a, c, "identical input must hash the same")
}

func TestSparseChainHashEmpty(t *testing.T) {
	assert.Equal(t, "", chainOf(nil))
	assert.Equal(t, "", chainOf([]byte{}))
	assert.Equal(t, "", sparseChainHashSpans(nil))
	assert.Equal(t, "", sparseChainHashSpans([][]byte{nil, {}}))
}

func TestSparseChainHashPartialBlock(t *testing.T) {
	data := strings.Repeat("x", 300)
	h := chainOf([]byte(data))
	assert.Equal(t, 16*2, len(h), "300 bytes should produce 2 hashes")

	data2 := strings.Repeat("x", 256) + strings.Repeat("y", 44)
	h2 := chainOf([]byte(data2))
	assert.Equal(t, h[:16], h2[:16], "first block must match")
	assert.NotEqual(t, h[16:], h2[16:], "partial block must differ")
}

func TestSparseChainHashBlockCount(t *testing.T) {
	cases := []struct {
		size      int
		maxBlocks int
	}{
		{256, 1},
		{1024, 3},         // 256+256+512
		{8192, 6},         // +1024+2048+4096
		{1200 * 1024, 14}, // 13 full + 1 partial
	}
	for _, tc := range cases {
		data := strings.Repeat("a", tc.size)
		h := chainOf([]byte(data))
		blocks := len(h) / 16
		assert.True(t, blocks <= tc.maxBlocks, "size %d: %d blocks > max %d", tc.size, blocks, tc.maxBlocks)
	}
}

// TestSparseChainHashSpansEquivalence pins that hashing the virtual
// concatenation of spans equals hashing one contiguous buffer: spans must not
// introduce offset drift at boundaries, including mid-block boundaries.
func TestSparseChainHashSpansEquivalence(t *testing.T) {
	cases := []struct {
		name  string
		spans []string
	}{
		{"single", []string{strings.Repeat("a", 1000)}},
		{"aligned boundary", []string{strings.Repeat("a", 256), strings.Repeat("b", 256)}},
		{"mid-block boundary", []string{strings.Repeat("a", 300), strings.Repeat("b", 200), strings.Repeat("c", 1000)}},
		{"with empty spans", []string{"", strings.Repeat("a", 300), "", strings.Repeat("b", 200), ""}},
		{"all empty", []string{"", ""}},
	}
	for _, tc := range cases {
		spans := make([][]byte, len(tc.spans))
		var joined strings.Builder
		for i, s := range tc.spans {
			spans[i] = []byte(s)
			joined.WriteString(s)
		}
		assert.Equal(t, chainOf([]byte(joined.String())), sparseChainHashSpans(spans), tc.name)
	}
}

// TestSparseChainHashSpansNoSpuriousBlocks verifies a trailing exhausted span
// does not emit an extra block hash.
func TestSparseChainHashSpansNoSpuriousBlocks(t *testing.T) {
	data := strings.Repeat("a", 300)
	h1 := chainOf([]byte(data))
	h2 := sparseChainHashSpans([][]byte{[]byte(data[:100]), []byte(data[100:200]), []byte(data[200:])})
	assert.Equal(t, h1, h2)
	assert.Equal(t, 2, len(h1)/16, "300 bytes must produce exactly 2 hashes")
}

// --- ComputePrefixHashes ---

// openaiBody builds a chat-completions request with a per-request volatile
// session identity in metadata and user, plus sampling params, alongside a
// large shared conversation.
func openaiBody(sessionID, conversation string) string {
	return `{"model":"gpt-4o","stream":true,"temperature":0.7,` +
		`"metadata":{"user_id":"` + sessionID + `"},"user":"` + sessionID + `",` +
		`"tools":[{"type":"function","function":{"name":"get_weather"}}],` +
		`"messages":[{"role":"system","content":"You are helpful."},` + conversation + `]}`
}

// TestComputePrefixHashesVolatileFieldImmunity is the core regression: bodies
// differing only in per-request volatile envelope fields (metadata.user_id,
// user) sharing identical prompt content must produce identical Prefix chains,
// identical System hashes, and identical Tools hashes. Before this fix the
// whole body was hashed, so a session ID in the first block destroyed the
// entire chain.
func TestComputePrefixHashesVolatileFieldImmunity(t *testing.T) {
	conversation := `{"role":"user","content":"` + strings.Repeat("hello ", 500) + `"}`
	bodyA := []byte(openaiBody("session-aaa", conversation))
	bodyB := []byte(openaiBody("session-bbb", conversation))
	require.NotEqual(t, bodyA, bodyB, "test bodies must differ in the volatile fields")

	a := ComputePrefixHashes(bodyA, types.RelayFormatOpenAI)
	b := ComputePrefixHashes(bodyB, types.RelayFormatOpenAI)

	assert.Equal(t, a.Prefix, b.Prefix, "volatile envelope fields must not reach the prefix chain")
	assert.Equal(t, a.System, b.System)
	assert.Equal(t, a.Tools, b.Tools)
	assert.NotEmpty(t, a.Prefix)
}

// TestComputePrefixHashesKeyOrderImmunity verifies that a client reordering
// top-level JSON keys does not change the prefix chain, since only prompt
// subtrees are extracted and recombined in a fixed order.
func TestComputePrefixHashesKeyOrderImmunity(t *testing.T) {
	messages := `[{"role":"system","content":"sys"},{"role":"user","content":"hi"}]`
	orderA := []byte(`{"model":"m","temperature":0.5,"messages":` + messages + `}`)
	orderB := []byte(`{"messages":` + messages + `,"temperature":0.5,"model":"m"}`)

	a := ComputePrefixHashes(orderA, types.RelayFormatOpenAI)
	b := ComputePrefixHashes(orderB, types.RelayFormatOpenAI)

	require.NotEmpty(t, a.Prefix)
	assert.Equal(t, a.Prefix, b.Prefix)
	assert.Equal(t, a.System, b.System)
}

// TestComputePrefixHashesConversationAppendSharesPrefix verifies multi-turn
// prefix sharing after extraction: appending a new user turn to the same
// conversation keeps the leading block hashes identical for every block that
// fits entirely within the shared prompt bytes. The boundary block (partial in
// the shorter body) naturally differs, as in the raw-chain consistency test.
func TestComputePrefixHashesConversationAppendSharesPrefix(t *testing.T) {
	first := openaiBody("s1", `{"role":"user","content":"`+strings.Repeat("q", 3000)+`"}`)
	second := openaiBody("s2", `{"role":"user","content":"`+strings.Repeat("q", 3000)+`"},{"role":"assistant","content":"ok"},{"role":"user","content":"next"}`)

	h1 := ComputePrefixHashes([]byte(first), types.RelayFormatOpenAI)
	h2 := ComputePrefixHashes([]byte(second), types.RelayFormatOpenAI)
	require.NotEmpty(t, h1.Prefix)

	// Rebuild the prompt spans the same way production does, concatenate them,
	// and measure how many bytes the two prompts share.
	promptOf := func(body string) []byte {
		raw := []byte(body)
		return bytes.Join([][]byte{extractRaw(raw, "tools"), extractRaw(raw, "messages")}, nil)
	}
	p1, p2 := promptOf(first), promptOf(second)
	shared := 0
	for shared < len(p1) && shared < len(p2) && p1[shared] == p2[shared] {
		shared++
	}
	require.Greater(t, shared, firstBlockSize, "test prompts must share a meaningful prefix")

	fullBlocks, covered := 0, 0
	for _, bs := range sparseBlockSizes {
		if covered+bs > shared {
			break
		}
		covered += bs
		fullBlocks++
	}
	require.Greater(t, fullBlocks, 0)
	assert.Equal(t, h1.Prefix[:fullBlocks*16], h2.Prefix[:fullBlocks*16],
		"blocks fitting entirely within the shared prompt must match")
}

// TestComputePrefixHashesPromptChangesDiverge verifies that changing actual
// prompt content (system or tools) still produces different chains — the fix
// must not over-normalize.
func TestComputePrefixHashesPromptChangesDiverge(t *testing.T) {
	conv := `{"role":"user","content":"hello"}`
	base := ComputePrefixHashes([]byte(openaiBody("s", conv)), types.RelayFormatOpenAI)

	diffSystem := ComputePrefixHashes([]byte(strings.Replace(openaiBody("s", conv),
		`"content":"You are helpful."`, `"content":"You are helpful!"`, 1)), types.RelayFormatOpenAI)
	diffTools := ComputePrefixHashes([]byte(strings.Replace(openaiBody("s", conv),
		`"name":"get_weather"`, `"name":"get_time"`, 1)), types.RelayFormatOpenAI)

	assert.NotEqual(t, base.Prefix, diffSystem.Prefix, "system change must break the chain")
	assert.NotEqual(t, base.Prefix, diffTools.Prefix, "tools change must break the chain")
	assert.NotEqual(t, base.System, diffSystem.System)
	assert.NotEqual(t, base.Tools, diffTools.Tools)
}

// TestComputePrefixHashesPerFormatExtraction covers each supported format with
// explicit expected subtree behavior: System/Tools hashes reflect the right
// fields, the chain depends only on prompt subtrees, and unknown formats and
// empty bodies yield empty hashes.
func TestComputePrefixHashesPerFormatExtraction(t *testing.T) {
	cases := []struct {
		name       string
		format     types.RelayFormat
		body       string
		wantSystem bool
		wantTools  bool
		wantPrefix bool
	}{
		{
			name:       "openai chat",
			format:     types.RelayFormatOpenAI,
			body:       `{"model":"m","metadata":{"user_id":"u1"},"messages":[{"role":"system","content":"S"},{"role":"user","content":"U"}],"tools":[{"type":"function"}]}`,
			wantSystem: true, wantTools: true, wantPrefix: true,
		},
		{
			name:       "openai developer role",
			format:     types.RelayFormatOpenAI,
			body:       `{"messages":[{"role":"developer","content":"D"},{"role":"user","content":"U"}]}`,
			wantSystem: true, wantPrefix: true,
		},
		{
			name:       "openai legacy completions",
			format:     types.RelayFormatOpenAI,
			body:       `{"model":"m","prompt":"once upon a time","max_tokens":16}`,
			wantPrefix: true,
		},
		{
			name:       "claude",
			format:     types.RelayFormatClaude,
			body:       `{"model":"m","metadata":{"user_id":"u1"},"system":[{"type":"text","text":"S"}],"tools":[{"name":"t"}],"messages":[{"role":"user","content":"U"}]}`,
			wantSystem: true, wantTools: true, wantPrefix: true,
		},
		{
			name:       "gemini",
			format:     types.RelayFormatGemini,
			body:       `{"model":"m","systemInstruction":{"parts":[{"text":"S"}]},"tools":[{"functionDeclarations":[]}],"contents":[{"role":"user","parts":[{"text":"U"}]}]}`,
			wantSystem: true, wantTools: true, wantPrefix: true,
		},
		{
			name:       "responses",
			format:     types.RelayFormatOpenAIResponses,
			body:       `{"model":"m","instructions":"S","tools":[{"type":"function"}],"input":[{"role":"user","content":"U"}]}`,
			wantSystem: true, wantTools: true, wantPrefix: true,
		},
		{
			name:   "unknown format",
			format: types.RelayFormatEmbedding,
			body:   `{"model":"m","input":"text"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := ComputePrefixHashes([]byte(tc.body), tc.format)
			if tc.wantSystem {
				assert.NotEmpty(t, h.System)
			} else {
				assert.Empty(t, h.System)
			}
			if tc.wantTools {
				assert.NotEmpty(t, h.Tools)
			} else {
				assert.Empty(t, h.Tools)
			}
			if tc.wantPrefix {
				assert.NotEmpty(t, h.Prefix)
			} else {
				assert.Empty(t, h.Prefix)
			}
		})
	}

	// Empty and unparseable bodies produce empty hashes rather than falling
	// back to whole-body hashing.
	assert.Equal(t, PrefixHashes{}, ComputePrefixHashes(nil, types.RelayFormatOpenAI))
	assert.Equal(t, PrefixHashes{}, ComputePrefixHashes([]byte("not json"), types.RelayFormatClaude))
}

// TestComputePrefixHashesClaudeVolatileImmunity repeats the core regression for
// Claude, whose metadata.user_id is the most common real-world offender.
func TestComputePrefixHashesClaudeVolatileImmunity(t *testing.T) {
	conversation := `[{"role":"user","content":"` + strings.Repeat("hi ", 400) + `"}]`
	build := func(session string) []byte {
		return []byte(`{"model":"claude-sonnet-4","metadata":{"user_id":"` + session +
			`"},"system":"You are Claude.","max_tokens":1024,"messages":` + conversation + `}`)
	}

	a := ComputePrefixHashes(build("sess-1"), types.RelayFormatClaude)
	b := ComputePrefixHashes(build("sess-2"), types.RelayFormatClaude)

	require.NotEmpty(t, a.Prefix)
	assert.Equal(t, a.Prefix, b.Prefix)
	assert.Equal(t, a.System, b.System)
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
