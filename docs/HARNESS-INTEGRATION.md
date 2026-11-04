# Harness 接入参考：从 ai-memory 抄什么、怎么抄

> 来源：[akitaonrails/ai-memory](https://github.com/akitaonrails/ai-memory)（**MIT**，15k★）。
> 它是目前开源世界里**跨 harness 接线最全的一份目录**：`docs/support-matrix.md` 覆盖 30+ harness，
> 每个都写清 MCP 配置路径、hook 事件、注入方式、坑与未验证项；`hooks/<agent>/` 下有
> 可读的 shell/PowerShell 脚本模板；`docs/managed-harness-contributions.md` 是一份严格的
> "适配器正确性协议"。**我们能参考、能移植、能改写，但不要 fork 它的引擎。**

## 1. 能参考什么（按价值排序）

1. **支持矩阵本身**（最强）：每个 harness 的
   - MCP 安装方式与配置文件路径 / schema 约束
   - 生命周期 hook 事件清单与安装方式（JSON merge / YAML snippet / patch fragment / plugin bundle）
   - 注入机制：`hookSpecificOutput.additionalContext` / `{"context": ...}` / MCP handoff / 无
   - 会话存储位置、真实的"会话结束"事件是否存在、`Stop` 是否只是回合边界
   - 坑：detached SessionStart、token 只能写字面量、hook 需 trust 确认、yolo 关 hooks 等
2. **Hook 脚本模板**（`hooks/<agent>/*.sh` + `_lib.sh`，MIT）：读 stdin JSON → POST 到本机 hub；
   SessionStart 同步取回上下文并打印结构化 JSON。我们改写为调用 `marktoast hook` 即可。
3. **managed-harness 协议**（`docs/managed-harness-contributions.md`）：会话身份、只读导出、
   pre-turn 注入、yolo 映射的检查清单——直接当我们适配器的验收标准。
4. **安装/卸载语义**：幂等合并、保留第三方 hooks/servers、拒绝用户自有文件、备份、`--scope` 区分。

## 2. 不要抄什么

- **它的引擎**（wiki/consolidation/digest/retention）：和我们的 Markdown+Git 工作台模型不同；
- **它的 hook payload/HTTP API 命名**：按我们的 `/hook?event=...&agent=...` 设计；
- **managed workstream**（`ai-memory run` 的会话接管/恢复）：v1 不需要，风险大；
- **Rust 实现**：脚本模板是语言中立的，Go 重写安装器更简单。

## 3. 我们的模型 vs 它的模型

| | ai-memory | MarkToast |
|---|---|---|
| 真源 | 它自己的 wiki + SQLite | **用户 Git 仓库里的 Markdown** |
| 捕获 | hooks 事件 → 服务端 digest 成记忆 | hooks 事件 → **候选记忆 → inbox/审阅 → Git commit** |
| 注入 | handoff（上一会话交接）+ MCP recall | **MEMORY.md 索引投影 + MCP 工具**，Git diff 可审阅 |
| 适配器 | Rust 代码 + 脚本 + 文档 | **YAML descriptor + Go 安装器 + 脚本模板** |

结论：**抄它的"接线知识"和"脚本模式"，按我们的模型重写"安装器 + 注入器"。**

## 4. 首批 harness 的关键事实（摘自其 support matrix，已核实）

### ZCode（Supported）
- MCP：`~/.zcode/cli/config.json` 的 `mcp.servers`，严格 schema（`type`/`url`/`headers`）；
  token 只能写字面量，token-file 安装需手动加。
- Hooks：exec 形式（`type: "process"`）合并进根 `hooks` 块；6 个事件
  `SessionStart` / `UserPromptSubmit` / `PreToolUse` / `PostToolUse` / `PostToolUseFailure` / `Stop`；
  `PermissionRequest` 故意不装（与交互权限客户端竞态）。
- 注入：`SessionStart` stdout 的 `hookSpecificOutput.additionalContext` **可用**。
- 坑：没有真正的 SessionEnd（`Stop` 是回合边界）；最终回合后需要 finalize。

### DeepSeek Harness（Hooks + MCP）
- 无固定 MCP 配置文件：MCP 要往 `$DSH_HOME/profiles/<profile>/cordis.patch.yml` 写
  `- insert:` 片段（`@deepseek-ai/dsh-mcp-client`），无法自动 `--apply`。
- Hooks：`@deepseek-ai/dsh-hooks-claude-code` 桥，7 个事件（无 PreCompact/SessionEnd）；
  **SessionStart 是 detached 执行的，stdout 注入不可靠** → 走 MCP handoff。
- 坑：桥补丁需手合并；`Stop` 是回合边界；用事件归因 `agent=dsh`。

### Hermes Agent（Supported，但上游未端到端验证）
- MCP：`~/.hermes/config.yaml` 的 `mcp_servers:`，stdio bridge；Hermes 不读 MCP server instructions。
- Hooks：打印 `hooks:` 块给用户粘贴（ai-memory 从不自动写该 YAML；受 `hooks_auto_accept` 门控）；
  exec 形式、`shlex.split`、20s 超时；6 个事件：`on_session_start` / `pre_llm_call` /
  `pre_tool_call` / `post_tool_call` / `on_session_end`（每回合）/ `on_session_finalize`。
- 注入：Hermes **忽略 on_session_start 输出**；要在 `pre_llm_call` 打印 `{"context": ...}`。
- 坑：Hermes 上报自身进程目录为 cwd，需按 `TERMINAL_CWD` / marker 路由；gateway 空闲过期不会 finalize。

### Claude Code（Supported）
- MCP + 10 个 hook 事件（含 `PostToolUseFailure`、`PreCompact`、`SessionEnd`、subagent）；
  `SessionStart` 的 `additionalContext` 注入可用。

### Codex（Supported）
- MCP + hooks；`SessionEnd` 自 Codex CLI 0.145.0 起可用；老版本用 finalize 兜底。

## 5. 我们需要的 descriptor 扩展（已加进 internal/adapter）

```yaml
hooks:
  events: [SessionStart, UserPromptSubmit, Stop]
  install_kind: json_merge      # json_merge | yaml_snippet | patch_fragment | plugin_bundle | none
  target: ~/.zcode/cli/config.json
  injection: stdout_additional_context   # stdout_additional_context | pre_llm_call_context | mcp_handoff | none
  needs_trust: false
  notes: "Stop 是回合边界；最终回合后 finalize"
mcp:
  kind: http                    # http | stdio_bridge | patch_fragment | none
  target: ~/.zcode/cli/config.json
  notes: "严格 schema；token 字面量"
session:
  roots: [~/.zcode/v2/sessions]
  format: jsonl
  notes: "SessionEnd 不存在"
```

安装器（M2）要做的事：
1. 探测 harness（`detect` 路径）；
2. 读原配置 → 合并我们的 hooks/MCP → 原子写 + 备份；拒绝覆盖用户自有条目；
3. 把 hook 脚本 stage 到 `~/.marktoast/hooks/<agent>/`，脚本调用 `marktoast hook --event ... --agent ...`；
4. 打印无法自动应用的步骤（DSH cordis patch、Hermes YAML 粘贴、trust 确认）；
5. `adapters uninstall` 精确移除且保留第三方条目。

## 6. Hook 协议（MarkToast 版）

```
harness --stdin JSON--> marktoast hook --event session-start --agent zcode
                              │
                              ├─ POST http://127.0.0.1:3777/api/hook?event=...&agent=...
                              │  （fire-and-forget；失败不阻塞 harness）
                              └─ SessionStart 时：同步取索引/交接 → stdout 打印
                                 {"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"..."}}
```

要点：
- **捕获 fire-and-forget，注入只在支持的 harness 上同步做**；不支持的（DSH detached、Grok 等）走 MCP 工具兜底。
- 统一 20s 超时、无 shell 的 exec 形式优先；Windows 用 `.ps1` 模板。
- 失败要产生诊断（写 `~/.marktoast/logs/hooks.log`），不能静默吞。
- 每条 hook 记录 `agent` / `event` / `session_id` / `cwd`，供会话摄入与 handoff 使用。

## 7. 移植清单（M2）

- [ ] 从 ai-memory 矩阵整理成 `adapters/` 的 YAML（先 5 家：ZCode、DSH、Hermes、Claude Code、Codex）
- [ ] Go 安装器：`marktoast adapters install|uninstall|status <id>`
- [ ] hook 脚本模板（shell + PowerShell）+ `marktoast hook` 子命令
- [ ] 注入器：SessionStart 的 `additionalContext`；Hermes 的 `pre_llm_call` context
- [ ] MCP 合并器：ZCode/Claude Code/Codex 的 JSON/TOML；DSH 只打印 patch 片段
- [ ] 会话摄入读取器（参考 deja-vu，MIT）
- [ ] 验收：用 ai-memory 的 managed-harness 协议当 checklist（会话身份、只读、pre-turn 注入、幂等）

## 7.5 当前实现状态（2026-11-04）

- `marktoast adapters list`：5 个内置描述符（ZCode/DSH/Hermes/Claude Code/Codex）。
- `marktoast adapters install zcode [--apply]`：**已实现**。dry-run 打印计划；`--apply` 合并 hooks + MCP、
  备份配置、保留第三方条目；`uninstall --apply` 精确移除。
- `marktoast adapters install dsh|hermes`：**staged 半自动**，产出
  `~/.marktoast/adapters/<id>/hooks.json` + `README.txt` 说明，需用户按说明粘贴。
- `marktoast hook --event ... --agent ...`：已实现事件记录 + SessionStart 注入。
- `marktoast mcp`（stdio）与 `serve` 的 `/mcp`（streamable HTTP）：已实现 4 个 memory 工具。

## 8. 风险

- 上游 harness 版本漂移（hooks schema 变更）→ descriptor 带版本探测 + fixture；注入失败退回指令投影。
- Hermes 的 hooks 方案 ai-memory 自己也没端到端验证过 → 我们要先跑通再宣称 Supported。
- DSH 需要用户手改 Cordis patch → 安装器要做"半自动 + 校验"，不能假装一键。
- 抄它的矩阵文本要署名（MIT）；脚本模板如复制需保留版权头。

## 9. 子代理核查补充（ZCode / Hermes 源码级，2026-11-04）

### ZCode
- hooks 官方 7 事件（含 `PermissionRequest`，但会与交互权限客户端竞态，不装）；输入含
  `sessionId/cwd/turnId/traceId/toolName/toolInput/toolResponse/prompt/responseText`，输出支持
  `additionalContext` / 权限决策 / `updatedInput`，严格 schema。
- **优先用插件的 `hooks/hooks.json`**（自动启用），而不是改 `~/.zcode/cli/config.json`（顶层 `hooks`，
  默认关闭、需 `enabled: true`，容易漏）。
- 会话库是 `~/.zcode/cli/db/db.sqlite`（session/message/part 的 JSON 文本列）；
  **hook 的 `transcript_path` 不是完整会话日志**（只有当前 prompt 或最后一条回复）。
- 最省力路径：**直接复用 ZCode 原生项目记忆目录，不做格式转换**；`MEMORY.md` ≤200 行 / 25,000 字符；
  记忆根由 workspace 路径 SHA-256[:16] 推导。不要依赖 ZCode 的 SQLite schema。
- hook 入参同时提供 camelCase 与 Claude 风格 snake_case 兼容字段。

### Hermes
- 两条路径：**最低代码量 = 只配 MCP**（只有工具调用，没有 prefetch/sync_turn 生命周期）；
  **完整 = 实现 standalone `MemoryProvider`**（每个 profile 独立实例，绑定自己的 `hermes_home`；
  `MemoryManager` 同时只接受一个 external provider）。
- 若实现 provider，必须**关闭内置记忆写入**（`memory.memory_enabled=false` + `memory.user_profile_enabled=false`），
  否则双写冲突。
- 内置记忆：精确 `"\n§\n"` 分隔、无 frontmatter/ID/时间戳、2200/1375 字符预算（本机 MEMORY.md 已超限）。
- 会话导出优先 `hermes sessions export --format jsonl`（Markdown 是渲染格式，不是无损回导格式）。
- 脱敏可参考 `agent/redact.py`（凭据/隐私模式）；**Python 正则不能直接搬 Go RE2**，需重写并 fail-closed。

### DSH
- `@deepseek-ai/dsh-mcp-client` 0.2.1-rc.2（MIT；旧 npm 版本曾 BSD-3，安装须锁版本）；
  `insert` 一行 `id/name/config`，`transport: streamable-http` + `serverName/url/headers`。
- `dsh-hooks-claude-code`（BSD-3-Clause）只执行 command hook；SessionStart detached；仅作兜底。
- 静态兜底：受管 AGENTS.md 区块（约 65536 bytes），不能替代 MCP。

### 会话读取（复用）
- **deja-vu v0.23.5（MIT，Go）** registry 已覆盖 DSH `session.v4.jsonl.zstd`、ZCode、Hermes；
  reader 可直接移植，注意 DSH 的 zstd 解压。
- 参考实现：**dsh-memory-manager 1.4.3（MIT）**（fixture/scan/frontmatter/dedupe/index）；
  **dsh-claude-move（Apache-2.0，已冻结）**（白名单、冲突状态、幂等 manifest、受管 AGENTS.md 块）。

### 脱敏
- 候选：gitleaks（MIT，规则集）、detect-secrets（Apache-2.0）、Hermes `threat_patterns`（MIT）、
  ai-memory `scrub.rs`；统一 fail-closed：命中即拒写/要求确认，并记录审计事件。

