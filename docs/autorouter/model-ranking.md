# 智能路由任务分类与模型榜单

`model: "auto"` 的请求由 jev 判断任务类型（`task_type`）和难度（`difficulty`），网关按任务类型从模型榜单里挑模型、按难度定思考强度。任务类型的唯一用途是选榜单，所以分类从榜单反推：先确定有哪些可信的公开榜单，再按"能用不同榜单区分"来划分任务。

## 1. 设计原则

- **每个任务类型必须落到榜单上**：一个类型对应 1–3 个直接测量该类工作的公开指标。找不到对应指标的类型不单独设立，归入上级或默认画像。
- **指标集相同就合并**：两个类型选出的指标完全一样时，它们的模型排序必然相同，分开只会增加 jev 的判错机会，因此合并为一个类型。
- **少而准，不凑权重**：每个画像最多 3 个指标，等权平均。不为"覆盖更全"加入只间接相关的指标。
- **场景聚焦互联网公司**：软件开发、运维自动化、数据与业务分析、写作、日常问答。法律、医疗、金融等行业专项不设类型。
- **只用独立第三方榜单**：LiveBench、LMArena、Artificial Analysis、Design Arena，OpenRouter 只作为模型目录（价格、上下文长度、工具调用支持），自身的评分、用量、延迟都不使用。

## 2. 数据来源

| 来源 | 地址 | 取用内容 | 指标前缀 |
| --- | --- | --- | --- |
| LiveBench | `https://livebench.ai/data/<release>/table.csv` | 最新版本的 7 个大类平均分 | `lb.` |
| LMArena | `hf-mirror.com/datasets/lmarena-ai/leaderboard-dataset` 下各榜单的 `latest` parquet | 文本竞技场分类 Elo、事实性、WebDev、文档、搜索、Agent bash 可靠性 | `lma.` |
| Artificial Analysis | `GET https://artificialanalysis.ai/api/v2/language/models/free`（`x-api-key`） | Intelligence 指数，Strategy & Ops 能力指数 | `aa.` |
| Design Arena | `GET https://www.designarena.ai/api/v1/leaderboard/models/{category}`（`Authorization: Bearer sk_live_...`） | Models Arena 的 `website`、`uicomponent`、`slides` 三个分类 Elo | `da.` |
| OpenRouter 模型列表 | `GET https://openrouter.ai/api/v1/models`（无需 key） | 候选模型、价格、上下文长度、`tools` 支持、可选思考强度 | — |

LMArena 镜像与 429 退避沿用现有实现。

**LiveBench 只读最新版本**：从官网首页 bundle 列出的版本中取最新一个存在 `table.csv` 的版本，不与旧版本合并；新版本没有重跑的模型视为无 LiveBench 数据。`browser` 需要截图输入，OpenRouter 目录中不支持图像输入的模型在该项记为无数据，不按 0 分计。

Artificial Analysis 免费接口只返回指数，不含单项 benchmark 分数；按 `name` / `slug` 走第 5 节的名称匹配。Design Arena 每行带 `openRouterId`，有值时直接对应候选模型，为 `null` 时按 `displayName` 匹配。

**Design Arena 的取数方式**：设置了 `DESIGN_ARENA_API_KEY` 时直连官方接口；未设置时改读 OpenRouter `GET /api/v1/benchmarks`（需 `OPENROUTER_API_KEY`）中 `source=design-arena`、`arena=models` 的行，按 `model_permaslug` 对应候选。OpenRouter 转发的数据不含 `slides`，此时 `da.slides` 被停更检测剔除，`general.design` 只按 `lb.design` 排序。`rank_sources.design_arena` 记为 `openrouter@<as_of>` 以示区分。

## 3. 指标目录

只采集被画像用到的指标。

