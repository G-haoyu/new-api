package modelrank

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/parquet-go/parquet-go"
)

type sources struct {
	client     *http.Client
	openRouter string
	apiKey     string
	logf       func(format string, args ...any)
}

// get makes up to attempts tries, retrying transient failures; a 429 waits
// for Retry-After, or 15s more per attempt.
func (s *sources) get(ctx context.Context, url string, attempts int) ([]byte, error) {
	var lastErr error
	wait := time.Duration(0)
	for attempt := range attempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(max(wait, time.Duration(attempt)*2*time.Second)):
			}
			wait = 0
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if s.apiKey != "" && strings.HasPrefix(url, s.openRouter) {
			req.Header.Set("Authorization", "Bearer "+s.apiKey)
		}
		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("GET %s: %s", url, resp.Status)
			if resp.StatusCode == http.StatusTooManyRequests {
				wait = time.Duration(attempt+1) * 15 * time.Second
				if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
					wait = time.Duration(seconds) * time.Second
				}
			}
			continue
		}
		return body, nil
	}
	return nil, lastErr
}

func (s *sources) getJSON(ctx context.Context, url string, v any) error {
	body, err := s.get(ctx, url, 5)
	if err != nil {
		return err
	}
	return common.Unmarshal(body, v)
}

type orModel struct {
	ID            string `json:"id"`
	CanonicalSlug string `json:"canonical_slug"`
	ContextLength int    `json:"context_length"`
	Architecture  struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	Pricing struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
	SupportedParameters []string `json:"supported_parameters"`
	ExpirationDate      *string  `json:"expiration_date"`
	Reasoning           *struct {
		SupportedEfforts []string `json:"supported_efforts"`
	} `json:"reasoning"`
}

// loadCandidates keeps OpenRouter models usable as an agent backend: text in
// and out, tool calling, enough context, priced, not scheduled for removal.
// Routing aliases (~), variants (:free, :batch) and OpenRouter's own routers
// are dropped; ids sharing a canonical slug keep the shortest.
func (s *sources) loadCandidates(ctx context.Context, minContext int) (*pool, error) {
	var resp struct {
		Data []orModel `json:"data"`
	}
	if err := s.getJSON(ctx, s.openRouter+"/models", &resp); err != nil {
		return nil, err
	}
	slices.SortFunc(resp.Data, func(a, b orModel) int {
		if d := len(a.ID) - len(b.ID); d != 0 {
			return d
		}
		return strings.Compare(a.ID, b.ID)
	})
	p := newPool()
	names := map[string]bool{}
	for _, m := range resp.Data {
		if strings.ContainsAny(m.ID, ":~") || strings.HasPrefix(m.ID, "openrouter/") ||
			!slices.Contains(m.SupportedParameters, "tools") ||
			!slices.Contains(m.Architecture.InputModalities, "text") ||
			!slices.Equal(m.Architecture.OutputModalities, []string{"text"}) ||
			m.ContextLength < minContext || m.ExpirationDate != nil {
			continue
		}
		price, err := blendedPrice(m.Pricing.Prompt, m.Pricing.Completion)
		if err != nil || price <= 0 {
			continue
		}
		name := gatewayName(m.ID)
		if p.bySlug[m.CanonicalSlug] != nil || names[name] {
			continue
		}
		names[name] = true
		c := &candidate{ID: m.ID, Slug: m.CanonicalSlug, Name: name, Price: price}
		if m.Reasoning != nil {
			c.Efforts = m.Reasoning.SupportedEfforts
		}
		p.add(c)
	}
	return p, nil
}

func (s *sources) loadBenchmarks(ctx context.Context, p *pool) (string, error) {
	var resp struct {
		Data []struct {
			Source        string   `json:"source"`
			Slug          string   `json:"model_permaslug"`
			Intelligence  *float64 `json:"intelligence_index"`
			Coding        *float64 `json:"coding_index"`
			Agentic       *float64 `json:"agentic_index"`
			Arena         string   `json:"arena"`
			Category      string   `json:"category"`
			Elo           *float64 `json:"elo"`
			BenchmarkType string   `json:"benchmark_type"`
			Accuracy      *float64 `json:"accuracy"`
		} `json:"data"`
		Meta struct {
			AsOf string `json:"as_of"`
		} `json:"meta"`
	}
	if err := s.getJSON(ctx, s.openRouter+"/benchmarks", &resp); err != nil {
		return "", err
	}
	for _, row := range resp.Data {
		c := p.bySlug[row.Slug]
		if c == nil {
			continue
		}
		set := func(metric string, v *float64) {
			if metric != "" && v != nil {
				c.record(metric, *v)
			}
		}
		switch row.Source {
		case "artificial-analysis":
			set("aa.intelligence", row.Intelligence)
			set("aa.coding", row.Coding)
			set("aa.agentic", row.Agentic)
		case "design-arena":
			if row.Arena == "models" {
				set(designArenaCategories[row.Category], row.Elo)
			}
		case "openrouter":
			set(openRouterBenchmarks[row.BenchmarkType], row.Accuracy)
		}
	}
	return resp.Meta.AsOf, nil
}

var (
	liveBenchBundle  = regexp.MustCompile(`static/js/main\.[0-9a-f]+\.js`)
	liveBenchRelease = regexp.MustCompile(`"(20\d{6})"`)
)

