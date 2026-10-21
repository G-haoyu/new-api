package autorouter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
)

// QuestionsVersion changes whenever the question set or state shape changes,
// so results from different templates are never compared or cached together.
// v2: state carries the previous human instruction next to the current one.
// v3: state is an object with separate current_task/previous_task fields.
const QuestionsVersion = "autorouter_v3"

// TaskTypeUnknown means the instruction cannot be classified on its own
// ("继续", "ok", a choice among options from earlier context).
const TaskTypeUnknown = "unknown"

type jevQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// questions is the routing question set. It sees only the instruction, so
// every question is phrased to be answerable from that text alone.
var questions = map[string]jevQuestion{
	"task_type": {
		Type:         "choice",
		Instructions: "只根据这条用户消息本身判断用户要完成的任务类型，不要猜测消息之外的上下文。只要能判断出类型就选具体类型，即使缺少细节。",
		Criteria: map[string]string{
			"coding":         "要求编写、修改、重构代码或配置",
			"debugging":      "要求定位错误、分析根因、修复故障",
			"operations":     "要求部署、运维、监控，或处理基础设施、线上故障",
			"research":       "要求查资料、核实事实、比较方案或做技术调研",
			"review":         "要求审查代码、测试、报告或验收结果",
			"data_operation": "要求清洗、转换、统计、导入或导出数据",
			"summarization":  "要求总结、改写、提取、翻译或转换格式",
			"direct_qa":      "知识问答、概念解释、寒暄，或可以直接给出的建议",
			TaskTypeUnknown:  "离开前文完全无法判断要做什么：如“继续”“ok”“好的”、对前文方案的确认或选择、只用“这个”“上面”指代前文且没有说明动作",
		},
	},
	"difficulty": {
		Type:         "score",
		Instructions: "只根据这条消息描述的工作范围判断完成整个请求的复杂度，不要根据消息长度判断。",
		Criteria: []string{
			"简单：一句话就能回答，或者是单一、明确的小改动",
			"中等：需要少量推理，或一两步操作",
			"复杂：涉及多个文件、多个步骤或多个约束",
			"很复杂：跨系统、链路长、需要调研和反复验证，或者失败风险高",
		},
	},
	"task_change": {
		Type:         "noul",
		Instructions: "这条消息是否开启了一个与之前对话无关的新任务？",
		Criteria: map[string]string{
			"true":  "提出了新的、独立的目标，不依赖之前的对话也能理解要做什么",
			"false": "是对之前任务的延续：继续、确认、补充要求、追问、纠正或要求检查之前的结果",
		},
	},
	"larger_model_benefit": {
		Type:         "noul",
		Instructions: "换用能力更强的模型，是否能明显改善这条请求的结果？",
		Criteria: map[string]string{
			"true":  "需要深度推理、严谨的长链路分析或高精度输出；或者用户对之前的结果表达了不满、指出了错误、反复要求修正",
			"false": "常规能力的模型就能做好，更强的模型带来的提升很小",
		},
	},
}

// BuildState builds the classification state from the current human
// instruction and the one before it. Online routing and offline experiments
// must share it, otherwise thresholds tuned offline do not transfer.
//
// The instructions go in separate fields so user text that happens to contain
// a field name cannot blur the boundary between them. maxRunes is the content
// budget of both together. previous is capped at previousMaxRunes — task_change
// only needs its topic — and current gets whatever previous leaves unused. A
// first turn has no previous_task field.
func BuildState(current, previous string, maxRunes, previousMaxRunes int) map[string]string {
	if previous == "" {
		return map[string]string{"current_task": truncateMiddle(current, maxRunes)}
	}
	// The truncation marker is not content and does not count toward the budget.
	previousUsed := len([]rune(previous))
	if previousMaxRunes > 0 {
		previousUsed = min(previousUsed, previousMaxRunes)
	}
	currentBudget := 0 // unlimited
	if maxRunes > 0 {
		currentBudget = max(maxRunes-previousUsed, 1)
	}
	return map[string]string{
		"current_task":  truncateMiddle(current, currentBudget),
		"previous_task": truncateMiddle(previous, previousMaxRunes),
	}
}