| 指标键 | 来源与原始字段 | 测什么 | 刻度 |
| --- | --- | --- | --- |
| `lb.long_agentic` | LiveBench `long_agentic` | 多轮变更需求下扩展业务应用、在代码库上跑模拟数月的运维、长时间编码优化 | ratio |
| `lb.coding` | LiveBench `coding` | 在冻结的大型生产代码库上按 bug 报告修复真实缺陷，引入回归记 0 分 | ratio |
| `lb.browser` | LiveBench `browser` | 通过真实浏览器在商城、看板、工单、Wiki 等 Web 应用里完成多步操作 | ratio |
| `lb.data_analysis` | LiveBench `data_analysis` | 小表格上的连续事件识别、表连接列映射、CSV/JSON/Markdown 等格式互转 | ratio |
| `lb.research` | LiveBench `research` | 按真实业务简报调研并写出带来源的报告，按评分细则打分 | ratio |
| `lb.design` | LiveBench `design` | 按要求生成或改版 Word、PDF、PPT，渲染后按视觉与结构检查打分 | ratio |
| `lb.reasoning` | LiveBench `reasoning` | 逻辑、空间、心智理论短题 | ratio |
| `da.website` | Design Arena `models/website` | 按需求生成完整网页的人类偏好 | elo |
| `da.uicomponent` | Design Arena `models/uicomponent` | 生成单个 UI 组件的人类偏好 | elo |
| `da.slides` | Design Arena `models/slides` | 生成演示文稿的人类偏好 | elo |
| `aa.intelligence` | AA `artificial_analysis_intelligence_index` | 综合智力指数 | ratio |
| `aa.strategy_ops` | AA `artificial_analysis_strategy_and_ops_index` | 商业与管理知识、业务应用中的智能体工具使用、长上下文分析 | ratio |
| `lma.overall` | `text_style_control` / `overall` | 文本对话总体人类偏好 | elo |
| `lma.coding` | `text_style_control` / `coding` | 编程类对话偏好 | elo |
| `lma.math` | `text_style_control` / `math` | 数学类对话偏好 | elo |
| `lma.multi_turn` | `text_style_control` / `multi_turn` | 多轮对话 | elo |
| `lma.expert` | `text_style_control` / `expert` | 专家级难题 | elo |
| `lma.creative_writing` | `text_style_control` / `creative_writing` | 创意写作 | elo |
| `lma.writing_industry` | `text_style_control` / `industry_writing_and_literature_and_language` | 写作、文学与语言类职业场景 | elo |
| `lma.business_industry` | `text_style_control` / `industry_business_and_management_and_financial_operations` | 商业、管理与财务运营类职业场景 | elo |
| `lma.factuality` | `text_factuality` / `overall` | 事实准确性 | elo |
| `lma.webdev` | `webdev` / `webdev` | 前端页面生成的人类偏好 | elo |
| `lma.document` | `document_style_control` / `overall` | 基于上传文档作答 | elo |
| `lma.search` | `search_style_control` / `overall` | 联网搜索作答 | elo |
| `lma.agent_bash` | `agent_bash_reliability` / `overall` | Agent 执行 bash 的可靠性 | spread |

刻度含义见第 6.1 节。

## 4. 任务分类

两级分类：3 个一级类（`code`、`data`、`general`）、11 个二级类，外加 `unknown`。一级类参照 OpenRouter 任务分类的 `code:*`、`data:*` 与通用标签划分。jev 只在二级类和 `unknown` 中单选；一级类不单独提问，它的概率等于下属二级类概率之和。

每个一级类和二级类都有自己的画像（指标集），一级类画像用于二级类不可信或二级列表无可用模型时（见第 7 节）。`general` 的一级画像就是默认画像，对应 `default_models`。

### 4.1 分类与画像

