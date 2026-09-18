package attemptlog

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unsafe"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// sparseBlockSizes defines the fixed exponential sampling schedule for prefix
// hashing. Block sizes double after the first two 256-byte blocks, giving
// fine-grained matching near the start (system + tools + first message) and
// coarse coverage for conversation history. The schedule is the same for all
// body sizes, so absolute byte positions are consistent: a 200KB body and a
// 1.2MB body sharing the first 200KB produce the same initial block hashes.
//
// Coverage: 256+256+512+1024+...+32MB ≈ 64MB, far beyond any realistic prompt.
var sparseBlockSizes = func() []int {
	sizes := []int{256, 256}
	for size := 512; size <= 32*1024*1024; size *= 2 {
		sizes = append(sizes, size)
	}
	return sizes
}()

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

// sparseChainHashSpans hashes the virtual concatenation of spans at fixed
// exponential positions using a chained scheme: h[0] = SHA256(block[0]),
// h[i] = SHA256(h[i-1] || block[i]). The chain preserves prefix matching: two
// inputs sharing the first K bytes share all block hashes whose
// offset+size <= K.
//
// The concatenation is never materialized. Blocks are contiguous — every input
// byte is hashed exactly once, and "sparse" refers to the checkpoints emitted,
// not to sampling. A block straddling a span boundary writes its fragments into
// the same hasher, so the result is identical to hashing one contiguous buffer
// while allocating nothing beyond the output string. Zero-length spans are
// skipped rather than emitting a block that covers nothing.
//
// The last partial block (when the input doesn't fill the scheduled size) is
// included verbatim so identical inputs always produce identical chains. For
// 1.2MB of prompt this produces 14 hashes (vs ~4700 for dense 256-byte blocks),
// and the output is ~224 bytes (vs ~75KB).
func sparseChainHashSpans(spans [][]byte) string {
	total := 0
	for _, s := range spans {
		total += len(s)
	}
	if total == 0 {
		return ""
	}

	var sb strings.Builder
	var prevHash []byte
	spanIdx, spanOff := 0, 0
	for _, blockSize := range sparseBlockSizes {
		// Advance past exhausted spans before deciding whether input remains,
		// so a trailing nil span cannot emit a spurious final block.
		for spanIdx < len(spans) && spanOff >= len(spans[spanIdx]) {
			spanIdx++
			spanOff = 0
		}
		if spanIdx >= len(spans) {
			break
		}

		h := sha256.New()
		if prevHash != nil {
			h.Write(prevHash)
		}
		remaining := blockSize
		for remaining > 0 && spanIdx < len(spans) {
			avail := len(spans[spanIdx]) - spanOff
			if avail <= 0 {
				spanIdx++
				spanOff = 0
				continue
			}
			n := min(avail, remaining)
			h.Write(spans[spanIdx][spanOff : spanOff+n])
			spanOff += n
			remaining -= n
		}
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

// ComputePrefixHashes extracts the prompt-bearing subtrees from the raw body
// via gjson (using Result.Index for zero-copy slicing into the original body)
// and chain-hashes them in a fixed per-format order — the order those fields
// occupy in the rendered prompt (tool definitions are injected into the system
// region ahead of the conversation by most chat templates).
//
// The chain deliberately never reads envelope fields (model, stream, sampling
// params, and volatile identity fields such as metadata.user_id, user, or
// prompt_cache_key). The chain is cascading — one differing byte in an early
// block invalidates every later block hash — so hashing the whole body let a
// per-request session ID destroy cross-request prefix matching entirely. Only
// prompt content reaches the chain, mirroring what upstream KV caches
// (vLLM/SGLang) actually key on: the tokenized rendered prompt.
//
// Known limitation: Claude clients move cache_control breakpoints between
// turns, which mutates bytes mid-messages and shifts later offsets, degrading
// (not breaking) prefix matching for such requests. Stripping cache_control
// would require rewriting the messages array, conflicting with zero-copy
// extraction.
func ComputePrefixHashes(body []byte, relayFormat types.RelayFormat) PrefixHashes {
	if len(body) == 0 {
		return PrefixHashes{}
	}

	var hashes PrefixHashes
	switch relayFormat {
	case types.RelayFormatOpenAI:
		tools := extractRaw(body, "tools")
		hashes.Tools = hashHex(tools)
		messages := extractRaw(body, "messages")
		if messages == nil {
			// Legacy /v1/completions shares this relay format; prompt is the
			// whole conversation there.
			messages = extractRaw(body, "prompt")
		} else {
			// System lives inside messages; try both roles. The filter runs
			// over the already-extracted span instead of rescanning the body.
			system := extractRaw(messages, `#(role=system).content`)
			if system == nil {
				system = extractRaw(messages, `#(role=developer).content`)
			}
			hashes.System = hashHex(system)
		}
		hashes.Prefix = sparseChainHashSpans([][]byte{tools, messages})
	case types.RelayFormatClaude:
		system := extractRaw(body, "system")
		tools := extractRaw(body, "tools")
		messages := extractRaw(body, "messages")
		hashes.System = hashHex(system)
		hashes.Tools = hashHex(tools)
		hashes.Prefix = sparseChainHashSpans([][]byte{system, tools, messages})
	case types.RelayFormatGemini:
		system := extractRaw(body, "systemInstruction")
		tools := extractRaw(body, "tools")
		contents := extractRaw(body, "contents")
		hashes.System = hashHex(system)
		hashes.Tools = hashHex(tools)
		hashes.Prefix = sparseChainHashSpans([][]byte{system, tools, contents})
	case types.RelayFormatOpenAIResponses:
		instructions := extractRaw(body, "instructions")
		tools := extractRaw(body, "tools")
		input := extractRaw(body, "input")
		hashes.System = hashHex(instructions)
		hashes.Tools = hashHex(tools)
		hashes.Prefix = sparseChainHashSpans([][]byte{instructions, tools, input})
	default:
		return PrefixHashes{}
	}
	return hashes
}

// extractRaw returns the raw JSON bytes of a gjson path result, slicing the
// original body via Result.Index to avoid a copy. Returns nil for absent,
// null, empty array, or empty string values.
//
// gjson.GetBytes cannot be used here: it converts the result Raw to a Go
// string (copying the whole span) and callers then copy it again when
// converting back to bytes — two full copies of messages-sized spans. Instead
// this views the body as a string without copying (safe: nothing writes
// through the view) and slices the view back into the body's backing array.
// The result aliases body, so callers must keep body alive while using it.
func extractRaw(body []byte, path string) []byte {
	if path == "" {
		return nil
	}
	bodyStr := unsafe.String(unsafe.SliceData(body), len(body))
	r := gjson.Get(bodyStr, path)
	if !r.Exists() {
		return nil
	}
	raw := r.Raw
	if raw == "[]" || raw == "null" || raw == "\"\"" {
		return nil
	}
	// Slice the original body when gjson recorded the offset into bodyStr.
	if r.Index > 0 && r.Index+len(raw) <= len(bodyStr) {
		return body[r.Index : r.Index+len(raw)]
	}
	// Fallback (modifiers, sub-selectors): one string→[]byte copy.
	return []byte(raw)
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