// loadLiveBench reads the category averages of a LiveBench release; an empty
// release takes the newest one the site's bundle names that has a table.
func (s *sources) loadLiveBench(ctx context.Context, p *pool, site, release string) (string, error) {
	var body []byte
	var err error
	if release != "" {
		body, err = s.get(ctx, site+"/data/"+release+"/table.csv", 5)
	} else {
		release, body, err = s.latestLiveBench(ctx, site)
	}
	if err != nil {
		return "", err
	}
	rows, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
	if err != nil {
		return "", err
	}
	if len(rows) < 2 || rows[0][0] != "model" {
		return "", fmt.Errorf("unexpected LiveBench table header %v", rows[0])
	}
	var unmatched []string
	for _, row := range rows[1:] {
		c := p.match(row[0])
		if c == nil {
			unmatched = append(unmatched, row[0])
			continue
		}
		for i, column := range rows[0][1:] {
			metric := "lb." + column
			if _, ok := metricScales[metric]; !ok {
				continue
			}
			if v, err := strconv.ParseFloat(row[i+1], 64); err == nil {
				c.record(metric, v)
			}
		}
	}
	s.logf("livebench %s rows=%d unmatched=%v", release, len(rows)-1, unmatched)
	return release, nil
}

// latestLiveBench finds the newest release: the site publishes no index, but
// its bundle lists the release directories as "YYYYMMDD" strings.
func (s *sources) latestLiveBench(ctx context.Context, site string) (string, []byte, error) {
	index, err := s.get(ctx, site+"/", 5)
	if err != nil {
		return "", nil, err
	}
	bundlePath := liveBenchBundle.Find(index)
	if bundlePath == nil {
		return "", nil, fmt.Errorf("livebench: no bundle in %s", site)
	}
	bundle, err := s.get(ctx, site+"/"+string(bundlePath), 5)
	if err != nil {
		return "", nil, err
	}
	var releases []string
	for _, m := range liveBenchRelease.FindAllSubmatch(bundle, -1) {
		releases = append(releases, string(m[1]))
	}
	slices.Sort(releases)
	releases = slices.Compact(releases)
	slices.Reverse(releases)
	for _, release := range releases {
		if body, err := s.get(ctx, site+"/data/"+release+"/table.csv", 1); err == nil {
			return release, body, nil
		}
	}
	return "", nil, fmt.Errorf("livebench: none of the bundle's releases %v has a table", releases)
}

type lmArenaRow struct {
	ModelName string   `parquet:"model_name"`
	Rating    *float64 `parquet:"rating,optional"`
	Score     *float64 `parquet:"score,optional"`
	Category  string   `parquet:"category"`
	Date      string   `parquet:"leaderboard_publish_date"`
}

// loadLMArena reads the latest snapshot of each LMArena board. Text boards
// carry a rating, agent boards a signed score.
func (s *sources) loadLMArena(ctx context.Context, p *pool, base string) (string, error) {
	var dates []string
	for _, board := range lmArenaBoards {
		body, err := s.get(ctx, base+"/"+board.config+"/latest-00000-of-00001.parquet", 5)
		if err != nil {
			return "", err
		}
		rows, err := parquet.Read[lmArenaRow](bytes.NewReader(body), int64(len(body)))
		if err != nil {
			return "", fmt.Errorf("%s: %w", board.config, err)
		}
		date := ""
		unmatched := map[string]bool{}
		for _, row := range rows {
			date = max(date, row.Date)
			metric, ok := board.categories[row.Category]
			if !ok {
				continue
			}
			value := row.Rating
			if value == nil {
				value = row.Score
			}
			if value == nil {
				continue
			}
			c := p.match(row.ModelName)
			if c == nil {
				unmatched[row.ModelName] = true
				continue
			}
			c.record(metric, *value)
		}
		s.logf("lmarena %s rows=%d unmatched=%d date=%s", board.config, len(rows), len(unmatched), date)
		dates = append(dates, board.config+"@"+date)
	}
	return strings.Join(dates, " "), nil
}

// loadUsage reads OpenRouter's 7-day task classification: the top models of
// each tag with their share of the tag's requests.
func (s *sources) loadUsage(ctx context.Context, p *pool) (string, error) {
	var resp struct {
		Data struct {
			AsOf            string `json:"as_of"`
			WindowDays      int    `json:"window_days"`
			Classifications []struct {
				Tag    string `json:"tag"`
				Models []struct {
					ID    string  `json:"id"`
					Share float64 `json:"tag_usage_share"`
				} `json:"models"`
			} `json:"classifications"`
		} `json:"data"`
	}
	if err := s.getJSON(ctx, s.openRouter+"/classifications/task", &resp); err != nil {
		return "", err
	}
	for _, class := range resp.Data.Classifications {
		for _, m := range class.Models {
			if c := p.bySlug[m.ID]; c != nil {
				c.Usage[class.Tag] = m.Share
			}
		}
	}
	return fmt.Sprintf("%s (%dd)", resp.Data.AsOf, resp.Data.WindowDays), nil
}

// loadLatency fills the lowest endpoint p50 latency of the last 30 minutes.
// A failed lookup leaves the latency unknown.
func (s *sources) loadLatency(ctx context.Context, list []*candidate) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, c := range list {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			var resp struct {
				Data struct {
					Endpoints []struct {
						Latency *struct {
							P50 *float64 `json:"p50"`
						} `json:"latency_last_30m"`
					} `json:"endpoints"`
				} `json:"data"`
			}
			if err := s.getJSON(ctx, s.openRouter+"/models/"+c.ID+"/endpoints", &resp); err != nil {
				s.logf("latency %s: %v", c.ID, err)
				return
			}
			best := math.Inf(1)
			for _, e := range resp.Data.Endpoints {
				if e.Latency != nil && e.Latency.P50 != nil && *e.Latency.P50 > 0 {
					best = min(best, *e.Latency.P50)
				}
			}
			if !math.IsInf(best, 1) {
				c.LatencyMs = best
			}
		})
	}
	wg.Wait()
}