| 一级 | 二级 | jev 判定标准 | 指标（等权） |
| --- | --- | --- | --- |
| `code` 代码 | — | — | `lb.coding`，`lb.long_agentic`，`lma.agent_bash` |
| | `code.build` | 编写或修改程序逻辑与配置：实现功能、按问题报告修复缺陷、审查代码，保证改动后程序行为正确 | `lb.coding`，`lb.long_agentic`，`lma.coding` |
| | `code.frontend` | 按需求开发或修改用户界面：网页、App、小程序、桌面客户端的页面、UI 组件、样式、交互与渲染性能，以用户看到的外观与操作体验为准 | `lma.webdev`，`da.website`，`da.uicomponent` |
| | `code.ops` | 在终端或服务器上执行命令，完成部署、环境配置、服务运维与线上故障排查 | `lma.agent_bash`，`lb.long_agentic` |
| `data` 数据 | — | — | `lb.data_analysis`，`aa.strategy_ops` |
| | `data.analysis` | 对给定的表格或数据求出确定的答案：统计、筛选、去重、关联、识别趋势，或在 CSV、JSON、Markdown 等格式间转换 | `lb.data_analysis`，`lma.math` |
| | `data.business` | 运用业务与管理知识做判断：分析经营或运营数据、比较方案、制定计划，给出可执行的商业决策建议 | `aa.strategy_ops`，`lma.business_industry` |
| `general` 通用 | — | — | `aa.intelligence`，`lma.overall`，`lb.reasoning` |
| | `general.research` | 围绕需要最新信息或多方来源的问题检索、核实并整合资料，给出带来源的结论或报告 | `lb.research`，`lma.search`，`lma.factuality` |
| | `general.browser` | 在网页应用里逐步点击、填写、核对，完成跨多个页面的操作任务，如汇总多页记录、批量修改订单、合并重复工单 | `lb.browser` |
| | `general.writing` | 产出或加工文字内容：撰写文案、文章、邮件、故事、诗歌等，或对给定文本总结、改写、润色、翻译 | `lma.writing_industry`，`lma.creative_writing`，`lma.document` |
| | `general.design` | 制作或改版成品文件：Word 文档、PDF、幻灯片的版式、排版、配色、图表与品牌样式 | `lb.design`，`da.slides` |
| | `general.knowledge` | 解释概念、原理或某领域的专业事实，说明某件事是什么、为什么或如何运作 | `lma.factuality`，`lma.expert`，`aa.intelligence` |
| | `general.chat` | 日常对话与生活类求助：寒暄、闲聊、家常做法、个人化的推荐或经验分享 | `lma.overall`，`lma.multi_turn` |
| — | `unknown` | 离开前文完全无法判断要做什么：如"继续""ok""好的"、对前文方案的确认或选择、只用"这个""上面"指代前文且没有说明动作 | — |

### 4.2 划分依据

- **智能体按任务内容归类**：agent 是执行方式，不是任务内容。命令行与运维操作归 `code.ops`，浏览器操作归 `general.browser`，其余按所做的事归类。
- **`code.ops` 归入代码**：部署、配置、排障本质上是写命令和配置，`lma.agent_bash` 测的正是 agent 执行 bash 的可靠性。
- **`code.build` 同时覆盖开发、修复和审查**：公开榜单没有把这几类工作测出不同排序的指标，`lb.coding`（按 bug 报告修复真实缺陷）、`lb.long_agentic`（多轮变更需求下扩展应用）、`lma.coding`（编程对话的人类偏好）共同代表后端代码能力。
- **`code.frontend` 单列**：`lma.webdev`、`da.website`、`da.uicomponent` 都按人类对前端产出的偏好排序，与后端开发的排序差异明显。
- **`data.analysis` 与 `data.business` 分开**：前者是对数据本身的处理与计算，后者是在数据和信息上推理并给出决策（`aa.strategy_ops`）。
- **`general.research` 与 `general.knowledge` 分开**：前者要搜集外部信息并成文（`lb.research`、`lma.search`），后者直接凭模型知识回答。
- **`general.design` 用 `lb.design` 与 `da.slides`**：前者按渲染后的视觉与结构检查打分，后者是人类对幻灯片的偏好，两者都直接测量成品文档。
- **Design Arena 其余分类不用**：`gamedev`、`3d`、`svg`、`logo` 等不属于互联网公司的常规工作；`dataviz` 测的是图表页面生成，与 `data.analysis` 的统计、清洗、SQL 不是同一能力。
- **不用语言类榜单**：中文、英文等分榜按提问语言切分，测的不是任务能力，任何画像都不使用。
- **数学、逻辑题不单列**：互联网公司场景占比低，由 `general` 一级画像中的 `lb.reasoning` 覆盖。
- **difficulty、task_change、larger_model_benefit 三个问题不变。**

## 5. 候选模型与名称匹配

候选集来自 OpenRouter 模型列表，条件不变：输入含文本、输出只有文本；支持 `tools`；上下文 ≥ 128k；无 `expiration_date`；有正价格；id 不含 `:`、`~`，不以 `openrouter/` 开头。多个 id 共用同一 canonical slug 时保留最短的。

网关模型名去掉厂商前缀并改为厂商原生写法（Anthropic 版本号的点换成横杠，如 `anthropic/claude-opus-5.5` → `claude-opus-5-5`）。价格按输入 : 输出 = 3 : 1 混合，单位 USD / 1M tokens。

LiveBench、LMArena、Artificial Analysis 的模型名统一走同一套匹配：精确匹配 → 去括号、思考强度、日期后缀 → 补 `preview`、`it`、`instruct` 等发布后缀。非精确匹配输出 `alias <榜单名> -> <OpenRouter id>`，未匹配输出 `unmatched`。同一模型的多个思考强度变体匹配到同一候选时，每项指标取最好的一个。

