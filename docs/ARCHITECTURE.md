# MarkToast 架构设计

> 状态：草案 v0（2026-11-04）。私有开发期文档，随实现演进。

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
   `adapters/*.yaml` descriptor，加一家 harness 是配置工作，不是核心代码。
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
│  │ git     批量提交/分支/回滚/远端同步      │                         │
│  │ hub     HTTP + MCP（localhost, token）  │                         │
│  │ adapters descriptor 加载 + 写入器        │                         │
│  └───────────────┬───────────────────────┘                         │
│                  │                                                 │
│     CLI `marktoast`   ·   Wails v3 桌面 UI（后续）                  │
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
  .marktoast/                              # 派生物（.gitignore）
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
- 中文：入口层用 jieba 预处理（索引与查询两侧同一套分词）或退化为 `trigram`；
  `bm25(name*5, description*2, body*1) + 时间衰减 + 类型权重` 排序。
- 结果返回 `path + snippet + rank`，由 agent 决定是否读全文（不整段注入）。
- 后续可插拔向量索引（embedding 只做派生层，可关可重建）。

## 6. Git 工作流

- **批量提交**：写入先落盘，再按 debounce（默认 5s）聚合为一次 commit，避免仓库爆炸。
- **提交信息模板**：`mem(<project>): <op> <name>`，author 记录 harness 名。
- **分支**：`memory/experiment/<topic>` 用于记忆实验；合并时索引 regenerate，不合并文本。
- **冲突**：事实文件按 name 唯一；同名不同内容的冲突进 `inbox/conflicts/` 人工裁决。
- **远端**：`marktoast sync` → 用户自己的 Git remote（GitHub/Gitea/自建）。

## 7. 适配器

`adapters/<id>.yaml` 声明式描述，T1/T2/T3/T4 分级能力：

```yaml
id: zcode
detect: [~/.zcode]
instruction_file:
  global: ~/.zcode/AGENTS.md
  project: "{workspace}/AGENTS.md"
  marker: "<!-- marktoast:begin -->"
native_memory:
  root: ~/.zcode/cli/memories/projects/{projectKey}/memory
  index: MEMORY.md
session_logs: ~/.zcode/v2/sessions
capabilities: [read, write, inject]
tier: native
```

首批适配器：**ZCode → DSH → Hermes**；随后 Codex、Claude Code、Pi、OpenCode、Cline。
新增 harness 只需一个 YAML + 一个 fixture，社区可贡献。

## 8. 并发与故障模式

- **单写者**：hub 或 CLI 拿 `flock` 串行写；SQLite WAL；Markdown 原子替换（temp + rename）。
- **hub 挂掉**：降级为只读——AGENTS.md 里已投影的索引仍在，记忆仓库仍可 git/grep；
  不丢数据、不静默失忆。
- **Git 不可用**：记忆照常读写，提交排队；`marktoast status` 报告积压。

## 9. 技术栈

| 层 | 选择 | 理由 |
|---|---|---|
| 核心 / CLI | **Go 1.26** | 单二进制、跨平台、并发与 IO 友好；团队已有 Go 经验 |
| 桌面壳 | **Wails v3**（系统 WebView） | 与 Magpie 同路：单二进制、无 Electron 体积 |
| SQLite | **modernc.org/sqlite**（纯 Go） | 无 CGo，交叉编译简单，FTS5 可用 |
| Git | **go-git**（或 CLI 兜底） | 进程内提交；复杂操作走 `git` 命令 |
| 插件引擎（后续） | **moejs**（Apache-2.0，Magpie 同款） | 适配器可做成 JS 插件，社区可扩展 |
| 前端 | Vite + TypeScript（框架待定） | 轻量、可嵌入 Wails |

参考实现（架构与工程纪律借鉴，不直接 fork）：`yetone/magpie`（MIT，Go+Wails 本地 hub
与配置写入器）、`akitaonrails/ai-memory`（MIT，Markdown+Git-backed 记忆引擎）。
许可证与署名记录在 `docs/CREDITS.md`（待建）。

## 10. 安全与隐私

- 默认仅 `127.0.0.1` 监听；MCP/HTTP 使用本地 bearer token。
- 记忆进入索引/注入前做敏感信息脱敏（密钥模式、私密路径）。
- 无遥测、无云默认值；`--root` 可指向加密卷。
- 公开仓库前清理所有个人记忆样例与 token。

## 11. 里程碑

见 [ROADMAP.md](ROADMAP.md)。核心顺序：**store → index → git → CLI → adapters → hub → UI**。