// truncateMiddle keeps the head and tail of long text because users often
// paste content first and ask the question last. maxRunes <= 0 keeps all.
func truncateMiddle(text string, maxRunes int) string {
	runes := []rune(text)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return text
	}
	head := maxRunes * 3 / 5
	tail := maxRunes - head
	return string(runes[:head]) + "\n[...]\n" + string(runes[len(runes)-tail:])
}

// JevClient calls a jev decisions endpoint: the self-hosted /v1/systemone or
// OpenRouter's /api/alpha/decisions, which take the same state/questions body.
type JevClient struct {
	Endpoint   string
	Model      string // required by OpenRouter, ignored by the self-hosted endpoint
	APIKey     string
	HTTPClient *http.Client
}

type JevAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Noul          *float64           `json:"noul,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

type JevResponse struct {
	Model   string               `json:"model"`
	Answers map[string]JevAnswer `json:"answers"`
	Usage   map[string]any       `json:"usage,omitempty"`
	// LatencyMs is the server-side inference time reported by the
	// self-hosted endpoint; OpenRouter does not report it.
	LatencyMs *float64 `json:"latency_ms,omitempty"`

	// RequestBody and ResponseBody are the exact bytes on the wire, kept so
	// experiments can be audited and replayed.
	RequestBody  []byte `json:"-"`
	ResponseBody []byte `json:"-"`
}

// Decision is the routing view of a jev response.
type Decision struct {
	TaskType           string   `json:"task_type"`
	TaskTypeConfidence *float64 `json:"task_type_confidence,omitempty"`
	// TaskTypeProbability is the probability of the chosen task type.
	TaskTypeProbability float64 `json:"task_type_probability"`
	// Difficulty is the most likely rung, 1-based; DifficultyScore is jev's
	// probability-weighted position, 0-based.
	Difficulty         int      `json:"difficulty"`
	DifficultyScore    *float64 `json:"difficulty_score,omitempty"`
	TaskChange         *float64 `json:"task_change"`
	LargerModelBenefit *float64 `json:"larger_model_benefit"`
}

// Decision converts the response to its routing view. Without a previous
// instruction there is nothing to continue, so task_change is 1 by rule and
// the model's answer to it is ignored.
func (r *JevResponse) Decision(hasPrevious bool) Decision {
	taskType := r.Answers["task_type"]
	difficulty := r.Answers["difficulty"]
	decision := Decision{
		TaskType:            taskType.Choice,
		TaskTypeConfidence:  taskType.Confidence,
		TaskTypeProbability: taskType.Probabilities[taskType.Choice],
		DifficultyScore:     difficulty.Score,
		TaskChange:          r.Answers["task_change"].Noul,
		LargerModelBenefit:  r.Answers["larger_model_benefit"].Noul,
	}
	bestRung, bestProbability := -1, -1.0
	for key, probability := range difficulty.Probabilities {
		rung, err := strconv.Atoi(key)
		if err == nil && probability > bestProbability {
			bestRung, bestProbability = rung, probability
		}
	}
	if bestRung >= 0 {
		decision.Difficulty = bestRung + 1
	}
	if !hasPrevious {
		firstTurn := 1.0
		decision.TaskChange = &firstTurn
	}
	return decision
}

func (c *JevClient) Classify(ctx context.Context, state map[string]string) (*JevResponse, error) {
	payload := map[string]any{"state": state, "questions": questions}
	if c.Model != "" {
		payload["model"] = c.Model
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jev returned %d: %.512s", resp.StatusCode, respBody)
	}
	var result JevResponse
	if err := common.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	result.RequestBody = body
	result.ResponseBody = respBody
	return &result, nil
}
