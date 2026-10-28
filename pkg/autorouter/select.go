package autorouter

import (
	"math"
	"slices"
)

// Candidates 回答一个候选模型在当前请求下的可用性和价格，由分发中间件提供。
type Candidates struct {
	// Available 按客户端原样的请求判定；兜底路径不碰客户端的推理字段，用它。
	Available func(model string) bool
	// Overridable 撇开客户端的推理字段判定；可信的判断自己定档位，用它。
	Overridable func(model string) bool
	// Price 返回模型的混合用户价（美元/百万 token）。ok 为 false 表示没有价格或按次计价，
	// 没法按 token 比较。Price 为 nil 时不比价，按榜单顺序取第一个可用的。
	Price func(model string) (price float64, ok bool)
}

// Candidate 是质量/成本选择里的一个候选。
type Candidate struct {
	Model string  `json:"model"`
	Score float64 `json:"score"`
	Price float64 `json:"price"`
	// Utility 是分数减去价格折扣，只在候选带里的模型之间有意义。
	Utility float64 `json:"utility"`
	// Tied 表示效用与最高效用相差不超过 ScoreTieDelta，算同档。
	Tied bool `json:"tied,omitempty"`
}

// Pick 是一次质量/成本选择的过程，写进决策留痕。
type Pick struct {
	Chosen string `json:"chosen"`
	// Lambda 和 Delta 是这次比较用的 CostLambda 与 ScoreTieDelta（已限制到合法范围）。
	Lambda float64 `json:"lambda"`
	Delta  float64 `json:"delta"`
	// Band 是进入比较的候选：分数不低于最高分减去 CostLambda 与 ScoreTieDelta 之和的可用模型。
	Band []Candidate `json:"band"`
}

// pickByQualityCost 在同一任务类型的候选里一次比较质量和价格。
//
//   - 候选带：分数不低于"最高可用分 - λ - δ"。再便宜也不能比最高分低出这个范围。
//   - 价格折扣：带内按价格的对数位置打折，最便宜的折 0，最贵的折满 λ（λ=0 时不比价）。
//     用对数是因为模型价格跨好几个数量级；只在带内归一化，所以折扣不会超过 λ。
//   - 同档：折后分不低于最高折后分减去 δ 的都算同档，同档里取最便宜的，价格相同取折后分高的。
//
// scores 里没有分数的模型不参与；没有任何可用且有价格的候选时 ok 为 false。
func pickByQualityCost(models []string, scores map[string]float64, lambda, delta float64, overridable func(string) bool, price func(string) (float64, bool)) (Pick, bool) {
	scored := make([]string, 0, len(models))
	for _, model := range models {
		if _, found := scores[model]; found {
			scored = append(scored, model)
		}
	}
	slices.SortStableFunc(scored, func(a, b string) int {
		switch {
		case scores[a] > scores[b]:
			return -1
		case scores[a] < scores[b]:
			return 1
		}
		return 0
	})

	// 榜单按分数降序，第一个可用且有价格的就是最高分；之后只检查还在候选带里的模型，
	// 掉出带的模型不必再判定可用性（没有内存缓存时每次判定都是一次查询）。
	var band []Candidate
	var top float64
	for _, model := range scored {
		if len(band) > 0 && scores[model] < top-lambda-delta {
			break
		}
		if !overridable(model) {
			continue
		}
		modelPrice, priced := price(model)
		if !priced {
			continue
		}
		if len(band) == 0 {
			top = scores[model]
		}
		band = append(band, Candidate{Model: model, Score: scores[model], Price: modelPrice})
	}
	if len(band) == 0 {
		return Pick{}, false
	}

	floor, ceiling := priceRange(band)
	bestIndex := 0
	for i := range band {
		band[i].Utility = band[i].Score - lambda*logPosition(band[i].Price, floor, ceiling)
		if band[i].Utility > band[bestIndex].Utility {
			bestIndex = i
		}
	}
	// δ 为负或 NaN 时没有任何候选算同档，退回折后分最高的那个，保证总能选出一个。
	chosen := bestIndex
	tied := false
	for i := range band {
		band[i].Tied = band[i].Utility >= band[bestIndex].Utility-delta
		if !band[i].Tied {
			continue
		}
		if !tied || band[i].Price < band[chosen].Price ||
			band[i].Price == band[chosen].Price && band[i].Utility > band[chosen].Utility {
			chosen, tied = i, true
		}
	}
	return Pick{Chosen: band[chosen].Model, Lambda: lambda, Delta: delta, Band: band}, true
}

// priceRange 返回带内的价格下限和上限。免费（价格为 0）的模型不拉低下限，
// 否则一个免费模型会把其余模型的价格差都压扁；全部免费时返回 1，折扣恒为 0。
func priceRange(band []Candidate) (floor, ceiling float64) {
	floor, ceiling = math.Inf(1), 0
	for _, candidate := range band {
		if candidate.Price > 0 {
			floor = min(floor, candidate.Price)
		}
		ceiling = max(ceiling, candidate.Price)
	}
	if math.IsInf(floor, 1) {
		return 1, 1
	}
	return floor, ceiling
}

// logPosition 返回价格在 [floor, ceiling] 上的对数位置，范围 0 到 1。
func logPosition(price, floor, ceiling float64) float64 {
	if ceiling <= floor {
		return 0
	}
	return math.Log(max(price, floor)/floor) / math.Log(ceiling/floor)
}
