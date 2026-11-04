# 复用调研：MarkToast 能从开源记忆生态吸收什么

> 日期：2026-11-04。目标：回答"市面上开源记忆方案，哪些能复用、以什么方式复用、哪些不能碰"。
> 结论按四档标注：**直接依赖 / sidecar / 设计参考 / 避免**。许可证全部核实到 SPDX 并读过 LICENSE；
> 实验在本机真实执行，输出见文末。详细原始报告：`~/.cache/dsh-memory-research/reuse-{storage,retrieval,integration}.md`。

## 0. TL;DR：采纳清单

| 组件 | 许可证 | 复用方式 | 用在 MarkToast 哪里 | 版本 |
|---|---|---|---|---|
| **modernc.org/sqlite** | BSD-3-Clause | 直接依赖 | `internal/index`（FTS5 派生索引） | v1.61.1 |
| **go-ego/gse** | Apache-2.0 | 可选依赖 | `internal/index` 中文分词（`Tokenizer` 实现之一） | v1.3.1 |
| **mark3labs/mcp-go**（备选）/ **官方 MCP Go SDK** | MIT / Apache-2.0 AND MIT | 直接依赖 | `internal/hub` MCP server（stdio + streamable HTTP） | v1.5.0 / v1.8.0 |
| **自研 bigram tokenizer** | —（本项目） | 自研 | 无词典、零依赖的中文快路径（CLI / 兜底） | — |
| **deja-vu**（vshulcz） | MIT | **参考/可移植** | 会话读取器（43 agents，含 ZCode/Hermes/DSH）、`cjkfold` 繁简折叠、`atomicfile` | HEAD |
| **ai-memory**（akitaonrails） | MIT | 设计参考 + v2 sidecar 插槽 | retention/decay 公式、单写者 + 可重建索引结构、MCP 工具命名 | 15k★ |
| **Letta memfs**（letta-ai） | Apache-2.0 | 设计参考 | 根 `MEMORY.md` + 子目录 `MEMORY.md`、git 冲突状态机、attempt token | — |
| **EverOS** | Apache-2.0 (+NOTICE) | 设计参考 | `.index/` 可整删重建、LSN 崩溃重放、scope 编进路径 | 13.8k★ |
| **Hermes memory provider 接口** | MIT | 设计参考 / 后续实现 | `internal/adapter` 的 provider 生命周期、`prefetch/sync_turn/tools` | 本机源码 |
| **ZCode 项目记忆格式** | Apache-2.0 | 格式采用 | `internal/store`（`MEMORY.md` + 一事实一文件 + frontmatter） | 已采用 |
| **gitleaks 规则集** | MIT | 参考/移植规则 | 写入前脱敏（M2） | — |
| **Wails v3** | MIT | 直接依赖 | M4 桌面壳 | nightly |
| **moejs** | Apache-2.0 | 直接依赖（M5） | 适配器 JS 插件宿主 | 429★ |
| **Bleve v2 + CJK analyzer** | Apache-2.0 | 备选 | 若 FTS5 能力不足时的第二索引 | v2.6.2 |
| **sqlite-vec + bge-small-zh-v1.5** | Apache-2.0 / MIT | v2 可选 | 语义混合检索（RRF） | 需实测 |

**红线（不得进入二进制）**：basic-memory（AGPL-3.0）、OpenViking（AGPL-3.0）。
**明确不引入**：mem0（Python 重栈 + 云端 MCP + telemetry）、Graphiti/Zep（依赖 Neo4j）、cognee/MemOS/Memobase（图/向量基础设施或停更）。

## 1. 一句话结论

**"直接依赖"档几乎没有可用的记忆引擎——记忆层必须自研；真正能吸收的是"算法、格式、协议、读取器"，
以及少量高质量的 Go 工具库（SQLite、MCP SDK、分词、CJK 折叠、会话读取）。**
这也正是 MarkToast 的差异化：没有一家在 SQLite 里把中文记忆检索做对。

## 2. 存储 / 引擎层（A 报告要点）

