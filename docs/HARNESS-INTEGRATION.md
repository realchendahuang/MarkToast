# Harness 接入说明

> MarkToast 把同一份 Git 版本化的 Markdown 记忆接到 **40 家** coding agent。
> 本文讲清楚三件事：三级安装模式、每家 harness 的目标文件 / hook 事件 / 注入方式 /
> 已知坑，以及安装与验证步骤。

## 来源声明

40 家的接线事实（配置路径、hook 事件名、注入机制、版本差异与坑）主要参考
[akitaonrails/ai-memory](https://github.com/akitaonrails/ai-memory)（MIT）的：

- `docs/support-matrix.md`：逐家状态、配置路径、hook 事件、注入方式、坑与未验证项；
- `docs/mcp-install.md` 与 `docs/install.md`：MCP 注册与 hooks 安装细节；
- `docs/managed-harness-contributions.md`：适配器正确性协议——我们拿它当验收清单。

然后按 MarkToast 的模型重写：**不 fork ai-memory 的引擎**（它的 wiki / consolidation /
managed workstream 与我们无关），只移植"接线知识"和可读的脚本模式；每条事实落在
`internal/adapter/defaults/<id>.yaml` 的 `notes` / `hooks.notes` / `mcp.notes` 里，
并标注依据（ai-memory 文档小节或对应 Rust 源码路径）。归属与许可见
[CREDITS.md](CREDITS.md)。

> 本页表格与 `internal/adapter/defaults/*.yaml` 对齐：40 家 = 19 `auto` +
> 12 `staged` + 9 `mcp_only`。改 YAML 后请同步本页数字与分组。

## 1. 总览

| 维度 | 数量 | 说明 |
|---|---|---|
| 内置 harness | **40** | 一家一个 `defaults/<id>.yaml`，编译进二进制 |
| `auto` 自动安装 | 19 | 能安全合并 JSON/TOML 配置的家 |
| `staged` 片段安装 | 12 | 插件、补丁或需要人工粘贴的家 |
| `mcp_only` 仅 MCP | 9 | 只有 MCP、没有可接生命周期 hooks 的家 |
| tier `native` | 2 | ZCode、Hermes Agent（有原生记忆格式） |
| tier `projection` | 26 | 指令文件 / hooks / MCP 投影 |
| tier `readonly` | 12 | 只读取或注入，不写回 harness 自己的记忆 |

能力层级沿用 README 的 T1–T4：

| 层 | 做什么 | 在 descriptor 里 |
|---|---|---|
| T1 指令投影 | 把 `MEMORY.md` 索引写进 harness 的指令文件 marker 块 | `instruction_file` |
| T2 会话摄取 | 读会话记录，产出 `inbox/` 候选记忆 | `session` / `session_logs` |
| T3 MCP 工具 | `memory_search/get/save/list` | `mcp` |
| T4 生命周期 hooks | 事件归集 + SessionStart 注入 | `hooks` |

## 2. 三级安装模式

| 模式 | 判定标准 | MarkToast 的行为 | 产物 | UI 动作 |
|---|---|---|---|---|
| `auto` | 配置是 JSON/TOML、schema 明确、能安全合并 | 按 `hooks.schema` 合并 hooks，按 `mcp.kind`/`mcp.shape` 注册 MCP；写前备份、幂等、保留第三方条目、原子写 | 直接改 `hooks.target` / `mcp.target`；原文件备份为 `<file>.marktoast-backup-<时间戳>` | **接入 / 修复** |
| `staged` | 插件包、补丁文件，或写入后必须人工确认 | 生成 `hooks.*`、`mcp.*`、`README.txt`（含粘贴步骤、hub URL、token、该家的坑），打印步骤 | `~/.marktoast/adapters/<id>/` | **生成片段** |
| `mcp_only` | 只有 MCP，没有（或无法安全接的）生命周期 hooks | 只生成 MCP 片段与说明，不写 hooks；`installed` 恒为 false | 同上 | **生成片段** |

- 描述符可用 `install_mode` 显式声明；缺省时由 `hooks.install_kind` 推导：
  `json_merge` / `toml_merge` / `json_bundle` → `auto`；`mcp_only` / `none` →
  `mcp_only`；其余 → `staged`。
- `auto` 必须同时有 `hooks.target`、`hooks.schema` 和至少一个 `hooks.events`，
  否则 `adapter.Validate()` 拒绝（`go test ./internal/adapter` 会失败）。
- 引擎遇到无法安全合并的 schema 会**自动降级为 staged**，并在 `adapters list` 里
  标 `auto*`、在报告里写明原因——不会写坏用户配置。
- MCP 统一指向本地 hub：`http://127.0.0.1:3777/mcp`（`marktoast serve` 启动；
  脚本与 MCP 用 `<root>/.marktoast/token` 的 bearer token）。
- `Claude Desktop (Chat)` 的 `stdio_bridge` 是客户端侧 shim：
  `npx -y mcp-remote http://127.0.0.1:3777/mcp`，**不是** MarkToast 自带 bridge；
  `Claude Desktop (Code tab)` 实际运行 Claude Code，复用 `~/.claude/settings.json`，
  是两个不同 id（见矩阵）。
- MCP URL 统一是纯 `http://127.0.0.1:3777/mcp`：`?flavor=moonshot` / `?flavor=bedrock`
  是 ai-memory 的 tool-schema 变体机制，MarkToast 没有这个概念；Kimi Code / Kiro CLI
  若上游要求 flavor 参数，需要用户自行追加。

## 3. 全量支持矩阵

表格列：`安装形态` 是 `hooks.install_kind`（+ `hooks.schema` / `hooks.scope`），
`hooks 目标` / `hooks 事件` / `注入` / `MCP` 直接对应 YAML 字段；`说明 / 坑` 是该家
YAML 的 `notes`。更细的字段级依据（源码路径、版本）在各家 YAML 的 `hooks.notes` 与
`mcp.notes` 里，维护时以 YAML 为准。

### 自动接入 · `auto` · 19 家

MarkToast 直接合并配置，写前备份、保留第三方条目。

| Harness（`id`） | 层级 | 安装形态 | hooks 目标 | hooks 事件 | 注入 | MCP | 说明 / 坑 |
|---|---|---|---|---|---|---|---|
| Antigravity CLI (agy)（`antigravity-cli`） | projection | `json_merge` · schema `named_group` · global | ~/.gemini/config/hooks.json | `SessionStart` `PreInvocation` `PreToolUse` `PostToolUse` `Stop` | stdout_additional_context | `http` · `mcpServers`<br>~/.gemini/config/mcp_config.json | 别名 agy/antigravity；与 gemini-cli 共享 ~/.gemini 根但配置文件名不同（detect 有重叠，安装器按 id 区分）。技能根：~/.gemini/config/skills（App/IDE）与 ~/.gemini/antigravity-cli/skills（CLI），不读 ~/.agents/skills。 |
| Claude Code（`claude-code`） | projection | `json_merge` · schema `claude` · global | ~/.claude/settings.json | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `PostToolUseFailure` `PreCompact` `Stop` `SessionEnd` `SubagentStart` `SubagentStop` | stdout_additional_context | `http` · `mcpServers`<br>~/.claude.json | 指令投影 + MCP + hooks；hooks 也可作为会话摄取来源。 |
| Claude Desktop (Code tab)（`claude-desktop-code`） | projection | `json_merge` · schema `claude` · global | ~/.claude/settings.json | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `PostToolUseFailure` `PreCompact` `Stop` `SessionEnd` `SubagentStart` `SubagentStop` | stdout_additional_context | `http` · `mcpServers`<br>~/.claude.json | 与 claude-code 共用 hooks 目标文件，装一次即覆盖；与 Chat tab（claude-desktop-chat）是两个不同 id。 |
| CodeBuddy Code（`codebuddy`） | projection | `json_merge` · schema `claude` · global | ~/.codebuddy/settings.json | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `PreCompact` `Stop` `SessionEnd` | stdout_additional_context | — | 腾讯 CodeBuddy Code；别名 code-buddy；无 native transcript backfill。 |
| Codex（`codex`） | projection | `json_merge` · schema `bundle` · global | ~/.codex/hooks.json | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `PreCompact` `Stop` `SessionEnd` | stdout_additional_context | `http` · `mcp_servers`<br>~/.codex/config.toml | 指令投影 + MCP + hooks；SessionEnd 版本差异要在适配器里探测。 |
| Command Code（`command-code`） | projection | `json_merge` · schema `claude_no_matcher` · global | ~/.commandcode/settings.json | `SessionStart` `PreToolUse` `PostToolUse` `Stop` | stdout_additional_context | `http` · `mcpServers`<br>~/.commandcode/mcp.json | 可执行名 command-code（Windows 上 cmdc）；--session <uuid> 续接。 |
| Cursor（`cursor`） | projection | `json_merge` · schema `flat` · global | ~/.cursor/hooks.json | `sessionStart` `sessionEnd` `beforeSubmitPrompt` `preToolUse` `postToolUse` `postToolUseFailure` `preCompact` `stop` | stdout_additional_context | `http` · `mcpServers`<br>~/.cursor/mcp.json | Cursor 不能 post-compaction 注入，压缩后靠已装 instructions 重新落地。 |
| Devin CLI（`devin`） | projection | `json_bundle` · schema `event_map` · global | ~/.devin/hooks.v1.json | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `PostCompaction` `Stop` `SessionEnd` | stdout_additional_context | `http` · `mcpServers`<br>~/.devin/config.json | SessionEnd 可用（真实会话结束），无需 finalize-session；PostCompaction 是 Devin 独有事件。 |
| Factory Droid（`factory-droid`） | projection | `json_merge` · schema `event_map` · global | ~/.factory/hooks.json | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `PreCompact` `Stop` `SessionEnd` | stdout_additional_context | `http` · `mcpServers`<br>~/.factory/mcp.json | 厂商拒绝把 hooks 写进 settings.json，专用 hooks.json 是唯一稳定入口；别名 droid。 |
| Gemini CLI（`gemini-cli`） | projection | `json_merge` · schema `claude` · global | ~/.gemini/settings.json | `SessionStart` `SessionEnd` `BeforeTool` `AfterTool` `PreCompress` | stdout_additional_context | `http` · `mcpServers`<br>~/.gemini/settings.json | ai-memory 把 Gemini 归入 AGENTS.md 阵营（install_instructions.rs: Codex/OpenCode/Cursor/Gemini/Kimi Code 收敛到 AGENTS.md）；Gemini 自身也读 GEMINI.md，MarkToast 目前只投影 AGENTS.md，尚未覆盖 GEMINI.md。 |
| GitHub Copilot CLI（`copilot-cli`） | projection | `json_merge` · schema `flat_no_matcher` · global | ~/.copilot/hooks/marktoast.json | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `PostToolUseFailure` `PreCompact` `Stop` `SessionEnd` `SubagentStart` `SubagentStop` | stdout_additional_context | `http` · `mcpServers`<br>~/.copilot/mcp-config.json | 注意区分 VS Code Copilot（vs-code-copilot，MCP-only）与 Copilot CLI；别名 copilot-cli。 |
| Grok Build CLI（`grok`） | projection | `json_merge` · schema `bundle` · global | ~/.grok/hooks/marktoast.json | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `PreCompact` `Stop` `SessionEnd` `SubagentStart` `SubagentStop` | stdout_additional_context | `http` · `mcp_servers`<br>~/.grok/config.toml | Grok 只能通过 use_tool 元工具访问 MCP 工具，首轮规则文件是 handoff 可达的关键；Grok 不接受 SessionStart/UserPromptSubmit 注入。 |
| IBM Bob（`ibm-bob`） | projection | `json_merge` · schema `claude` · global | ~/.bob/settings/settings.json | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `Stop` | stdout_additional_context | `http` · `mcpServers`<br>~/.bob/settings/mcp.json | 别名 bob；IDE 与 Bob Shell 共享 ~/.bob/settings。 |
| Kimi Code（`kimi-code`） | projection | `toml_merge` · schema `toml` · global | ~/.kimi-code/config.toml | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `PostToolUseFailure` `PreCompact` `Stop` `SessionEnd` `SubagentStart` `SubagentStop` | stdout_additional_context | `http` · `mcpServers`<br>~/.kimi-code/mcp.json | 别名 kimi；$KIMI_CODE_HOME 同时移动 mcp.json 与 config.toml。 |
| Meta Muse Code（`meta-muse`） | projection | `json_merge` · schema `claude` · global | ~/.config/muse/settings.json | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `PostToolUseFailure` `PreCompact` `Stop` `SessionEnd` | stdout_additional_context | `stdio_bridge` · `mcpServers`<br>~/.config/muse/settings.json | 别名 muse/muse-code；读 ~/.agents/skills 与 AGENTS.md，技能无需另装。 |
| Qoder（`qoder`） | projection | `json_merge` · schema `claude` · global | ~/.qoder/settings.json | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `PostToolUseFailure` `PreCompact` `Stop` `SessionEnd` | stdout_additional_context | `stdio_bridge` · `mcpServers`<br>~/.qoder/settings.json | Qoder CLI 与 IDE 共享 ~/.qoder；读 AGENTS.md 与共享 ~/.agents/skills，routing/skills 无需 Qoder 专属副本。 |
| Qwen Code（`qwen-code`） | projection | `json_merge` · schema `claude` · global | ~/.qwen/settings.json | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `PostToolUseFailure` `PreCompact` `Stop` `SessionEnd` `SubagentStart` `SubagentStop` | stdout_additional_context | `http` · `mcpServers`<br>~/.qwen/settings.json | 别名 qwen/qwencode；读 AGENTS.md 与 QWEN.md，routing 片段无需 Qwen 专属文件；setup 在 ~/.qwen 存在时自动接线。 |
| ZCode（`zcode`） | native | `json_merge` · schema `exec` · global | ~/.zcode/cli/config.json | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `PostToolUseFailure` `Stop` | stdout_additional_context | `http` · `mcp.servers`<br>~/.zcode/cli/config.json | 原生记忆：MEMORY.md 索引（≤200 行 / 25,000 字符）+ 一条事实一个 Markdown（frontmatter）；记忆根由 workspace 路径 SHA-256[:16] 推导。 |
| Zero (Gitlawb/zero)（`zero`） | projection | `json_bundle` · schema `exec_list` · global | ~/.config/zero/hooks.json | `sessionStart` `sessionEnd` `beforeTool` `afterTool` `specialistStart` `specialistStop` | mcp_handoff | `http` · `mcp.servers`<br>~/.config/zero/config.json | exec_list bundle（hooks.json）与 mcp.servers（config.json）的合并幂等、可回滚。注意 Zero 的 hook 沙箱仍要求用户放行网络或跑 hook-drain，安装器只改配置、不改沙箱策略。 |

### 片段安装 · `staged` · 12 家

生成可粘贴的 hooks / MCP 片段，由你手动合并或安装插件。

| Harness（`id`） | 层级 | 安装形态 | hooks 目标 | hooks 事件 | 注入 | MCP | 说明 / 坑 |
|---|---|---|---|---|---|---|---|
| Cline CLI（`cline`） | projection | `json_bundle` · global | ~/.cline/hooks | `TaskStart` `TaskResume` `UserPromptSubmit` `PreToolUse` `PostToolUse` `TaskComplete` `TaskCancel` `TaskError` `SessionShutdown` | mcp_handoff | `http` · `mcpServers`<br>~/.cline/data/settings/cline_mcp_settings.json | 每事件一个 JSON 文件，不是单文件 schema，故 staged；VC 扩展 hooks 目录不同。 |
| Crush（`crush`） | readonly | `none` · schema `none` · global | —（插件包） | — | none | — | Managed-only（ai-memory run crush）；无 hooks/MCP 安装器，MarkToast 只读会话库。 |
| DeepSeek Harness（`dsh`） | projection | `patch_fragment` · schema `none` · global | ~/.dsh/profiles/{profile}/cordis.patch.yml | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `Stop` `SubagentStart` `SubagentStop` | mcp_handoff | `patch_fragment`<br>~/.dsh/profiles/{profile}/cordis.patch.yml | 无固定 MCP 配置文件；静态兜底是受管 AGENTS.md 区块（约 65536 bytes），但不能替代 MCP。 |
| GrizzyBot（`grizzybot`） | readonly | `manual` · schema `none` · global | —（插件包） | — | none | — | 无可写配置，只有应用内 bridge，故为 staged。 |
| Hermes Agent（`hermes`） | native | `yaml_snippet` · schema `yaml` · global | ~/.hermes/config.yaml | `on_session_start` `pre_llm_call` `pre_tool_call` `post_tool_call` `on_session_end` `on_session_finalize` | pre_llm_call_context | `stdio_bridge` · `mcp_servers`<br>~/.hermes/config.yaml | 内置 MEMORY.md/USER.md 用精确 "\n§\n" 分隔，2200/1375 字符预算；完整 preload/turn sync 需实现 standalone MemoryProvider 并关闭内置写入以避免双写。 |
| Kiro CLI（`kiro-cli`） | projection | `json_merge` · schema `claude` · global | ~/.kiro/agents | `agentSpawn` `userPromptSubmit` `preToolUse` `postToolUse` `stop` | stdout_additional_context | `http` · `mcpServers`<br>~/.kiro/settings/mcp.json | v2/v3 是两个不兼容的 hook 注册格式；MarkToast 目前以 v2 片段为准，v3 尚未覆盖。 |
| Oh My Pi (OMP)（`oh-my-pi`） | projection | `plugin_bundle` · global | ~/.omp/agent/extensions/ai-memory-omp.ts | — | stdout_additional_context | `http` · `mcpServers`<br>~/.omp/agent/mcp.json | 别名 omp/oh-my-pi；extension 与 mcp.json 必须落在同一 agent dir。 |
| OpenClaw（`openclaw`） | projection | `plugin_bundle` · global | —（插件包） | `session_start` `session_end` `before_tool` `after_tool` | stdout_additional_context | `http` · `mcp.servers`<br>~/.openclaw/config.json | 插件包没有单一配置目标，须经 openclaw plugins install 安装，故为 staged。 |
| OpenCode（`opencode`） | projection | `plugin_bundle` · schema `plugin` · global | ~/.config/opencode/plugins/ai-memory.ts | `session.created` `session.idle` `session.deleted` `session.compacted` | stdout_additional_context | `http` · `mcp.servers`<br>~/.config/opencode/opencode.json | V1/V2 由 executable 版本探测；插件是 TS 源码，故 staged。 |
| Pi（`pi`） | projection | `plugin_bundle` · global | ~/.pi/agent/extensions/ai-memory-pi.ts | — | mcp_handoff | `stdio_bridge`<br>~/.pi/agent/extensions/ai-memory-pi.ts | Pi 与 OMP 是不同 harness（extension 文件名与目录不同），不要合并 id。 |
| Pool (Poolside Agent CLI)（`pool`） | projection | `yaml_snippet` · schema `yaml` · project | .poolside/settings.yaml | `SessionStart` `UserPromptSubmit` `PreToolUse` `PostToolUse` `Stop` | mcp_handoff | — | 别名 poolside；hooks 配置是项目级文件，MarkToast 只生成 staged 片段。 |
| Swival CLI（`swival-cli`） | readonly | `none` · schema `none` · global | —（插件包） | — | none | `http` · `mcpServers`<br>.swival/mcp.json | MCP-only，且配置在项目 checkout 内，故 staged 而非 mcp_only 自动写。 |

### 仅 MCP · `mcp_only` · 9 家

只生成 MCP 片段，不安装生命周期 hooks。

| Harness（`id`） | 层级 | 安装形态 | hooks 目标 | hooks 事件 | 注入 | MCP | 说明 / 坑 |
|---|---|---|---|---|---|---|---|
| Claude Desktop (Chat)（`claude-desktop-chat`） | readonly | `mcp_only` · schema `none` · global | — | — | none | `stdio_bridge` · `mcpServers`<br>~/Library/Application Support/Claude/claude_desktop_config.json | 与 Claude Desktop Code tab（claude-desktop-code）是两个 id；CLI 管理的是本地 JSON 配置，不是账号级 connector 或 .mcpb 扩展。 |
| FreeBuff（`freebuff`） | readonly | `mcp_only` · schema `none` · global | — | — | none | `stdio_bridge` · `mcpServers`<br>~/.agents/mcp.json | 配置文件在 ~/.agents（与共享 skills 同根）；stdio bridge 非 HTTP 条目。 |
| Goose（`goose`） | readonly | `mcp_only` · schema `none` · global | — | — | none | `http` · `extensions`<br>~/.config/goose/config.yaml | YAML 配置、extensions 容器、uri 键；Goose 不文档化 headers 环境变量展开。 |
| Kilo Code（`kilo-code`） | readonly | `mcp_only` · schema `none` · global | — | — | none | `http` · `mcp`<br>~/.config/kilo/kilo.jsonc | shape 用 mcp（Kilo 的容器键，不是 mcp.servers）；JSONC 需注释安全写入。 |
| OpenHuman（`openhuman`） | readonly | `mcp_only` · schema `none` · global | — | — | none | `http` · `mcpServers`<br>— | UI 内配置、无磁盘文件 → detect/target 都留空，MarkToast 只能展示接入说明。 |
| Prime Agent（`prime-agent`） | readonly | `mcp_only` · schema `none` · global | — | — | none | `http` · `mcpServers`<br>~/.prime/agent/settings.json | bearerTokenEnvVar 是 Prime 特有字段，不能写 headers。 |
| VS Code Copilot（`vs-code-copilot`） | readonly | `mcp_only` · schema `none` · global | — | — | none | `http` · `servers`<br>{workspace}/.vscode/mcp.json | 注意与 copilot-cli（GitHub Copilot CLI，有 hooks）区分；--client copilot 的裸别名在过去指 VS Code Copilot。 |
| Warp（`warp`） | readonly | `mcp_only` · schema `none` · global | — | — | none | `http` · `mcpServers`<br>~/.warp/.mcp.json | MCP-only documented（未端到端验证）；Warp 与 Warp Agent CLI 是两份配置。 |
| Zed（`zed`） | readonly | `mcp_only` · schema `none` · global | — | — | none | `http` · `context_servers`<br>~/.config/zed/settings.json | context_servers 是 Zed 独有的 shape（不是 mcpServers）。 |

## 4. 安装与验证

### 4.1 CLI

```bash
marktoast serve                                  # 先起 hub（MCP 片段需要 URL 与 token）

marktoast adapters list                          # 40 家：mode / detected / staged / installed
marktoast adapters status <id>                   # 单家详情；不带 id 只看有状态的家

marktoast adapters install <id>                  # 默认 dry-run：打印将写入什么
marktoast adapters install <id> --apply          # auto：备份后合并；staged/mcp_only：生成片段
marktoast adapters install <id> --apply --workspace /path/to/repo   # project scope 的家
marktoast adapters uninstall <id> --apply        # 精确回滚 MarkToast 条目，并删除 staged 目录
```

### 4.2 Web UI

「适配器」页列出全部 40 家，按 `自动 / 片段 / 仅 MCP` 分组：

- 每家显示 mode 徽章、层级、配置目标与状态：**已接入**（auto 已写入）、
  **已生成片段**（`~/.marktoast/adapters/<id>/` 已有文件）、**已检测**、**未检测**；
- 动作按模式出现：**接入 / 修复**（auto）、**生成片段**（staged / mcp_only）、
  **查看说明**（弹层展示 mode、目标文件、hook 事件、注入、MCP 与该家说明）；
- 状态来自 hub 的 `/api/adapters`（`mode`、`install_kind`、`staged`、`staged_ready`、
  `installed`），不依赖前端硬编码清单。

### 4.3 验证清单

| 检查项 | 怎么验 |
|---|---|
| 配置合并（auto） | `--apply` 前看 dry-run；`--apply` 后 `git diff` 式检查目标文件（或在备份旁对比），第三方 hooks/MCP 条目仍在 |
| 片段生成（staged/mcp_only） | `ls ~/.marktoast/adapters/<id>/`，按 `README.txt` 粘贴；MCP 片段里的 URL 与 token 正确 |
| hooks 生效 | 重启该 harness，触发一次会话；`<root>/.marktoast/events.ndjson` 增长，事件带该家 `agent` id |
| 注入可见 | 新会话开头看到 `MEMORY.md` 索引 / handoff；Hermes 看 `pre_llm_call` 的 `{"context": ...}` |
| MCP 可连 | 客户端工具列表出现 `memory_search/get/save/list`；或 `marktoast mcp` 本地 smoke |
| 幂等 | 重复 `install --apply` 不产生重复条目 |
| 可回滚 | `uninstall --apply` 后 MarkToast 条目消失、其他条目不动 |
| 检测状态 | `marktoast adapters list` 的 detected / staged / installed 与 UI 一致 |

## 5. 注入与常见坑

描述符的 `hooks.injection` 记录该家真实的注入机制：

| `hooks.injection` | 含义 | 代表 |
|---|---|---|
| `stdout_additional_context` | hook stdout 的结构化 JSON 会被 harness 读入上下文 | 大多数 auto 家 |
| `pre_llm_call_context` | harness 忽略 session-start 输出，要在每回合前打印 `{"context": ...}` | Hermes Agent |
| `mcp_handoff` | stdout 注入不可靠，靠 MCP 取上下文 | DSH（SessionStart detached）、Cline、Pi、Zero、Pool |
| `none` | 没有可用注入点（仅 MCP / 只读） | 9 家 mcp_only |

跨家高频坑（逐家细节见矩阵与 YAML 的 `hooks.notes`）：

- **没有真正的会话结束**：很多家 `Stop` 只是回合边界；有 `SessionEnd` /
  `on_session_finalize` 的（Claude Code、Codex ≥0.145.0、Devin、Hermes）才按真实
  会话结束处理。MarkToast 目前把 `Stop` 也当作摄取事件（见 `internal/ingest`），
  最后一回合的候选记忆可能稍晚出现在 `inbox`。
- **SessionStart detached**：DSH 的桥以 detached 方式跑 SessionStart，stdout 不可靠，
  所以走 `mcp_handoff`；DSH 的 MCP 还是 `cordis.patch.yml` 的 `- insert:` 片段。
- **版本差异要探测**：Codex 的 `SessionEnd` 自 CLI 0.145.0 起才可用；旧版本不触发。
- **token 只能写字面量**：ZCode 的 `mcp.servers` schema 严格（`type`/`url`/`headers`），
  token 存文件时需手动加。
- **信任门控**：Hermes 用 `hooks_auto_accept` / `shell-hooks-allowlist.json` 管 user
  hooks（`needs_trust: true`），每个 profile 是独立 home，要各自确认。
- **不能事后注入**：Cursor 不支持 post-compaction 注入，压缩后靠已投影的 instructions
  重新落地。
- **厂商限制写入位置**：Factory Droid 拒绝把 hooks 写进 `settings.json`，专用
  `~/.factory/hooks.json` 是唯一稳定入口；openclaw / oh-my-pi / pi / opencode 是插件
  形态（需要装插件或生成 TS 扩展），所以是 `staged` 而不是改 JSON。
- **project scope**：Pool 写仓库内 `.poolside/settings.yaml`、Kiro CLI 写 agent 配置，
  安装时用 `--workspace`；auto 家的 `hooks.scope` 会在 YAML 里标明。
- **同名不同物**：`claude-code` 与 `claude-desktop-code` 共用 hooks 目标（装一次即
  覆盖）；`copilot-cli` 与 `vs-code-copilot` 是两家；`codebuddy` 是腾讯 CodeBuddy
  Code，不是 CodeBuddy IDE。
- **平台差异**：Windows 上可执行名/路径不同（如 Command Code 是 `cmdc`）；以各家
  YAML 的 `notes` 为准。

## 6. 新增一家 harness

步骤、字段说明与校验命令见 [adapters/README.md](../adapters/README.md)：

1. 从 ai-memory 的 support matrix / 官方文档取证，**不要编造路径**，拿不准就降级 `staged`
   并在 `notes` 写"未端到端验证"；
2. 新增 `internal/adapter/defaults/<id>.yaml`，定 `install_mode`，补齐
   `hooks.target` / `schema` / `scope` / `mcp.shape`；
3. `go test ./internal/adapter`（Validate、id 唯一、全量校验）；
4. `marktoast adapters install <id>` dry-run → `--apply` → 按 §4.3 验证；
5. 回来更新本文的矩阵（数字、分组）与 [ROADMAP.md](ROADMAP.md) 的验证状态。

验收标准参考 ai-memory 的 `docs/managed-harness-contributions.md`：会话身份、只读导出、
pre-turn 注入、幂等安装、卸载回滚、yolo/权限映射。

## 7. 来源与许可

- 事实来源：[ai-memory](https://github.com/akitaonrails/ai-memory)（MIT）的
  support matrix、mcp-install、install 与 managed-harness-contributions；
- 实现方式：MarkToast 自己的 YAML 描述符 + Go 通用安装引擎（`internal/adapter`、
  `internal/harness`），**没有 fork ai-memory 的引擎**；
- 归属、许可与"借鉴了什么"的完整清单见 [CREDITS.md](CREDITS.md)。
