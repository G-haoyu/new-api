// Command modelrank writes the autorouter ranking with its full evidence to a
// file. The gateway refreshes the ranking itself on a schedule; this command
// regenerates the mapping embedded as the first-boot default and audits a
// ranking offline.
//
//	OPENROUTER_API_KEY=... go run ./pkg/autorouter/modelrank/cmd/modelrank
//	go run ./pkg/autorouter/modelrank/cmd/modelrank -out /tmp/task_models.json -livebench-release 20261014
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/autorouter/modelrank"
)

func main() {
	cfg := modelrank.DefaultConfig()
	out := flag.String("out", "setting/autorouter_setting/task_models.json", "generated mapping file")
	apiKeyEnv := flag.String("api-key-env", "OPENROUTER_API_KEY", "env var holding the OpenRouter key")
	flag.StringVar(&cfg.OpenRouter, "openrouter", cfg.OpenRouter, "OpenRouter API base")
	flag.StringVar(&cfg.LiveBench, "livebench", cfg.LiveBench, "LiveBench site")
	flag.StringVar(&cfg.LiveBenchRelease, "livebench-release", "", "LiveBench release directory (YYYYMMDD), empty for the newest")
	flag.StringVar(&cfg.LMArena, "lmarena", cfg.LMArena, "LMArena leaderboard dataset base")
	flag.IntVar(&cfg.MinContext, "min-context", cfg.MinContext, "minimum context length of a candidate")
	flag.Float64Var(&cfg.MinCoverage, "min-coverage", cfg.MinCoverage, "minimum share of a task's metric weight a model must have data for")
	flag.Float64Var(&cfg.DefaultFloor, "default-floor", cfg.DefaultFloor, "cheap leading default models score at least this share of the best general score")
	flag.Float64Var(&cfg.UsageBonus, "usage-bonus", cfg.UsageBonus, "score bonus of the most used model of the task's OpenRouter tags")
	flag.Float64Var(&cfg.LatencyTolerance, "latency-tolerance", cfg.LatencyTolerance, "latency ratio below which two models count as equally fast")
	flag.Parse()
	cfg.APIKey = os.Getenv(*apiKeyEnv)
	cfg.Logf = func(format string, args ...any) { fmt.Printf(format+"\n", args...) }

	mapping, err := modelrank.Build(context.Background(), cfg)
	if err != nil {
		log.Fatal(err)
	}
	data, err := common.Marshal(mapping)
	if err != nil {
		log.Fatal(err)
	}
	if data, err = common.IndentJson(data); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
	for _, task := range modelrank.TaskOrder {
		fmt.Printf("%-15s %d %v\n", task, len(mapping.TaskModels[task]), mapping.TaskModels[task])
	}
	fmt.Printf("%-15s %d %v\nwrote %s\n", "default", len(mapping.DefaultModels), mapping.DefaultModels, *out)
}