## 6. 打分

### 6.1 归一化

每项指标在拥有该指标的候选之间归一化到 0..1：

| 刻度 | 公式 |
| --- | --- |
| ratio | `value / best` |
| elo | `2 / (1 + 10^((best - value) / 400))` |
| spread | `(value - worst) / (best - worst)` |

### 6.2 画像得分

- **分数**：画像内有数据的指标取算术平均。
- **覆盖要求**：有数据的指标数 ≥ 画像指标数的一半（向上取整）。即 1–2 个指标的画像至少有 1 项，3 个指标的画像至少有 2 项。不满足的模型不进入该画像的列表。

分数只来自榜单，不叠加用量、延迟等其他信号。

## 7. 列表与运行时选择

### 7.1 列表

`task_models` 以画像名为键，共 13 个列表（11 个二级类 + `code`、`data` 两个一级类）。每个列表包含所有满足覆盖要求的模型，按分数降序；同分时价格低的在前，再相同按 id。列表不截断，运行时取第一个当前可用的模型。

`default_models` 按 `general` 一级画像生成，同时充当 `general` 的一级列表：

1. 先放分数 ≥ 最高分 0.9 倍、且在分数/价格帕累托前沿上的模型，按价格升序；
2. 再放其余有 `general` 分数的模型，按分数降序。

### 7.2 选择顺序

jev 返回各二级类的概率。设概率最高的二级类为 `leaf`，其一级类为 `parent`；一级类的概率为其下所有二级类概率之和，`best` 为概率最高的一级类：

| 条件 | 画像 |
| --- | --- |
| `leaf` 不是 `unknown` 且 `P(leaf) ≥ min_task_type_probability` | `leaf` |
| 否则 `P(best) ≥ min_task_type_probability` | `best` |
| 否则 | 沿用上一次路由；没有上一次时用 `default_models` |

确定画像后按 `task_models[leaf] → task_models[parent] → default_models` 依次取第一个可用模型；从一级类开始时按 `task_models[best] → default_models`。`general` 没有一级列表，这一步即 `default_models`。

思考强度、`larger_model_benefit` 提档、同任务延续（`task_change < continuation_task_change` 时沿用上一次的模型、思考强度不降）的规则不变。可用性判断与 `ResolveVariant` 解析思考强度变体的规则不变。

路由日志与 trace 记录 `task_type`（jev 选中的二级类）、`route_profile`（实际使用的画像）和 `route_fallback`（`leaf` / `parent` / `default`），用于复核分类和兜底的发生频率。

## 8. 在线刷新

沿用系统任务 `autorouter_rank`：master 节点在数据库租约下按 `rank_refresh_interval_hours`（默认 24，最小 1）执行，依次写入 `task_models`、`default_models`、`rank_sources`，其他节点经配置同步生效，不需要重启。任一来源失败或任一画像没有模型时不写配置，线上保留上一份榜单。

- **启用条件**：`autorouter_setting.enabled` 与 `rank_refresh_enabled` 为 true，且进程环境变量有 `ARTIFICIAL_ANALYSIS_API_KEY`，以及 `DESIGN_ARENA_API_KEY` 或 `OPENROUTER_API_KEY` 之一。OpenRouter 模型列表是公开接口，不需要 key。
- **网络**：网关需能访问 `openrouter.ai`、`livebench.ai`、`hf-mirror.com`、`artificialanalysis.ai`、`www.designarena.ai`。不再逐模型查询端点延迟，一次刷新通常在一分钟内完成。
- **数据新鲜度**：按 OpenRouter 目录的上架时间（`created`）取每个来源评过分的最新 10 个候选（`-stale-probe`），某项指标在这 10 个里都没有数据时视为停更，本轮不参与打分，记入 `rank_sources.stale_metrics` 和刷新记录。来源评过分的候选不足 10 个时不做判断；视觉指标只看支持图像输入的候选。
- **分类版本**：`rank_sources` 记录 `taxonomy_version`。启动或配置同步时若已存的版本与代码中的分类不一致，运行时改用内置 `task_models.json`，并立即触发一次刷新，不等周期。

