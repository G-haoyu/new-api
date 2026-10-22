# 智能路由模型榜单

`model: "auto"` 的请求由 jev 判断任务类型（`task_type`）和难度（`difficulty`），网关再按任务类型从**模型榜单**里挑模型、按难度定思考强度。本文说明这份榜单从哪来、怎么算、怎么在线更新。

相关代码：

| 位置 | 作用 |
| --- | --- |
| `pkg/autorouter/modelrank/` | 榜单生成库：拉数据、匹配模型名、打分、排序 |
| `pkg/autorouter/modelrank/cmd/modelrank/` | 命令行：离线生成带完整证据的榜单文件 |
| `controller/system_task_handlers.go` 中的 `autoRouterRankHandler` | 网关内定时刷新任务 |
| `setting/autorouter_setting/task_models.json` | 内置榜单，首次启动、尚未刷新时使用 |
| `pkg/autorouter/policy.go` | 运行时按榜单选模型 |

## 1. 数据来源

榜单只使用公开数据，不使用网关自身的流量或日志。每次生成都实时拉取下列来源，并把各来源的快照日期写进 `rank_sources`。

| 来源 | 地址 | 取用内容 | 指标键 |
| --- | --- | --- | --- |
| OpenRouter 模型列表 | `GET /api/v1/models` | 候选模型、价格、上下文长度、工具调用支持、可选思考强度 | — |
| OpenRouter benchmarks | `GET /api/v1/benchmarks`（需 key） | Artificial Analysis 三项指数；Design Arena（arena=models）Elo；OpenRouter 自测 GPQA Diamond、τ-bench airline 准确率 | `aa.*`、`da.*`、`or.*` |
| OpenRouter 任务用量 | `GET /api/v1/classifications/task`（需 key） | 近 7 天 29 个任务标签各自前 10 名模型及用量占比 | 仅用于加分 |
| OpenRouter 端点 | `GET /api/v1/models/{id}/endpoints` | 近 30 分钟各端点 p50 首 token 延迟，取最小值 | 仅用于帕累托比较 |
| LiveBench | `https://livebench.ai/data/<release>/table.csv` | 各大类平均分 | `lb.*` |
| LMArena | `hf-mirror.com/datasets/lmarena-ai/leaderboard-dataset` 下 7 个榜单的 `latest` parquet | 文本竞技场各分类 Elo、事实性、WebDev、文档、搜索、Agent 榜 | `lma.*` |

说明：

- OpenRouter benchmarks 的行以 `model_permaslug`（即模型的 canonical slug）为键，直接对应候选模型，不需要做名称匹配。
- LiveBench 没有公开的版本索引。生成器读取官网首页引用的 `static/js/main.<hash>.js`，取出其中所有 `"YYYYMMDD"` 形式的版本目录，从新到旧尝试，选第一个存在 `table.csv` 的版本。也可以用 `-livebench-release` 固定某个版本。
- LMArena 原始数据托管在 Hugging Face。由于 `huggingface.co` 在内网不可达，默认走 `hf-mirror.com` 镜像。镜像会限流（429），生成器按 `Retry-After` 退避，最多重试 5 次。

LMArena 读取的榜单与分类：

| 榜单（config） | 分类 → 指标 |
| --- | --- |
| `text_style_control` | overall → `lma.overall`，coding → `lma.coding`，hard_prompts → `lma.hard_prompts`，math → `lma.math`，instruction_following → `lma.if`，multi_turn → `lma.multi_turn`，longer_query → `lma.longer_query`，chinese → `lma.chinese`，expert → `lma.expert` |
| `text_factuality` | overall → `lma.factuality` |
| `webdev` | webdev → `lma.webdev` |
| `document_style_control` | overall → `lma.document` |
| `search_style_control` | overall → `lma.search` |
| `agent` | overall → `lma.agent` |
| `agent_bash_reliability` | overall → `lma.agent_bash` |

## 2. 候选模型

候选集来自 OpenRouter 模型列表，必须同时满足：

- 输入包含文本，输出只有文本；
- `supported_parameters` 包含 `tools`（能做 agent 后端）；
- 上下文长度 ≥ 128k（`-min-context`）；
- 没有 `expiration_date`（不在下线计划中）；
- 有正价格；
- id 不含 `:`（`:free`、`:batch` 等变体）和 `~`（路由别名），也不以 `openrouter/` 开头（OpenRouter 自己的路由器）。

多个 id 共用同一个 canonical slug 时，保留最短的那个。

