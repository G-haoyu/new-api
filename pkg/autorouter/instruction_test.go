package autorouter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractInstruction(t *testing.T) {
	tests := []struct {
		name string
		body string
		want Instruction
	}{
		{
			name: "openai chat skips tool messages and keeps previous turn",
			body: `{"messages":[{"role":"system","content":"sys"},{"role":"user","content":"修复登录 bug"},{"role":"assistant","content":null,"tool_calls":[{"id":"1"}]},{"role":"tool","content":"ok"},{"role":"user","content":"再加个单测"}]}`,
			want: Instruction{Text: "再加个单测", Previous: "修复登录 bug", HumanTurns: 2},
		},
		{
			name: "claude tool_result-only turn is not a human turn",
			body: `{"system":"sys","messages":[{"role":"user","content":[{"type":"text","text":"重构这个模块"}]},{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"file body"}]}]}`,
			want: Instruction{Text: "重构这个模块", HumanTurns: 1},
		},
		{
			name: "claude code system-reminder blocks are stripped",
			body: `{"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>\nCodebase instructions\n</system-reminder>"},{"type":"text","text":"<system-reminder data-role=\"user-context\">ctx</system-reminder>\n帮我看下这个报错"}]}]}`,
			want: Instruction{Text: "帮我看下这个报错", HumanTurns: 1},
		},
		{
			name: "codex responses drops environment and AGENTS.md messages",
			body: `{"instructions":"sys","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions for /repo\n\n<INSTRUCTIONS>rules</INSTRUCTIONS>"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n<cwd>/repo</cwd>\n</environment_context>"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"检查一下是不是没安装成功"}]},{"type":"function_call","call_id":"c1","name":"exec","arguments":"{}"},{"type":"function_call_output","call_id":"c1","output":"done"}]}`,
			want: Instruction{Text: "检查一下是不是没安装成功", HumanTurns: 1},
		},
		{
			name: "responses string input",
			body: `{"input":"总结这篇文章"}`,
			want: Instruction{Text: "总结这篇文章", HumanTurns: 1},
		},
		{
			name: "user_query wrapper keeps its content",
			body: `{"messages":[{"role":"user","content":"<system-reminder data-role=\"user-context\">info</system-reminder>\n<user_query>你是什么模型</user_query>"}]}`,
			want: Instruction{Text: "你是什么模型", HumanTurns: 1},
		},
		{
			name: "tool result image placeholder is dropped",
			body: `{"messages":[{"role":"user","content":"截图里的按钮对齐了吗"},{"role":"user","content":[{"type":"text","text":"Tool result media from Read:"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA"}}]}]}`,
			want: Instruction{Text: "截图里的按钮对齐了吗", HumanTurns: 1},
		},
		{
			name: "memory fork appended to main conversation is auxiliary",
			body: `{"messages":[{"role":"user","content":"列出 top 20 用户"},{"role":"assistant","content":"..."},{"role":"user","content":"You are now acting as the memory extraction subagent."}]}`,
			want: Instruction{Text: "列出 top 20 用户", HumanTurns: 1, Auxiliary: true},
		},
		{
			name: "background request that starts with its own prompt is auxiliary",
			body: `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Analyze this rollout and produce JSON"}]}]}`,
			want: Instruction{Auxiliary: true},
		},
		{
			name: "codex agent_message delivery is marked as agent",
			body: `{"input":[{"type":"agent_message","content":[{"type":"input_text","text":"Message Type: NEW_TASK\nTask name: /root/scan\nPayload:"}]}]}`,
			want: Instruction{Text: "Message Type: NEW_TASK\nTask name: /root/scan\nPayload:", HumanTurns: 1, Agent: true},
		},
		{
			name: "gemini contents",
			body: `{"contents":[{"role":"user","parts":[{"text":"翻译成英文"}]},{"role":"model","parts":[{"text":"..."}]},{"role":"user","parts":[{"functionResponse":{"name":"f"}}]}]}`,
			want: Instruction{Text: "翻译成英文", HumanTurns: 1},
		},
		{
			name: "no user message",
			body: `{"messages":[{"role":"system","content":"sys"}]}`,
			want: Instruction{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ExtractInstruction([]byte(tt.body)))
		})
	}
}

func TestBuildState(t *testing.T) {
	tests := []struct {
		name              string
		current, previous string
		max, previousMax  int
		want              map[string]string
	}{
		{
			name: "first turn has no previous_task field and keeps head and tail", current: "一二三四五六七八九十", max: 5, previousMax: 2,
			want: map[string]string{"current_task": "一二三\n[...]\n九十"},
		},
		{
			name: "short previous leaves the rest of the budget to current", current: "一二三四五六七八", previous: "上一", max: 10, previousMax: 4,
			want: map[string]string{"current_task": "一二三四五六七八", "previous_task": "上一"},
		},
		{
			name: "long previous is capped and current gets the remainder", current: "一二三四五六七八九十", previous: "甲乙丙丁戊己", max: 10, previousMax: 5,
			want: map[string]string{"current_task": "一二三\n[...]\n九十", "previous_task": "甲乙丙\n[...]\n戊己"},
		},
		{
			name: "zero budget keeps everything", current: "一二三四五六七八九十", previous: "甲乙丙丁戊己",
			want: map[string]string{"current_task": "一二三四五六七八九十", "previous_task": "甲乙丙丁戊己"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, BuildState(tt.current, tt.previous, tt.max, tt.previousMax))
		})
	}
}

func TestDecisionPicksMostLikelyDifficultyRung(t *testing.T) {
	confidence, score, change := 0.7, 1.4, 0.2
	resp := JevResponse{Answers: map[string]JevAnswer{
		"task_type":   {Type: "choice", Choice: "coding", Confidence: &confidence, Probabilities: map[string]float64{"coding": 0.8, "debugging": 0.2}},
		"difficulty":  {Type: "score", Score: &score, Probabilities: map[string]float64{"0": 0.1, "1": 0.5, "2": 0.4}},
		"task_change": {Type: "noul", Noul: &change},
	}}
	decision := resp.Decision(true)
	assert.Equal(t, "coding", decision.TaskType)
	assert.Equal(t, 0.8, decision.TaskTypeProbability)
	assert.Equal(t, 2, decision.Difficulty)
	assert.Equal(t, &change, decision.TaskChange)
	assert.Nil(t, decision.LargerModelBenefit)

	// A first turn has nothing to continue: task_change is 1 regardless of the model.
	firstTurn := resp.Decision(false)
	assert.Equal(t, 1.0, *firstTurn.TaskChange)
	assert.Equal(t, 0.2, *resp.Answers["task_change"].Noul, "raw answer is kept for audit")
}
