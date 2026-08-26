package attemptlog

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// BlockBytes is the chunk size for prefix block-chain hashing. Smaller blocks
// give finer prefix-match granularity at the cost of more hashes per request.
// 256 bytes ≈ 50-100 tokens, coarse enough to bound index memory yet fine
// enough to distinguish prefix boundaries in agent conversations.
const BlockBytes = 256

// hashHex is SHA-256 truncated to 16 hex characters (64 bits). 64 bits keeps
// collision probability negligible for training-scale clustering while saving
// storage in the chain.
func hashHex(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}

// blockChainHash splits data into BlockBytes-sized blocks and computes a
// chained hash: h[0] = SHA256(block[0]), h[i] = SHA256(h[i-1] || block[i]).
// The chain preserves prefix matching: two requests sharing the first K blocks
// share h[0..K-1], so the training pipeline can walk chains to find the
// longest common prefix without knowing block boundaries in advance.
//
// The last partial block is included verbatim (not padded, not dropped) so
// that identical full prompts always produce identical chains.
func blockChainHash(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var sb strings.Builder
	var prevHash []byte
	for i := 0; i < len(data); i += BlockBytes {
		end := i + BlockBytes
		if end > len(data) {
			end = len(data)
		}
		h := sha256.New()
		if prevHash != nil {
			h.Write(prevHash)
		}
		h.Write(data[i:end])
		sum := h.Sum(nil)
		sb.WriteString(hex.EncodeToString(sum[:8]))
		prevHash = sum
	}
	return sb.String()
}

// PrefixHashes holds the three prefix identifiers computed from the raw
// request body. They are byte-exact (no whitespace normalization) to match
// upstream KV-cache semantics: even a single-byte difference breaks cache
// reuse, so the hash must not collapse away those differences.
type PrefixHashes struct {
	System string
	Tools  string
	Prefix string
}

// ComputePrefixHashes extracts the prompt-relevant raw bytes from the request
// body via gjson and hashes them. gjson .Raw returns the original JSON bytes
// including whitespace, so two requests that differ only in JSON formatting
// (spaces, key order) may hash differently — which is correct for byte-exact
// prefix tracking.
//
// The canonical concatenation order is system → tools → messages, matching the
// order an upstream model ingests the prompt. For OpenAI chat the system
// prompt lives inside the messages array, so the system field is empty there.
func ComputePrefixHashes(c *gin.Context, relayFormat types.RelayFormat) PrefixHashes {
	body := rawBodyBytes(c)
	if len(body) == 0 {
		return PrefixHashes{}
	}

	var system, tools, messages []byte
	switch relayFormat {
	case types.RelayFormatOpenAI:
		// System lives inside messages (role=system or role=developer).
		// gjson doesn't support OR in path queries, so try both separately.
		if r := gjson.GetBytes(body, `messages.#(role=system).content`); r.Exists() {
			system = []byte(r.Raw)
		}
		if len(system) == 0 {
			if r := gjson.GetBytes(body, `messages.#(role=developer).content`); r.Exists() {
				system = []byte(r.Raw)
			}
		}
		tools = rawOrNil(gjson.GetBytes(body, "tools"))
		messages = rawOrNil(gjson.GetBytes(body, "messages"))
	case types.RelayFormatClaude:
		system = rawOrNil(gjson.GetBytes(body, "system"))
		tools = rawOrNil(gjson.GetBytes(body, "tools"))
		messages = rawOrNil(gjson.GetBytes(body, "messages"))
	case types.RelayFormatGemini:
		system = rawOrNil(gjson.GetBytes(body, "systemInstruction"))
		tools = rawOrNil(gjson.GetBytes(body, "tools"))
		messages = rawOrNil(gjson.GetBytes(body, "contents"))
	case types.RelayFormatOpenAIResponses:
		system = rawOrNil(gjson.GetBytes(body, "instructions"))
		tools = rawOrNil(gjson.GetBytes(body, "tools"))
		messages = rawOrNil(gjson.GetBytes(body, "input"))
	default:
		return PrefixHashes{}
	}

	fullPrompt := concatRaw(system, tools, messages)
	return PrefixHashes{
		System: hashHex(system),
		Tools:  hashHex(tools),
		Prefix: blockChainHash(fullPrompt),
	}
}

// rawOrNil returns the raw bytes of a gjson result, or nil if the result
// does not exist. gjson .Raw includes quotes/brackets/whitespace verbatim.
func rawOrNil(r gjson.Result) []byte {
	if !r.Exists() {
		return nil
	}
	return []byte(r.Raw)
}

// concatRaw concatenates non-nil byte slices in order.
func concatRaw(parts ...[]byte) []byte {
	var total int
	for _, p := range parts {
		total += len(p)
	}
	if total == 0 {
		return nil
	}
	buf := make([]byte, 0, total)
	for _, p := range parts {
		buf = append(buf, p...)
	}
	return buf
}

// rawBodyBytes returns the raw HTTP request body via the replayable body
// storage. A new reader is requested so this does not consume the handler's
// own body read.
func rawBodyBytes(c *gin.Context) []byte {
	if c == nil {
		return nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil {
		return nil
	}
	reader, err := storage.NewReader()
	if err != nil {
		return nil
	}
	defer reader.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := reader.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf
}