**网关模型名**：去掉 OpenRouter id 的厂商前缀，并改成厂商原生 API 的写法。目前只有 Anthropic 需要转换：版本号中的点换成横杠，例如 `anthropic/claude-opus-5.5` → `claude-opus-5-5`；其余厂商与 OpenRouter 一致，例如 `openai/gpt-6.1-sol` → `gpt-6.1-sol`。榜单里写的是基础名。带思考强度后缀的变体（如 `gpt-5-high`）由运行时自动解析，见第 6 节。

**价格**：输入、输出单价按 3 : 1 混合，单位为 USD / 1M tokens。

## 3. 排行榜名称匹配

LiveBench 和 LMArena 用的是各自的模型名，例如 `Claude Opus 5.5 (Max)`、`DeepSeek-V4-Pro (High, 20260813)`、`o4-mini (2025-04-16)`。按下面的顺序匹配到候选模型：

1. **精确匹配**：小写，取最后一个 `/` 之后的部分，去掉 `:` 之后的部分，只保留字母和数字，再与候选的 id 和 canonical slug 比较。slug 带快照日期，所以带日期的名称能命中对应快照。
2. **去后缀匹配**：先去掉括号内容，再去掉末尾的思考强度、`thinking`、`reasoning`、日期等后缀。`max` 同时也是产品档位名（如 `qwen3.8-max`），只在第二轮才去掉。如果名称里带日期，优先匹配"基础名 + 日期"的快照。
3. **补发布后缀**：对第 1 步得到的基础名，依次尝试补上 OpenRouter id 常带、而排行榜名称常省略的尾巴：`preview`、`it`、`instruct`、`0`、`exp`、`latest`。

所有非精确匹配都会以 `alias <榜单名> -> <OpenRouter id>` 的形式输出，便于人工复核。匹配不上的名称也会输出（`unmatched`），不参与计算。

同一模型的多个思考强度变体（如 `-low`、`-high`）匹配到同一个候选时，每项指标取最好的一个，因为思考强度由路由器另行选择。

## 4. 打分

### 4.1 归一化

每项指标在拥有该指标的候选之间归一化到 0..1：

| 刻度 | 适用指标 | 公式 |
| --- | --- | --- |
| ratio | 准确率、指数类：`aa.*`、`or.*`、`lb.*` | `value / best` |
| elo | 竞技场评分：`da.*`、`lma.*`（agent 两项除外） | `2 / (1 + 10^((best - value) / 400))`，即对最强模型的期望胜率相对五五开的比值 |
| spread | 有正负号的系数：`lma.agent`、`lma.agent_bash` | `(value - worst) / (best - worst)` |

### 4.2 任务画像

每个 jev 任务类型对应一组指标权重（每组权重之和为 1）：

| 任务 | 指标与权重 |
| --- | --- |
| coding | aa.coding .30，lb.coding .25，lma.coding .15，lma.webdev .15，lb.long_agentic .10，da.website .05 |
| debugging | aa.coding .30，lb.coding .20，lb.long_agentic .15，lma.agent_bash .15，lb.reasoning .10，lma.coding .10 |
| review | aa.coding .30，lb.coding .25，lb.reasoning .20，lma.coding .15，lma.hard_prompts .10 |
| operations | aa.agentic .30，or.tau .20，lma.agent_bash .20，lb.long_agentic .15，lma.agent .15 |
| research | lb.research .25，aa.intelligence .20，or.gpqa .15，lma.search .15，lma.expert .15，lb.browser .10 |
| data_operation | lb.data_analysis .40，aa.intelligence .15，lma.math .15，lma.if .15，da.dataviz .15 |
| summarization | lma.document .25，lma.longer_query .25，lma.if .20，lma.chinese .15，lma.multi_turn .15 |
| direct_qa | lma.overall .25，lma.factuality .20，or.gpqa .20，aa.intelligence .15，lma.chinese .10，lma.hard_prompts .10 |
| general（默认列表） | aa.intelligence .30，lma.overall .25，lb.reasoning .15，aa.coding .15，lma.if .15 |

### 4.3 质量分与覆盖率

- **覆盖率**：模型有数据的指标权重之和。覆盖率 < 0.5（`-min-coverage`）的模型不参与该任务的排名，避免只上过一个小榜的模型凭单项分数排到前面。
- **质量分**：在有数据的指标上做加权平均，即 `Σ(w·v) / 覆盖率`。

### 4.4 用量加分

OpenRouter 任务分类给出每个标签前 10 名模型的用量占比。每个任务类型映射到若干标签：

