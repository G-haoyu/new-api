// Package autorouter classifies the human instruction of a request so the
// gateway can pick a model and reasoning effort for it.
package autorouter

import (
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

// Instruction is the human-written text of the current turn — the router's
// only classification input. Agent clients resend the whole history on every
// call, so all calls of one agent loop yield the same Text, which keeps the
// loop on one model without needing a session id.
type Instruction struct {
	Text string
	// Previous is the human instruction before Text. Its cached decision is
	// what a continuation or an "unknown" result falls back to.
	Previous   string
	HumanTurns int
	// Agent marks text delivered by a parent agent (e.g. Codex NEW_TASK)
	// rather than typed by a person.
	Agent bool
	// Auxiliary marks client background requests (titles, recaps, memory
	// consolidation, suggestions). They are not user tasks.
	Auxiliary bool
}

// Client-injected blocks inside user messages. RE2 has no backreferences, so
// each tag gets its own pattern.
var injectedBlockPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)<system-reminder\b[^>]*>.*?</system-reminder>`),
	regexp.MustCompile(`(?s)<environment_context\b[^>]*>.*?</environment_context>`),
	regexp.MustCompile(`(?s)<user_instructions\b[^>]*>.*?</user_instructions>`),
	regexp.MustCompile(`(?s)<INSTRUCTIONS\b[^>]*>.*?</INSTRUCTIONS>`),
	regexp.MustCompile(`(?s)<turn_aborted\b[^>]*>.*?</turn_aborted>`),
}

// Some clients wrap the human text itself; keep the content, drop the tags.
var userQueryTag = regexp.MustCompile(`</?user_query>`)

var droppedPrefixes = []string{
	"Tool result media from", // image placeholder emitted for tool results
	"# AGENTS.md instructions",
}

var auxiliaryPrefixes = []string{
	"Analyze this rollout",
	"## Memory Writing Agent",
	"You are now acting as the memory",
	"You are a helpful assistant. You will be presented with a user prompt",
	"<session>",
	"[SUGGESTION MODE",
	"The user stepped away",
	"Automation:",
}

var agentPrefixes = []string{
	"Message Type: NEW_TASK",
	"Message Type: MESSAGE",
}

type humanTurn struct {
	text      string
	agent     bool
	auxiliary bool
}

// ExtractInstruction finds the latest human instruction in a request body.
// It works on the body shape rather than a relay format, so the same rules
// apply to client requests and to the upstream bodies recorded in Langfuse.
// Tool results never count as human turns: Claude tool_result blocks are not
// text parts, and Responses function_call_output items carry no user role.
func ExtractInstruction(body []byte) Instruction {
	root := gjson.ParseBytes(body)
	var turns []humanTurn
	switch {
	case root.Get("input").Exists(): // OpenAI Responses
		input := root.Get("input")
		if input.Type == gjson.String {
			turns = appendHumanTurn(turns, []string{input.String()})
			break
		}
		input.ForEach(func(_, item gjson.Result) bool {
			// agent_message is Codex's inter-agent delivery and has no role.
			if item.Get("role").String() == "user" || item.Get("type").String() == "agent_message" {
				turns = appendHumanTurn(turns, contentTextParts(item.Get("content")))
			}
			return true
		})
	case root.Get("contents").Exists(): // Gemini
		root.Get("contents").ForEach(func(_, content gjson.Result) bool {
			if content.Get("role").String() != "user" {
				return true
			}
			var parts []string
			content.Get("parts").ForEach(func(_, part gjson.Result) bool {
				if text := part.Get("text"); text.Type == gjson.String {
					parts = append(parts, text.String())
				}
				return true
			})
			turns = appendHumanTurn(turns, parts)
			return true
		})
	default: // OpenAI Chat and Claude Messages
		root.Get("messages").ForEach(func(_, message gjson.Result) bool {
			if message.Get("role").String() == "user" {
				turns = appendHumanTurn(turns, contentTextParts(message.Get("content")))
			}
			return true
		})
	}
	if len(turns) == 0 {
		return Instruction{}
	}

	// A background request either starts with its own prompt or forks the
	// main conversation by appending one.
	result := Instruction{Auxiliary: turns[0].auxiliary || turns[len(turns)-1].auxiliary}
	var human []humanTurn
	for _, turn := range turns {
		if !turn.auxiliary {
			human = append(human, turn)
		}
	}
	result.HumanTurns = len(human)
	if len(human) == 0 {
		return result
	}
	last := human[len(human)-1]
	result.Text = last.text
	result.Agent = last.agent
	if len(human) > 1 {
		result.Previous = human[len(human)-2].text
	}
	return result
}

// contentTextParts returns the text parts of an OpenAI, Claude or Responses
// message content, which is either a string or an array of typed parts.
func contentTextParts(content gjson.Result) []string {
	if content.Type == gjson.String {
		return []string{content.String()}
	}
	var parts []string
	content.ForEach(func(_, part gjson.Result) bool {
		switch part.Get("type").String() {
		case "text", "input_text":
			parts = append(parts, part.Get("text").String())
		}
		return true
	})
	return parts
}

// appendHumanTurn strips client-injected text from one user message and
// appends it as a turn when real text remains.
func appendHumanTurn(turns []humanTurn, parts []string) []humanTurn {
	var turn humanTurn
	var kept []string
	for _, part := range parts {
		for _, pattern := range injectedBlockPatterns {
			part = pattern.ReplaceAllString(part, "")
		}
		part = strings.TrimSpace(userQueryTag.ReplaceAllString(part, ""))
		if part == "" || hasAnyPrefix(part, droppedPrefixes) {
			continue
		}
		if hasAnyPrefix(part, auxiliaryPrefixes) {
			turn.auxiliary = true
		}
		if hasAnyPrefix(part, agentPrefixes) {
			turn.agent = true
		}
		kept = append(kept, part)
	}
	if len(kept) == 0 {
		return turns
	}
	turn.text = strings.Join(kept, "\n")
	return append(turns, turn)
}

func hasAnyPrefix(text string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}
