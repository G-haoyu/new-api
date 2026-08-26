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
func ComputePrefixHashes(body []byte, relayFormat types.RelayFormat) PrefixHashes {
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

// rawOrNil returns the raw bytes of a gjson result, or nil if the result does
// not exist or is an empty value (null, empty array, empty string). gjson .Raw
// includes quotes/brackets/whitespace verbatim. Treating `[]` and `null` as
// absent matches the semantics that "no tools" and "tools: []" should both
// produce an empty hash.
func rawOrNil(r gjson.Result) []byte {
	if !r.Exists() {
		return nil
	}
	raw := r.Raw
	if raw == "[]" || raw == "null" || raw == "\"\"" {
		return nil
	}
	return []byte(raw)
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

// RawBodyBytes returns the raw HTTP request body via the replayable body
// storage. For the in-memory path (the common case) this is zero-copy — the
// storage returns its backing array directly. Exported so callers can read the
// body once and pass bytes to multiple extractors.
func RawBodyBytes(c *gin.Context) []byte {
	if c == nil {
		return nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil {
		return nil
	}
	data, err := storage.Bytes()
	if err != nil || data == nil {
		return nil
	}
	return data
}

// LastUserText extracts the plain text of the last user message from the raw
// request body, for use as task-type-classification input. Unlike the prefix
// hashes, this does not need to be byte-exact — it just needs the text content
// the user actually asked about in the current turn.
//
// For OpenAI/Claude: the last message with role=user, content field.
// For Gemini: the last content with role=user, parts[].text.
// For Responses: the last input item with role=user, content.
// Content can be a string or an array of typed parts; only text parts are
// concatenated. Returns "" when no user message is found.
func LastUserText(body []byte, relayFormat types.RelayFormat) string {
	if len(body) == 0 {
		return ""
	}

	var lastContent gjson.Result
	switch relayFormat {
	case types.RelayFormatOpenAI, types.RelayFormatClaude:
		userMsgs := gjson.GetBytes(body, `messages.#(role=user)#`).Array()
		if len(userMsgs) == 0 {
			return ""
		}
		lastContent = userMsgs[len(userMsgs)-1].Get("content")
	case types.RelayFormatGemini:
		userContents := gjson.GetBytes(body, `contents.#(role=user)#`).Array()
		if len(userContents) == 0 {
			return ""
		}
		parts := userContents[len(userContents)-1].Get("parts").Array()
		var sb strings.Builder
		for _, p := range parts {
			if t := p.Get("text").String(); t != "" {
				sb.WriteString(t)
			}
		}
		return sb.String()
	case types.RelayFormatOpenAIResponses:
		// input can be a string or an array of input items.
		inputResult := gjson.GetBytes(body, "input")
		if inputResult.Type == gjson.String {
			return inputResult.String()
		}
		userInputs := gjson.GetBytes(body, `input.#(role=user)#`).Array()
		if len(userInputs) == 0 {
			return ""
		}
		lastContent = userInputs[len(userInputs)-1].Get("content")
	default:
		return ""
	}

	return extractTextFromContent(lastContent)
}

// extractTextFromContent handles the two content shapes used by OpenAI/Claude/
// Responses: a bare string, or an array of typed parts whose text is in the
// "text" field.
func extractTextFromContent(r gjson.Result) string {
	if !r.Exists() {
		return ""
	}
	if r.Type == gjson.String {
		return r.String()
	}
	if r.IsArray() {
		var sb strings.Builder
		r.ForEach(func(_, part gjson.Result) bool {
			if t := part.Get("text").String(); t != "" {
				sb.WriteString(t)
			}
			return true
		})
		return sb.String()
	}
	return ""
}