| 候选 | 许可证 | 结论 | 关键理由 |
|---|---|---|---|
| ai-memory | MIT | **设计参考**（v2 可 sidecar） | 唯一与 MarkToast 同构（Markdown 真源 + SQLite 派生 + Git）；Rust 单二进制；retention 公式可直接移植 Go；sidecar 会造成双真源，v2 才考虑 |
| Letta Code memfs | Apache-2.0 | **设计参考** | 根/子目录 `MEMORY.md`、`skills/` 不投影、冲突四态（`MERGE_HEAD` 等）+ attempt token，正对 Git 工作流 |
| EverOS | Apache-2.0 + NOTICE | **设计参考** | `.index/` 可整删重建、`md_change_state` + LSN 重放、scope 路径化；**带 NOTICE，移植代码必须随附** |
| mem9 | Apache-2.0 | 设计参考 | adapter YAML 字段与 per-harness 插件布局（Go，但绑 TiDB/PG） |
| memsearch | MIT | 设计参考 | Markdown→派生索引的增量流水线 |
| basic-memory | **AGPL-3.0** | **避免**（只读概念） | 代码/链接均不可；observations/relations 概念可参考 |
| OpenViking | **AGPL-3.0** | **避免** | 星再多也不能碰代码；只能看接口设计 |
| mem0 | Apache-2.0 | **避免** | 本地 OpenMemory 已消失，MCP 云端化；Python + qdrant + posthog；ADD-only 与可回滚冲突 |
| Graphiti / Zep | Apache-2.0 | **避免（v1）** | 核心依赖 `neo4j>=5.26`，等于多养一个图库 |
| cognee / MemOS / Memobase | Apache-2.0 | **避免** | Python 图/向量基础设施；Memobase 2026-01 后停更 |

**可吸收的两个算法/结构**：

1. **ai-memory retention 公式**（MIT，可移植 Go）：
   `salience·e^(−λ·age) + σ·ln(1+access_count)·e^(−μ·days_since_access)`，默认 λ=0.02、σ=0.6、μ=0.04、cold=0.20、硬删 180 天；
   分层 λ（semantic/procedural 不衰减、pinned 豁免）；**只用于 sweep，不参与检索排序**。
2. **单写者 + 可重建索引**：`wiki/`（Git 真源）+ SQLite WAL + 4 连接只读池；`.index/` 可整目录删除重建；
   外部改动（用户 vim / 别的 agent）走"盘点 → 重索引 → 提交"，不能只依赖 fsnotify。

## 3. 检索层（B 报告 + Lead 实验）

### 3.1 实测结论（本机 Go 1.26.5 / macOS）

- **modernc.org/sqlite v1.61.1 默认带 FTS5**，可直接 `CREATE VIRTUAL TABLE ... USING fts5`。
- **不能注册 Go 侧自定义 FTS5 tokenizer**：公共 API 没有 `RegisterTokenizer`（编译报 `undefined: sqlite.RegisterTokenizer`），
  SQL 里 `tokenize='jieba'` 报 `no such tokenizer: jieba`。要自定义 tokenizer 只能 CGO/原生 SQLite/loadable extension。
- **trigram 的硬缺口**：两字中文查询 0 命中（实测 "微信"、"支付"），三字起命中；英文子串可命中。
- **预分词 + unicode61 可行**：中文两字/多字均命中。
- **bigram 预分词（Lead 实验）**：把汉字连续段切成重叠 bigram（"启用微信支付" → `启用 用微 微信 信支 支付`），
  查询侧同样处理，`unicode61` + FTS5：两字 "微信"、"支付"、"回滚"、"闪屏" 全部命中，英文 "deploy" 也命中。
  **不需要词典、不需要 CGO、不需要 7 秒加载时间**，是 CLI 短进程的快路径候选。
- **gse v1.3.1（Apache-2.0，纯 Go）**：词级分词质量好（"发版后 首页 闪屏"），
  但冷启动加载词典约 **7 秒**（实测），适合常驻 hub，不适合每次 CLI 调用。
- **gojieba（MIT）**：分词最快但需要 CGO；作为质量对照/备选。
- **Bleve v2 + `analysis/lang/cjk`（Apache-2.0）**：CJK bigram 与我们的 SQLite bigram 等价，
  但索引/查询两侧都必须用 cjk analyzer（否则 0 命中），同一字段中文 analyzer 会吞掉英文（需双字段）；
  引入第二套索引，v1 不做，作为 FTS5 能力不足时的备选。

### 3.2 排序与查询

- **不要默认全词 AND**：40 条自然语言查询在三种方案下都因"所有词必须命中"而 0 命中；
  改为词项 **OR + BM25 排序** 后，gse/gojieba 达到 **R@5 95%**（40 条真实中文查询、495 篇语料）。
- 排序：`lexical = -bm25(name*5, description*2, body*1)` + 精确名称/短语加分 + 时间衰减 + type boost；
  权重最终以 MarkToast 自己的 holdout eval 为准。
- 回退顺序：**gse 预分词 → trigram（≥3 字符）→ 两字 bigram 影子/有界 `LIKE` → gojieba（CGO）→ Bleve**。

