package modelrank

// scale says how a raw metric becomes a 0..1 score across the candidate pool.
type scale int

const (
	// ratio metrics are accuracies or indexes where zero means no capability:
	// value / best.
	ratio scale = iota
	// elo metrics are arena ratings: the expected score against the best model
	// relative to an even match, 2 / (1 + 10^((best-value)/400)).
	elo
	// spread metrics are signed arena coefficients: (value-worst) / (best-worst).
	spread
)

// metricScales lists every metric the generator collects. Keys are
// <source>.<metric>: aa = Artificial Analysis, da = Design Arena, or =
// OpenRouter's own benchmark runs, lb = LiveBench, lma = LMArena.
var metricScales = map[string]scale{
	"aa.intelligence":  ratio,
	"aa.coding":        ratio,
	"aa.agentic":       ratio,
	"or.gpqa":          ratio,
	"or.tau":           ratio,
	"da.website":       elo,
	"da.uicomponent":   elo,
	"da.dataviz":       elo,
	"lb.long_agentic":  ratio,
	"lb.reasoning":     ratio,
	"lb.coding":        ratio,
	"lb.browser":       ratio,
	"lb.data_analysis": ratio,
	"lb.research":      ratio,
	"lma.overall":      elo,
	"lma.coding":       elo,
	"lma.hard_prompts": elo,
	"lma.math":         elo,
	"lma.if":           elo,
	"lma.multi_turn":   elo,
	"lma.longer_query": elo,
	"lma.chinese":      elo,
	"lma.expert":       elo,
	"lma.factuality":   elo,
	"lma.webdev":       elo,
	"lma.document":     elo,
	"lma.search":       elo,
	"lma.agent":        spread,
	"lma.agent_bash":   spread,
}

// designArenaCategories maps Design Arena categories to metrics.
var designArenaCategories = map[string]string{
	"website":     "da.website",
	"uicomponent": "da.uicomponent",
	"dataviz":     "da.dataviz",
}

// openRouterBenchmarks maps OpenRouter benchmark types to metrics.
var openRouterBenchmarks = map[string]string{
	"gpqa_diamond":               "or.gpqa",
	"tau_bench_verified_airline": "or.tau",
}

// lmArenaBoard is one LMArena leaderboard config and the metric each of its
// categories feeds.
type lmArenaBoard struct {
	config     string
	categories map[string]string
}

var lmArenaBoards = []lmArenaBoard{
	{"text_style_control", map[string]string{
		"overall":               "lma.overall",
		"coding":                "lma.coding",
		"hard_prompts":          "lma.hard_prompts",
		"math":                  "lma.math",
		"instruction_following": "lma.if",
		"multi_turn":            "lma.multi_turn",
		"longer_query":          "lma.longer_query",
		"chinese":               "lma.chinese",
		"expert":                "lma.expert",
	}},
	{"text_factuality", map[string]string{"overall": "lma.factuality"}},
	{"webdev", map[string]string{"webdev": "lma.webdev"}},
	{"document_style_control", map[string]string{"overall": "lma.document"}},
	{"search_style_control", map[string]string{"overall": "lma.search"}},
	{"agent", map[string]string{"overall": "lma.agent"}},
	{"agent_bash_reliability", map[string]string{"overall": "lma.agent_bash"}},
}

// TaskOrder is the jev task types the mapping covers; unknown is served by
// the default models.
var TaskOrder = []string{"coding", "debugging", "review", "operations", "research", "data_operation", "summarization", "direct_qa"}

// generalProfile scores models for the default list.
const generalProfile = "general"

// taskWeights is the metric mix scoring each task type. Weights of a task sum
// to 1; a model's score is the weighted mean over the metrics it has data for.
var taskWeights = map[string]map[string]float64{
	"coding": {
		"aa.coding": 0.30, "lb.coding": 0.25, "lma.coding": 0.15, "lma.webdev": 0.15,
		"lb.long_agentic": 0.10, "da.website": 0.05,
	},
	"debugging": {
		"aa.coding": 0.30, "lb.coding": 0.20, "lb.long_agentic": 0.15, "lma.agent_bash": 0.15,
		"lb.reasoning": 0.10, "lma.coding": 0.10,
	},
	"review": {
		"aa.coding": 0.30, "lb.coding": 0.25, "lb.reasoning": 0.20, "lma.coding": 0.15,
		"lma.hard_prompts": 0.10,
	},
	"operations": {
		"aa.agentic": 0.30, "or.tau": 0.20, "lma.agent_bash": 0.20, "lb.long_agentic": 0.15,
		"lma.agent": 0.15,
	},
	"research": {
		"lb.research": 0.25, "aa.intelligence": 0.20, "or.gpqa": 0.15, "lma.search": 0.15,
		"lb.browser": 0.10, "lma.expert": 0.15,
	},
	"data_operation": {
		"lb.data_analysis": 0.40, "aa.intelligence": 0.15, "lma.math": 0.15, "lma.if": 0.15,
		"da.dataviz": 0.15,
	},
	"summarization": {
		"lma.document": 0.25, "lma.longer_query": 0.25, "lma.if": 0.20, "lma.chinese": 0.15,
		"lma.multi_turn": 0.15,
	},
	"direct_qa": {
		"lma.overall": 0.25, "lma.factuality": 0.20, "or.gpqa": 0.20, "aa.intelligence": 0.15,
		"lma.chinese": 0.10, "lma.hard_prompts": 0.10,
	},
	generalProfile: {
		"aa.intelligence": 0.30, "lma.overall": 0.25, "lb.reasoning": 0.15, "aa.coding": 0.15,
		"lma.if": 0.15,
	},
}

// usageTags are the OpenRouter task classification tags of each task type.
var usageTags = map[string][]string{
	"coding":         {"code:general_impl", "code:frontend_ui", "code:file_read_write", "code:repo_scan"},
	"debugging":      {"code:debugging"},
	"review":         {"code:review_security", "security_audit"},
	"operations":     {"code:shell_execution", "code:devops_config", "devops", "agent:workflow_execution", "agent:tool_dispatch"},
	"research":       {"research_report", "agent:web_search"},
	"data_operation": {"data:extraction", "data:transformation", "code:sql_database"},
	"summarization":  {"summarization", "translation"},
	"direct_qa":      {"qa_knowledge", "conversational_reply"},
}