| 配置项 | 默认值 | 说明 |
| --- | --- | --- |
| `autorouter_setting.rank_refresh_enabled` | `true` | 是否定时刷新 |
| `autorouter_setting.rank_refresh_interval_hours` | `24` | 刷新周期，单位小时 |
| `autorouter_setting.task_models` | 内置榜单 | 画像名 → 模型列表 |
| `autorouter_setting.default_models` | 内置榜单 | 默认模型列表 |
| `autorouter_setting.rank_sources` | 内置榜单 | 各来源快照日期、生成时间、`taxonomy_version` |
| 环境变量 `ARTIFICIAL_ANALYSIS_API_KEY` | 无 | Artificial Analysis 免费接口所需；未设置时不刷新 |
| 环境变量 `DESIGN_ARENA_API_KEY` | 无 | Design Arena 接口所需（`sk_live_` 开头，经 designarena.ai/developers/apply 申请）；未设置时经 OpenRouter 读取 |
| 环境变量 `OPENROUTER_API_KEY` | 无 | 未设置 `DESIGN_ARENA_API_KEY` 时读取 OpenRouter 转发的 Design Arena 数据 |

`modelrank` 命令行同步调整：新增 `-aa-key-env`（默认 `ARTIFICIAL_ANALYSIS_API_KEY`）、`-da-key-env`（默认 `DESIGN_ARENA_API_KEY`）、`-openrouter-key-env`（默认 `OPENROUTER_API_KEY`）、`-artificial-analysis`、`-design-arena`（接口地址），去掉 `-api-key-env`；去掉 `-usage-bonus`、`-latency-tolerance`。输出文件的 `weights` 改为 `profiles`（画像名 → 指标列表），`evidence` 记录每个模型在各画像下的分数、有数据的指标数和各指标归一化值。

## 9. 代码改动

| 位置 | 改动 |
| --- | --- |
| `pkg/autorouter/jev.go` | `task_type` 选项换成 11 个二级类 + `unknown`，判定标准用第 4.1 节的文案；二级类名的前缀即一级类 |
| `pkg/autorouter/modelrank/metrics.go` | 指标目录换成第 3 节；`taskWeights` 换成画像 → 指标列表；`code`、`data` 一级画像，`general` 一级画像即默认画像；`designArenaCategories` 只保留 `website`、`uicomponent`、`slides`；删除 `usageTags`、`openRouterBenchmarks` |
| `pkg/autorouter/modelrank/sources.go` | 新增 Artificial Analysis 免费接口与 Design Arena 接口加载，无 Design Arena key 时经 OpenRouter `/benchmarks` 读取；新增指标新鲜度统计；LMArena 增加 `creative_writing` 与两个 `industry_*` 分类；删除 OpenRouter 自测 benchmark（GPQA、τ-bench）、AA 指数转发、任务用量、端点延迟的加载 |
| `pkg/autorouter/modelrank/rank.go` | 等权平均与按指标数的覆盖要求；删除用量加分；帕累托比较只看分数与价格 |
| `pkg/autorouter/policy.go` | 按第 7.2 节的顺序选画像与列表，记录 `route_profile`、`route_fallback` |
| `setting/autorouter_setting/` | 新的内置 `task_models.json`；`taxonomy_version` 校验 |
| `controller/system_task_handlers.go` | 刷新任务的启用条件改为检查 `ARTIFICIAL_ANALYSIS_API_KEY`，以及 `DESIGN_ARENA_API_KEY` 或 `OPENROUTER_API_KEY` |

## 10. 已知限制

- **小榜覆盖有限**：`lb.*` 只含最新版本跑过的模型（`20261025` 为 31 个），`lma.document`、`lma.search` 各四十余个；两指标画像只要有其中一项即可上榜；`general.browser` 只用 `lb.browser`，只覆盖 LiveBench 收录的模型。未收录模型落到一级列表。
- **Artificial Analysis 免费接口只有指数**：单项 benchmark（如 Terminal-Bench、FrontierCode）需要付费接口，因此 `aa.*` 只用 Intelligence 指数（v5）和 Strategy & Ops 能力指数。接口仍返回 `coding_index`、`agentic_index` 字段，但它们已不在 v5 指数体系中、不再随新模型更新，不使用。Intelligence v5 只覆盖 2026-03 之后发布的模型。
- **LMArena 分榜新旧不一**：文档、搜索榜可能比文本榜旧一个月以上，以 `rank_sources` 记录的日期为准。
- **分类准确率待校准**：11 个二级类的判定标准需要用标注集验证 jev 的选择准确率，`min_task_type_probability` 随之调整；一级类兜底保证二级类判错时仍落在相近的榜单上。
