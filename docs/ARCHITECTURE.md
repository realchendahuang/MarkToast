# MarkToast 架构设计

> 状态：v0（2026-11-04 起草，随实现更新）。开发文档，随实现更新。

## 1. 一句话定位

MarkToast 是一个**本地优先、Git 版本化的 Markdown 记忆中枢**：任何 coding agent
（ZCode / Hermes / DSH / Codex / Claude Code / Pi / …）都可以读写同一份记忆，
换 harness 不丢上下文。

## 2. 设计原则

1. **Markdown 是唯一真源。** 索引、向量、UI 缓存全部是可重建的派生物；删掉 `.marktoast/`
   不丢任何记忆。
2. **Git 是工作流，不是备份。** 每次变更可 diff；分支即记忆实验；历史即审计；
   回滚是 `git revert` 而不是一个待开发功能。
3. **先联邦，后搬家。** 适配器优先对接各 harness 已有的原生记忆（指令文件、原生记忆目录、
   hooks）；导入外部 store 只是兜底。
4. **Hub + 适配器。** 核心只做存储 / 索引 / Git / HTTP+MCP；harness 的差异全部收进
   `internal/adapter/defaults/*.yaml` descriptor，加一家 harness 是配置工作，不是核心代码。
5. **本地零云。** 默认只监听 `127.0.0.1`；跨机同步走用户自己的 Git remote；无遥测。
6. **中文优先。** CJK 分词 / 子串检索是一等公民。
7. **多层降级。** hub 挂了，Markdown 仍可读、可 grep、可 git；不能让记忆"失忆"。

## 3. 总体架构

```
┌─────────────────────────────── 本机 ───────────────────────────────┐
│                                                                    │
│  Markdown 记忆仓库（唯一真源，Git 管理）                             │
│  ~/Memory/                                                         │
│    projects/<slug>-<hash16>/memory/MEMORY.md + <fact>.md           │
│    global/                                                         │
│                                                                    │
│  ┌────────────── core（Go） ──────────────┐                         │
│  │ store   读写/锁/frontmatter/原子替换     │                         │
│  │ index   SQLite FTS5（CJK）+ 增量重建     │                         │
│  │ git     批量提交/回滚（远端用 git CLI）  │                         │
│  │ hub     HTTP + MCP（localhost, token）  │                         │
│  │ adapters descriptor 加载 + 写入器        │                         │
│  └───────────────┬───────────────────────┘                         │
│                  │                                                 │
│   CLI `marktoast` · Wails v3 桌面壳（内嵌 UI）                      │
│                  │                                                 │
│  ┌───────────────┴───────────── 适配层（按 harness 分级）──────────┐ │
│  │ T1 指令文件投影（AGENTS.md/CLAUDE.md/GEMINI.md marker 块）      │ │
│  │ T2 会话日志摄取（只读，产出候选记忆）                            │ │
│  │ T3 MCP 工具（凡是 MCP 客户端）                                   │ │
│  │ T4 原生插件（ZCode hooks/skills、Hermes provider、DSH plugin）   │ │
│  └───────────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────────┘
                     ↕ 可选的 Git remote（跨机 / 团队）
```

## 4. 存储布局

与 ZCode 的项目记忆布局同构，便于互操作：

```
~/Memory/                                  # 默认仓库根（可用 --root 覆盖）
  projects/
    feedsieve-d3cb28c4ac319b40/            # <workspace basename>-<sha256(abs path)[:16]>
      memory/
        MEMORY.md                          # 索引（一行一条；由 store 统一生成）
        no-github-ci-local-gates.md        # 每条事实一个文件
        trust-hardening-roadmap.md
  global/                                  # 跨项目 / 用户画像事实
    user-fast-answers.md
  .marktoast/                              # 派生物（.git/info/exclude 忽略）
    index.db                               # SQLite FTS5
    lock
    cache/
```

事实文件格式（与 ZCode 兼容）：

```markdown
---
name: concise-memory-name
description: one-line recall description
metadata:
  node_type: memory
  type: user            # user | feedback | project | reference
  originSessionId: sess_0123456789abcdef
---
One durable fact.

**Why:** ...
**How to apply:** ...
```

## 5. 索引（FTS5 + CJK）

- SQLite FTS5，`external content` 表 + 触发器同步；索引是派生物，`marktoast reindex` 可全量重建。
- 中文：`internal/index` 使用零依赖 CJK bigram 分词（索引与查询两侧相同）；
  FTS5 + BM25 排序，两字中文查询可直接命中。