### 3.3 v2 语义层

- **sqlite-vec**（Apache-2.0）需确认 modernc 内置支持（B 报告称 v1.61.1 已内置 transpile 版 sqlite-vec 0.1.9，待实测）；
- embedding 首选 **bge-small-zh-v1.5**（MIT，中文短文本，体积小）；跨语言/长文再上 bge-m3；
- 混合排序用 RRF（k=60），FTS/vector 各取 top50；模型或索引失败自动退回 v1 FTS。

## 4. 接入层（C 报告 + Lead 核查）

### 4.1 MCP SDK

| SDK | 许可证 | 版本 | 结论 |
|---|---|---|---|
| **modelcontextprotocol/go-sdk**（官方） | Apache-2.0 AND MIT（过渡期混合） | v1.8.0（2026-09-04） | 5.3k★，pushed 2026-11-03 | **v1 首选**：官方规范实现；stdio + Streamable HTTP（`mcp/streamable.go`）、多 client（`mcp/server.go`）、session/header/token metadata、middleware——正好支撑"一个 hub 服务多个 harness + 每 harness 独立 token/scope"；混合许可需保留双声明 |
| mark3labs/mcp-go | MIT | v1.5.0（2026-10-30） | 9.2k★，pushed 2026-11-03 | 备选：MIT 单一许可更干净，`server/streamable_http.go` 成熟；若不想处理混合许可可换它 |

MarkToast 的 hub 用一个进程同时提供 stdio（给 CLI 型 harness）和 streamable HTTP（给常驻 harness）；
每个 harness 一个 client token，工具面固定为 `memory_search` / `memory_get` / `memory_save` / `memory_list`。
服务端在 middleware 里做 bearer 认证，按 `harness/project/scope` 授权；stdio 用一次性 token（环境变量），
不要写进普通客户端配置。

### 4.2 三家 harness 的最省力路径

| harness | 官方扩展点 | MarkToast 接入（MVP） | 复用点 |
|---|---|---|---|
| **ZCode**（Apache-2.0） | hooks（`SessionStart` / `UserPromptSubmit` / `PreToolUse` / `PostToolUse` / `Stop` 等 7 事件，JSON stdin/stdout）、AGENTS.md、MCP | **SessionStart 注入索引 + Stop/PostToolUse 捕获候选 + MCP 工具** | 事件 schema 直接照用；hooks 文档 `apps/zcode-cli/docs/usage/09-hooks.md` |
| **Hermes**（MIT） | memory provider 插件（`agent/memory_provider.py`：`system_prompt_block`/`prefetch`/`sync_turn`/`handle_tool_call`/`on_session_end` 等 22 个方法）、MCP client、hooks | 先 MCP+指令投影；M2 实现 provider 插件（长文记忆不受 2200 字符内置预算限制） | provider 生命周期设计（MIT，可参考/重写） |
| **DSH**（MIT） | 插件（`cordis.patch.yml`）、MCP client（`@deepseek-ai/dsh-mcp-client`）、hooks-claude-code 桥 | **MCP + AGENTS.md marker 块注入**；插件包（M2） | 插件补丁格式与 MCP client 配置 |

后续 Codex / Claude Code / Pi：统一走 **指令文件投影 + MCP**（最通用），hooks 只在值得时加。

### 4.3 会话读取 / 格式转换

- **deja-vu（MIT，Go）** 是最大惊喜：`internal/sources/` 有 498 个文件、覆盖 43 个 agent（含 ZCode、Hermes、DSH）的会话读取器；
  直接移植需要的 reader 并在 `docs/CREDITS.md` 署名，比自研快得多。
- 另有 `internal/cjkfold`（繁→简折叠，保证两岸三地写法互相命中）、`internal/atomicfile`（原子写）可借鉴。
- 记忆格式：**ZCode 项目记忆格式作为 MarkToast 基准**；Hermes `MEMORY.md`/`USER.md`（`§` 分隔）作为导入格式；
  DSH 插件 `dsh-memory-manager` 已有 ZCode 导入逻辑，可参考其映射。

### 4.4 脱敏

- **gitleaks（MIT）** 的规则集（TOML）可直接移植/裁剪；Hermes 的 `threat_patterns`（MIT）覆盖 prompt injection/隐私模式；
- 写入前做：密钥正则 → 私密路径 → 用户自定义 deny 列表；命中时拒绝写入或要求确认（M2）。

### 4.5 桌面与插件