| 任务 | OpenRouter 标签 |
| --- | --- |
| coding | code:general_impl，code:frontend_ui，code:file_read_write，code:repo_scan |
| debugging | code:debugging |
| review | code:review_security，security_audit |
| operations | code:shell_execution，code:devops_config，devops，agent:workflow_execution，agent:tool_dispatch |
| research | research_report，agent:web_search |
| data_operation | data:extraction，data:transformation，code:sql_database |
| summarization | summarization，translation |
| direct_qa | qa_knowledge，conversational_reply |

模型在这些标签中的最大占比，除以该任务用量最高模型的占比，再乘以 0.03（`-usage-bonus`），作为加分。加分很小，只在质量相近的模型之间起排序作用。

最终分 = 质量分 + 用量加分。

## 5. 排序与选取

### 5.1 任务列表（`task_models`）

每个任务包含**所有**覆盖率达标的模型，按最终分从高到低排；同分时价格低的在前，再相同按 id 排。

列表不截断，原因是运行时取列表中第一个"当前令牌和分组可用"的模型。列表越完整，无论部署了哪些模型，任务都能落到"已部署模型中该任务排名最高的那个"，而不会因为头部几个模型没部署就退回默认列表。

**支配关系**：A 的分数、价格、延迟都不差于 B，且至少一项更好，则 A 支配 B。延迟只在双方都已知、且相差超过 1.5 倍（`-latency-tolerance`）时才参与比较，因为它只是 30 分钟快照，波动较大。按分数降序时，支配者一定排在被支配者前面，所以被支配的模型只在更优的模型都不可用时才会被选中。

### 5.2 默认列表（`default_models`）

用于以下情况：jev 不可用或结果不可信、任务类型为 unknown、任务列表中没有任何可用模型。按 general 画像排序：

1. 先放分数 ≥ 最高分 0.9 倍（`-default-floor`）、且在分数/价格/延迟帕累托前沿上的模型，按价格从低到高。这样兜底流量会落到"足够强里最便宜"的模型上。
2. 再放其余所有有 general 分数的模型，按分数降序。

这样只要部署了任意一个上榜模型，`auto` 就能路由出去。

### 5.3 证据

`modelrank` 命令行生成的文件除路由用的三个字段外，还包含：

- `weights`：各画像的指标权重；
- `evidence`：每个画像下全部上榜模型的 `score`、`quality`、`coverage`、`usage`，以及是否在前沿上（`frontier`）；
- `models`：每个模型的 OpenRouter id、混合价格、p50 延迟、可选思考强度、各项原始指标值。

用这些字段可以复核任何一个模型为什么排在它所在的位置。

## 6. 运行时如何使用榜单

`pkg/autorouter/policy.go` 的 `SelectRoute`：

1. jev 结果可信时，按 `difficulty_efforts` 把难度换成思考强度。若 `larger_model_benefit ≥ upgrade_benefit`，再提一档。
2. 若属于同一任务的延续（`task_change < continuation_task_change`），且上一次用的模型仍可用，就沿用该模型，思考强度不降。
3. 否则从 `task_models[task_type]` 取第一个可用模型；没有可用的就从 `default_models` 取。
4. jev 失败或结果不可信时，沿用上一次的路由；没有上一次就从 `default_models` 取，思考强度用 `default_effort`。

"可用"指的是：令牌模型白名单允许，且当前分组有启用的渠道支持该模型和请求路径。判断时，基础名本身或任一思考强度变体（`<model>-low`、`-medium`、`-high`、`-xhigh` 等）存在即可。最终发往上游的名称由 `ResolveVariant` 决定：

- 优先用 `<model>-<effort>`；
- 其次用基础名（即厂商默认思考强度）；
- 再次用最接近的思考强度，距离相同时取较强的一档。

可用性检查在单个请求内会缓存结果。未开启内存缓存时，会先用一次查询取出各分组已启用的模型集合，集合之外的名称直接跳过，不逐个查数据库。

## 7. 动态更新

### 7.1 定时刷新（默认开启）

网关内置系统任务 `autorouter_rank`，基于现有系统任务框架，用数据库租约保证多节点同一时刻只有一个节点执行。

- **启用条件**：`autorouter_setting.enabled` 和 `autorouter_setting.rank_refresh_enabled` 都为 true，且进程环境变量里有 `OPENROUTER_API_KEY`。
- **周期**：`autorouter_setting.rank_refresh_interval_hours`，默认 24 小时，最小 1 小时。距上一次运行满一个周期就会创建新任务；从未运行过时，首次满足启用条件就立即运行。
- **执行**：调用 `modelrank.Build` 拉取全部来源并排序，然后依次写入 `autorouter_setting.task_models`、`autorouter_setting.default_models`、`autorouter_setting.rank_sources` 三个配置项。
  - 执行节点立即生效。
  - 其他节点通过配置同步（`SYNC_FREQUENCY`，默认 60 秒）生效。
  - 全程不需要重启服务。