- 结果返回 `path + snippet + rank`，由 agent 决定是否读全文（不整段注入）。
- 后续可插拔向量索引（embedding 只做派生层，可关可重建）。

## 6. Git 工作流

- **批量提交**：写入先落盘，再按 debounce（默认 5s）聚合为一次 commit，避免仓库爆炸。
- **提交信息模板**：`mem(<project>): <op> <name>`，author 记录 harness 名。
- **分支**：需要记忆实验时直接用 `git branch` / `git switch`；MarkToast 不另造分支命令。
- **冲突**：事实文件按 name 唯一；同名不同内容的冲突进 `inbox/conflicts/` 人工裁决。
- **远端**：MarkToast 不封装同步；在记忆仓库中直接使用用户自己的 Git remote。
  例如：`git -C ~/Memory pull --rebase && git -C ~/Memory push`（GitHub/Gitea/自建均可）。

## 7. 适配器

内置描述符位于 `internal/adapter/defaults/<id>.yaml`，**40 家 harness 一家一个文件**，
声明式描述 T1/T2/T3/T4 能力。加一家 harness 是新增一个 YAML，不是改核心代码：

```yaml
# 示意：字段形态；具体每家的真实取值以 defaults/<id>.yaml 为准
id: example-harness
title: Example Harness
detect: [~/.example]
instruction_file:
  global: ~/.example/AGENTS.md
  project: "{workspace}/AGENTS.md"
  marker: "<!-- marktoast:begin -->"
native_memory:
  root: ~/.example/memories
  index: MEMORY.md
hooks:
  events: [SessionStart, UserPromptSubmit, Stop]
  install_kind: json_merge      # 安装形态
  schema: claude                # 落盘 hooks 的 schema 变体
  scope: global                 # global | project
  target: ~/.example/config.json
  injection: stdout_additional_context
  notes: "…（依据与坑）"
mcp:
  kind: http
  shape: mcpServers
  target: ~/.example/config.json
session_logs: ~/.example/sessions
tier: projection
capabilities: [read, inject, mcp]
install_mode: auto
```

### 7.1 三级安装模式

| 模式 | 含义 | 安装器行为 |
|---|---|---|
| `auto` | MarkToast 能安全合并该家配置（JSON/TOML、schema 明确） | 按 `hooks.schema` 合并 hooks，按 `mcp.kind`/`mcp.shape` 注册 MCP；备份、幂等、保留第三方条目、原子写 |
| `staged` | 需要插件安装、补丁合并或人工粘贴 | 在 `~/.marktoast/adapters/<id>/` 生成 `hooks.*` / `mcp.*` / `README.txt`，打印粘贴步骤与坑 |
| `mcp_only` | 只有 MCP，没有（或无法安全接）生命周期 hooks | 只生成 MCP 片段与说明，不写 hooks |

`install_mode` 缺省时由 `hooks.install_kind` 推导：`json_merge` / `toml_merge` /
`json_bundle` → `auto`；`mcp_only` / `none` → `mcp_only`；其余 → `staged`。

### 7.2 通用安装引擎

`internal/harness` 是唯一的安装入口（`Install` / `Uninstall`），按描述符分派，
替换了早期硬编码的 ZCode / DSH / Hermes 三家实现：

- **hooks 合并**支持 schema 变体：`claude` / `claude_no_matcher`
  （`hooks[event] = [{matcher?, hooks: [{type: "command", command, timeoutMs}]}]`）、
  `flat` / `flat_no_matcher`（事件 → `[{type, command}]`）、`event_map`（根级事件表）、
  `named_group`（命名分组）、`exec`（ZCode）、`exec_list`（Zero）、`bundle`
  （独立 hooks JSON 文件，可用 `bundle:<modifier>` 钉形状）、`toml`（`[[hooks]]`）、
  `yaml` / `plugin` / `none`。
- **MCP 注册**支持 shape：`mcpServers` / `mcp.servers` / `mcp_servers` /
  `context_servers` / `servers` / `mcp` / `extensions`（各家配置键形状）。