- **Wails v3**（MIT，nightly 活跃）：M4 桌面壳；beta/nightly 风险已接受，保留 Web UI 兜底。
- **moejs**（Apache-2.0，429★，活跃）：M5 适配器 JS 插件宿主，复用而非自研。
- Bubble Tea（MIT）：TUI 第二界面，M5。

## 5. 许可证红线

| 项目 | SPDX | 处理 |
|---|---|---|
| basic-memory、OpenViking | AGPL-3.0 | **不得进二进制、不得链接**；只能读文档/接口设计 |
| EverOS | Apache-2.0 + NOTICE | 可移植；必须随附 NOTICE 与修改说明 |
| ai-memory、memsearch、hindsight、deja-vu、gitleaks、mark3labs/mcp-go | MIT | 可移植/依赖；保留版权声明 |
| Letta memfs、EverOS、gse、Bleve、sqlite-vec、moejs | Apache-2.0 | 可依赖/参考；保留 LICENSE/NOTICE |
| 官方 MCP Go SDK | MIT/Apache 混合 | 可依赖；保留双许可声明（建议优先 mark3labs） |

## 6. 采纳决策（v1）

1. **不引入第三方记忆引擎运行时**；store/index/gitx/hub 自研（工作量在此，无法外包）。
2. 索引：SQLite FTS5 + 预分词；实现 `Tokenizer` 接口，提供 **bigram（零依赖快路径）** 与 **gse（可选精度路径）**；
   CLI 默认 bigram，hub 可启用 gse；用 eval 决定默认值。
3. 查询：**OR + BM25**（非 AND），名称/短语加分，trigram 仅 ≥3 字符回退，两字用 bigram 兜底。
4. MCP：**官方 `modelcontextprotocol/go-sdk` v1.8.0**（stdio + Streamable HTTP、多 client、token/session metadata；
   许可证为 Apache-2.0 AND MIT，保留双声明），`mark3labs/mcp-go`（MIT）作为备选。
5. 接入顺序：**ZCode（hooks + MCP）→ DSH（MCP + marker 注入）→ Hermes（provider 插件）**；
   Codex / Claude Code / Pi 走指令投影 + MCP。
6. 会话摄取：**移植 deja-vu 的 reader**（MIT，署名），先做三家，不做 43 家。
7. 脱敏：gitleaks 规则 + Hermes threat_patterns 裁剪（M2）。
8. v2：ai-memory/hindsight 作为**可选 sidecar 语义层**（不进默认安装、删掉不丢记忆），sqlite-vec + bge-small-zh 做 RRF。

## 7. 先做的 5 个实验

1. **bigram vs gse A/B**：同 schema、同 50 条中文查询（真实记忆），测 R@5、p95、CLI 冷启动、重索引耗时。
2. **两字/中英/代码标识符边界**：`记忆`、`检索`、`FTS5`、`down`、`LongTermMemoryStore`、路径/API 名，验证 primary/trigram/bigram 路由。
3. **50 条冻结 eval**：tune/holdout 拆分，保存 SQL、词典 hash、结果 JSON。
4. **增量一致性**：Markdown 改/删/改名、进程重启、词典变更后的索引不脏。
5. **v2 sqlite-vec 最小 RRF**：100–300 条真实记忆，bge-small-zh embedding，FTS top50 + vector top50，只有 R@5 稳定提升才进 M2。

## 8. 来源

- modernc SQLite：<https://pkg.go.dev/modernc.org/sqlite>
- SQLite FTS5 / trigram / custom tokenizers：<https://sqlite.org/fts5.html>
- gojieba：<https://github.com/yanyiwu/gojieba>；gse：<https://github.com/go-ego/gse>
- Bleve：<https://github.com/blevesearch/bleve>
- mark3labs/mcp-go：<https://github.com/mark3labs/mcp-go>；官方 Go SDK：<https://github.com/modelcontextprotocol/go-sdk>
- deja-vu：<https://github.com/vshulcz/deja-vu>
- ai-memory：<https://github.com/akitaonrails/ai-memory>
- Letta / memfs：<https://github.com/letta-ai/letta-code>
- EverOS：<https://github.com/EverMind-AI/EverOS>
- Hermes Agent：<https://github.com/NousResearch/hermes-agent>
- ZCode hooks：<https://github.com/zai-org/ZCode/blob/main/apps/zcode-cli/docs/usage/09-hooks.md>
- gitleaks：<https://github.com/gitleaks/gitleaks>
- Wails：<https://github.com/wailsapp/wails>；moejs：<https://github.com/MoeForge/moejs>