- **失败处理**：任一来源失败，或任一任务没有可用模型时，任务记为失败，不写任何配置，线上继续使用上一份榜单。下一个周期会重试。`rank_sources` 最后写入，它的 `generated_at` 更新了，就表示这一轮三项都已写完。
- **记录**：每次运行在"系统任务"中留一条记录。结果里包含 `rank_sources`、各任务的模型数和前 3 名、默认列表长度；失败时记录错误原因。

网关进程需要能访问 `openrouter.ai`、`livebench.ai`、`hf-mirror.com`。一次完整刷新的耗时主要花在为每个上榜模型查询端点延迟上（并发 8），通常几分钟。

### 7.2 手动修改

`autorouter_setting.task_models` 和 `default_models` 是普通配置项，可以通过 `PUT /api/option/` 或管理后台修改，修改即时生效。注意：开启定时刷新时，下一次刷新会覆盖手动修改。如果要长期固定一份手工榜单，需要把 `rank_refresh_enabled` 设为 false。

### 7.3 配置项一览

| 配置项 | 默认值 | 说明 |
| --- | --- | --- |
| `autorouter_setting.rank_refresh_enabled` | `true` | 是否定时刷新榜单 |
| `autorouter_setting.rank_refresh_interval_hours` | `24` | 刷新周期，单位小时 |
| `autorouter_setting.task_models` | 内置榜单 | 任务类型 → 模型列表 |
| `autorouter_setting.default_models` | 内置榜单 | 默认模型列表 |
| `autorouter_setting.rank_sources` | 内置榜单 | 当前榜单的各来源快照日期和生成时间 |
| 环境变量 `OPENROUTER_API_KEY` | 无 | 访问 OpenRouter benchmarks / classifications 所需；未设置时不刷新 |

## 8. 命令行

```bash
# 重新生成内置榜单（带完整证据）
OPENROUTER_API_KEY=... go run ./pkg/autorouter/modelrank/cmd/modelrank

# 输出到别处、固定 LiveBench 版本
OPENROUTER_API_KEY=... go run ./pkg/autorouter/modelrank/cmd/modelrank \
  -out /tmp/task_models.json -livebench-release 20261014
```

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-out` | `setting/autorouter_setting/task_models.json` | 输出文件 |
| `-api-key-env` | `OPENROUTER_API_KEY` | 存放 OpenRouter key 的环境变量名 |
| `-openrouter` | `https://openrouter.ai/api/v1` | OpenRouter API 地址 |
| `-livebench` | `https://livebench.ai` | LiveBench 站点 |
| `-livebench-release` | 空（取最新） | LiveBench 版本目录 |
| `-lmarena` | hf-mirror 上的 LMArena 数据集地址 | LMArena 数据地址 |
| `-min-context` | `128000` | 候选模型最小上下文长度 |
| `-min-coverage` | `0.5` | 参与排名所需的最小覆盖率 |
| `-default-floor` | `0.9` | 默认列表头部"便宜且强"模型的分数下限（相对最高分） |
| `-usage-bonus` | `0.03` | 用量加分上限 |
| `-latency-tolerance` | `1.5` | 延迟相差多少倍以内视为一样快 |

运行时会输出各来源行数、未匹配名称、所有非精确匹配（`alias`），以及每个任务的模型数和列表，便于在更新内置榜单前人工复核。

## 9. 已知限制

- **模型命名**：网关模型名按"厂商原生写法"推断（见第 2 节）。如果渠道里配置的名称与之不同（例如加了自定义前缀），这些模型不会被选中，需要在渠道里配置模型重定向，或调整名称规则。
- **带日期的快照**：OpenRouter 上同时存在"不带日期"和"带日期"的同一模型时，两者都会出现在榜单里（如 `deepseek-v4.1-flash` 和 `deepseek-v4.1-flash-1017`），哪个在渠道里配置了就用哪个。
- **延迟**：取自 30 分钟快照，只影响帕累托判断和默认列表头部的选择；任务列表主要按分数排序，延迟不改变其中的大顺序。
- **数据新旧**：各来源的更新频率不同。LMArena 文档榜可能比其他榜单旧一个月，以 `rank_sources` 中记录的日期为准。
- **未收录模型**：排行榜上没有足够数据（覆盖率 < 0.5）的新模型，不会出现在对应任务的列表里，等榜单收录后会在下次刷新时自动进入。