- **无法安全合并的 schema 一律降级 `staged`**，宁可由用户粘贴，也不写坏别人的配置。
- `uninstall` 按写入前记录的 marker/条目精确回滚；staged 目录直接删除。
- hub `/api/adapters` 返回 `mode`、`install_kind`、`staged`（能力）、`staged_ready`（片段是否已生成）
  与 `installed`（仅 auto 家做真实配置检测），Web UI 据此分组与显示动作。

### 7.3 能力分级（tier）

| tier | 含义 |
|---|---|
| `native` | 有稳定的原生记忆格式，可双向读写（如 ZCode） |
| `projection` | 只能投影：指令文件 / MCP / hooks，不直接写原生记忆 |
| `readonly` | 只读取会话或记忆，不写回 |

### 7.4 事实来源与归属

40 家 harness 的配置路径、hook 事件、注入方式与已知坑，主要参考
[akitaonrails/ai-memory](https://github.com/akitaonrails/ai-memory)（MIT）的
`docs/support-matrix.md`、`docs/mcp-install.md`、`docs/install.md` 与
`docs/managed-harness-contributions.md`，再按其自身文档核实，写进每个 YAML 的 `notes`。
MarkToast **不 fork 其引擎**，只移植接线事实与脚本模式；归属见 [CREDITS.md](CREDITS.md)。

新增 harness 只需一个 YAML（可选一个字段断言）；描述符校验与安装步骤见
[adapters/README.md](../adapters/README.md)，全量矩阵见
[HARNESS-INTEGRATION.md](HARNESS-INTEGRATION.md)。

## 8. 并发与故障模式

- **单写者**：CLI / HTTP / MCP 都经过 `internal/service` 的统一记忆操作；操作期间持有
  仓库级 `flock`（Windows 使用 `LockFileEx`），SQLite WAL，Markdown 使用唯一临时文件 +
  fsync + rename。
- **索引一致性**：每次写入后更新 SQLite 单条事务与 `MEMORY.md`；索引打开/查询前按
  Markdown mtime 检查是否过期，外部编辑或 `git pull` 后可自动重建。
- **hub 挂掉**：降级为只读——AGENTS.md 里已投影的索引仍在，记忆仓库仍可 git/grep；
  不丢数据、不静默失忆。
- **Git 不可用**：记忆照常读写，后续变更可手工提交；`marktoast status` 报告仓库状态。

## 9. 技术栈

| 层 | 选择 | 理由 |
|---|---|---|
| 核心 / CLI | **Go 1.26** | 单二进制、跨平台、并发与 IO 友好；团队已有 Go 经验 |
| 桌面壳 | **Wails v3**（系统 WebView） | 与 Magpie 同路：单二进制、无 Electron 体积 |
| SQLite | **modernc.org/sqlite**（纯 Go） | 无 CGo，交叉编译简单，FTS5 可用 |
| Git | **调用 `git` CLI**（exec）+ 批量提交 | 行为可预期、覆盖边界操作；开发者本机已有 git |
| 插件引擎（后续） | **moejs**（Apache-2.0，Magpie 同款） | 适配器可做成 JS 插件，社区可扩展 |
| 前端 | **原生 HTML/CSS/JS**（静态资源内嵌、无框架） | 对齐 Magpie：系统 WebView 内足够快、无框架负担 |
| TUI | **Bubble Tea + Lipgloss** | 与 Magpie 同款；给无 GUI/远程场景 |
| 平台 | **macOS 优先**，架构保持可移植 | 先在两台 mac 上做深，Windows/Linux 后续 |

参考实现（架构与工程纪律借鉴，不直接 fork）：`yetone/magpie`（MIT，Go+Wails 本地 hub
与配置写入器）、`akitaonrails/ai-memory`（MIT，Markdown+Git-backed 记忆引擎）。
许可证与署名记录在 `docs/CREDITS.md`。

## 10. 安全与隐私

- 默认且强制仅 loopback 监听；拒绝非 loopback Host 与跨站 Origin。
- 浏览器 UI 使用进程级 HttpOnly + SameSite 会话 Cookie；脚本 API 与 MCP 使用
  `<root>/.marktoast/token` 的 bearer token。Token 读取/创建失败时 fail-closed。
- `/api/*` 全部需要认证；Cookie 认证的写请求必须通过同源校验。
- 记忆进入索引/注入前做敏感信息脱敏（密钥模式、私密路径）。
- 无遥测、无云默认值；`--root` 可指向加密卷。
- 公开仓库前清理所有个人记忆样例与 token。


## 12. 已实现的接口面（v0）

### 12.1 HTTP API（hub，loopback）

| 方法 | 路径 | 作用 |
|---|---|---|
| GET | `/api/status` | 版本、仓库、facts、projects、adapters、index、git |
| GET | `/api/search?q=&project=&type=&limit=&offset=&sort=` | 分页排名搜索（FTS5 + BM25） |
| GET | `/api/memory?path=` / `?name=` | 单条记忆（frontmatter + body + raw） |
| POST | `/api/memory` | 保存记忆（写 Markdown → 索引 → `MEMORY.md` → commit） |
| GET | `/api/memories?project=&type=&limit=&offset=&sort=` | 分页列表 |
| GET | `/api/projects` | 项目与 facts 计数 |
| GET | `/api/adapters` | 全量适配器：`mode` / `install_kind` / `staged`（能力）/ `staged_ready`（片段是否已生成）/ `detected` / `installed` + hooks/mcp 元数据 |
| GET | `/api/git/log?n=` | 最近提交 |
| POST | `/mcp` | MCP Streamable HTTP（Bearer token 来自 `<root>/.marktoast/token`） |

### 12.2 MCP 工具

`memory_search` / `memory_get` / `memory_save` / `memory_list`。
stdio 入口是 `marktoast mcp`；HTTP 入口由 `marktoast serve` 提供。

### 12.3 Hook 协议

`marktoast hook --event <event> --agent <id>`：

- 从 stdin 读 harness 事件 JSON，追加到 `<root>/.marktoast/events.ndjson`（T2 摄取原料）；
- `SessionStart` 时输出 `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"..."}}`，
  内容是该项目的 `MEMORY.md`（或生成的紧凑索引），超过 6000 字符截断；
- 其它事件输出 `{}`，兼容要求 JSON 的 harness。

注入是否生效由 harness 决定，描述符的 `hooks.injection` 记录该家的真实机制：

| `hooks.injection` | 含义 | 代表 |
|---|---|---|
| `stdout_additional_context` | hook stdout 的结构化 `additionalContext` 会被读入 | ZCode、Claude Code、CodeBuddy |
| `pre_llm_call_context` | 在每回合前打印 `{"context": ...}` | Hermes（忽略 session-start 输出） |
| `mcp_handoff` | stdout 注入不可靠，走 MCP 兜底 | DSH（SessionStart detached） |
| `none` | 该家没有可用注入点 | 仅 MCP / read-only 家 |

同一种注入机制在不同家仍可能有细节差异（detached 执行、只接受字面量 token、
`Stop` 只是回合边界等），这些都写进对应 YAML 的 `hooks.notes`，安装前先读。

当前 `marktoast hook` 对所有 `SessionStart` 统一输出 `additionalContext`；
`pre_llm_call_context`（如 Hermes）与 `mcp_handoff`（如 DSH）的家由 provider 插件 /
MCP 兜底，注入效果的逐家实测仍是 M2 的剩余验证项（见 [ROADMAP.md](ROADMAP.md)）。

### 12.4 适配器安装语义

`marktoast adapters install <id>` 默认 dry-run，`--apply` 才写；按描述符的
`install_mode` 分三档执行（引擎在 `internal/harness`）：

- **auto**：按 `hooks.schema` 合并 hooks、按 `mcp.kind`/`mcp.shape` 注册 MCP。
  写前备份、幂等、保留第三方条目、原子写；`uninstall --apply` 只移除 MarkToast 条目。
  ZCode / Claude Code / Codex / Cursor / Gemini 等配置结构明确的家走这一档。
- **staged**：在 `~/.marktoast/adapters/<id>/` 生成 `hooks.*` / `mcp.*` / `README.txt`
  （含 hub URL、token、手贴步骤与该家的坑），由用户手动合并。DSH 的 Cordis patch、
  Hermes 的 `config.yaml` hooks 等走这一档。
- **mcp_only**：只生成 MCP 片段与说明，不装生命周期 hooks；`installed` 恒为 false，
  状态只看 `detected` 与 `staged_ready`。

无法安全合并的 schema 宁可降级为 staged，也不写坏用户配置；`adapters status` 与
hub `/api/adapters` 会如实报告 `mode`、`staged_ready` 与 `installed`。

## 13. 里程碑

见 [ROADMAP.md](ROADMAP.md)。核心顺序：**store → index → git → CLI → adapters → hub → UI**。
