package attemptlog

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
)

// A representative prompt body: system prompt + tools + a few messages.
// 8KB is typical for an agent request with a medium system prompt.
var benchBody8KB = []byte(`{"model":"glm-5.2","messages":[{"role":"system","content":"` +
	strings.Repeat("You are a helpful assistant. ", 80) +
	`"},{"role":"user","content":"Please fix this Python error: TypeError: undefined is not a function?"}],"tools":[{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{"location":{"type":"string"}}}}}]}`)

func BenchmarkComputePrefixHashes_8KB(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ComputePrefixHashes(benchBody8KB, types.RelayFormatOpenAI)
	}
}

func BenchmarkSparseChainHash_8KB(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = sparseChainHashSpans([][]byte{benchBody8KB})
	}
}

func BenchmarkSparseChainHash_100KB(b *testing.B) {
	big := strings.Repeat("a", 100*1024)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = sparseChainHashSpans([][]byte{[]byte(big)})
	}
}

func BenchmarkLastUserText_8KB(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = LastUserText(benchBody8KB, types.RelayFormatOpenAI)
	}
}

func BenchmarkGuessTaskType(b *testing.B) {
	text := "Please fix this Python error: TypeError: undefined is not a function?"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = GuessTaskType(text)
	}
}

// Large-body benchmarks model the 1M-context era: 200K/300K/600K/1200K byte
// bodies correspond roughly to 150K/256K/500K/1M token contexts. The dominant
// cost is sparseChainHash (O(log(body)) SHA256 calls); gjson extraction adds an
// O(body) streaming parse. These tell us whether telemetry stays negligible at
// extreme prompt sizes.
var benchSizes = []struct {
	name string
	size int
}{
	{"200K", 200 * 1024},
	{"300K", 300 * 1024},
	{"600K", 600 * 1024},
	{"1200K", 1200 * 1024},
}

func makeLargeOpenAIBody(targetBytes int) []byte {
	filler := strings.Repeat("a", targetBytes)
	return []byte(`{"model":"gpt-4o","messages":[{"role":"system","content":"` + filler + `"},{"role":"user","content":"hello"}],"tools":[]}`)
}

func BenchmarkBlockChainHash_Large(b *testing.B) {
	for _, sz := range benchSizes {
		data := makeLargeOpenAIBody(sz.size)
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = sparseChainHashSpans([][]byte{data})
			}
		})
	}
}

func BenchmarkComputePrefixHashes_Large(b *testing.B) {
	for _, sz := range benchSizes {
		body := makeLargeOpenAIBody(sz.size)
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = ComputePrefixHashes(body, types.RelayFormatOpenAI)
			}
		})
	}
}

func BenchmarkLastUserText_Large(b *testing.B) {
	for _, sz := range benchSizes {
		body := makeLargeOpenAIBody(sz.size)
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = LastUserText(body, types.RelayFormatOpenAI)
			}
		})
	}
}

// Realistic agent bodies: large system prompt + ~30 tool definitions +
// multi-turn messages + a per-request volatile session id in metadata/user.
// These exercise the prompt-subtree extraction path (gjson spans + chained
// hashing) rather than a flat filler buffer.
func makeRealisticOpenAIBody(targetBytes int, sessionID string) []byte {
	tools := make([]string, 0, 30)
	for i := range 30 {
		tools = append(tools, `{"type":"function","function":{"name":"tool_`+string(rune('a'+i))+`","description":"`+strings.Repeat("desc ", 20)+`","parameters":{"type":"object","properties":{"arg":{"type":"string","description":"`+strings.Repeat("d ", 15)+`"}}}}}`)
	}
	msgs := []string{`{"role":"system","content":"` + strings.Repeat("You are a coding agent. ", 30) + `"`}
	total := 4096
	for total < targetBytes {
		msgs = append(msgs, `{"role":"user","content":"`+strings.Repeat("u", 2000)+`"}`)
		msgs = append(msgs, `{"role":"assistant","content":"`+strings.Repeat("a", 2000)+`"}`)
		total += 4200
	}
	return []byte(`{"model":"gpt-4o","stream":true,"temperature":0.7,"max_tokens":4096,` +
		`"metadata":{"user_id":"session_` + sessionID + `"},` +
		`"user":"session_` + sessionID + `",` +
		`"tools":[` + strings.Join(tools, ",") + `],` +
		`"messages":[` + strings.Join(msgs, ",") + `]}`)
}

func makeRealisticClaudeBody(targetBytes int, sessionID string) []byte {
	msgs := make([]string, 0, 64)
	total := 2048
	for total < targetBytes {
		msgs = append(msgs, `{"role":"user","content":[{"type":"text","text":"`+strings.Repeat("u", 2000)+`"}]}`)
		msgs = append(msgs, `{"role":"assistant","content":[{"type":"text","text":"`+strings.Repeat("a", 2000)+`"}]}`)
		total += 4400
	}
	tools := make([]string, 0, 30)
	for i := range 30 {
		tools = append(tools, `{"name":"tool_`+string(rune('a'+i))+`","description":"`+strings.Repeat("desc ", 20)+`","input_schema":{"type":"object","properties":{"arg":{"type":"string"}}}}`)
	}
	return []byte(`{"model":"claude-sonnet-4","max_tokens":8192,"stream":true,` +
		`"metadata":{"user_id":"session_` + sessionID + `"},` +
		`"system":[{"type":"text","text":"` + strings.Repeat("You are Claude Code. ", 30) + `"}],` +
		`"tools":[` + strings.Join(tools, ",") + `],` +
		`"messages":[` + strings.Join(msgs, ",") + `]}`)
}

var benchRealSizes = []struct {
	name string
	size int
}{
	{"8K", 8 * 1024},
	{"128K", 128 * 1024},
	{"1200K", 1200 * 1024},
}

func BenchmarkComputePrefixHashes_Realistic_OpenAI(b *testing.B) {
	for _, sz := range benchRealSizes {
		body := makeRealisticOpenAIBody(sz.size, "abc123")
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for b.Loop() {
				_ = ComputePrefixHashes(body, types.RelayFormatOpenAI)
			}
		})
	}
}

func BenchmarkComputePrefixHashes_Realistic_Claude(b *testing.B) {
	for _, sz := range benchRealSizes {
		body := makeRealisticClaudeBody(sz.size, "abc123")
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for b.Loop() {
				_ = ComputePrefixHashes(body, types.RelayFormatClaude)
			}
		})
	}
}
