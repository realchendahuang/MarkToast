# 适配器目录

MarkToast 的适配器是**声明式 YAML**：加一家 harness 是配置工作，不是核心代码。
内置描述符覆盖约 40 家 coding agent / IDE / 桌面客户端，全部由同一个通用安装引擎
（`internal/harness`）按 `install_mode` 分三档执行。

- **内置默认值**在 `internal/adapter/defaults/*.yaml`（编译进二进制，随版本发布）。
- **用户覆盖**放在工作目录的 `adapters/`（本目录，存在时优先加载，便于本地开发调试）。
- 读取用户配置目录中的适配器（`$(os.UserConfigDir)/marktoast/adapters/`）是后续项，
  当前只在工作目录存在 `adapters/` 时加载用户覆盖。

同 `id` 的用户适配器会**整体替换**内置默认值（字段不做逐项合并）。加载时会展开 `~`
与环境变量，`{workspace}` / `{profile}` / `{projectKey}` 等占位符由安装引擎在运行时替换。

> 接线事实（配置路径、hook 事件、注入方式、坑）参考
> [akitaonrails/ai-memory](https://github.com/akitaonrails/ai-memory)（MIT）的
> `docs/support-matrix.md`、`docs/mcp-install.md`、`docs/install.md` 与
> `docs/managed-harness-contributions.md`，按 MarkToast 的 Markdown + Git 模型重写；
> 我们不 fork 它的引擎。归属与许可见 [docs/CREDITS.md](../docs/CREDITS.md)。
> 每家 harness 的完整接入说明见 [docs/HARNESS-INTEGRATION.md](../docs/HARNESS-INTEGRATION.md)。

## 一个完整示例

```yaml
id: my-harness                    # 必填，lower-kebab-case，全局唯一
title: My Harness                 # UI 显示名
detect:                           # 存在任一目录即视为已安装
  - ~/.my-harness

instruction_file:                 # T1 指令投影：harness 读取的指令文件
  global: ~/.my-harness/AGENTS.md
  project: "{workspace}/AGENTS.md"
  marker: "<!-- marktoast:begin -->"   # 只维护 marker 之间的块

native_memory:                    # 该 harness 自己的记忆目录（没有就留空）
  root: ~/.my-harness/memories
  index: MEMORY.md

hooks:
  events: [SessionStart, UserPromptSubmit, Stop]   # auto 模式必填
  install_kind: json_merge        # 见下方枚举
  schema: claude                  # 见下方枚举；auto 模式必填
  scope: global                   # global（用户配置）| project（仓库内配置）
  target: ~/.my-harness/config.json                 # auto 模式必填
  injection: stdout_additional_context              # 见下方枚举
  needs_trust: false              # 装完是否还需要在 harness 里确认信任
  notes: "坑与依据（写清从哪份文档/源码得到的事实）。"

mcp:
  kind: http                      # http | stdio_bridge | patch_fragment | project_json | none
  shape: mcpServers               # 见下方枚举
  target: ~/.my-harness/config.json
  notes: "schema 约束、token 是否只能写字面量等。"

session:                          # T2 会话摄取来源（可选）
  roots: [~/.my-harness/sessions]
  format: jsonl                   # jsonl | sqlite | json | unknown
  notes: "读哪个字段、什么版本。"

session_logs: ~/.my-harness/sessions
tier: projection                  # native | projection | readonly
capabilities: [read, inject]      # read | write | inject | mcp
install_mode: auto                # auto | staged | mcp_only；留空则按 hooks.install_kind 推导
docs:                             # 供 UI「查看说明」与维护者参考
  - https://example.com/docs/hooks
notes: "一句话说明接入方式与已知限制。"
```

## 字段说明

### 基础

| 字段 | 必填 | 说明 |
|---|---|---|
| `id` | 是 | lower-kebab-case（`a-z0-9-_.`），内置文件名为 `<id>.yaml`，全局唯一 |
| `title` | 是 | UI 与 `adapters list` 的显示名 |
| `detect` | 是 | 目录/文件存在性检测列表；任一命中即 `detected` |
| `tier` | 是 | `native`（原生记忆可双向读写）/ `projection`（指令文件 + MCP + hooks 投影）/ `readonly`（只读取会话，不写回） |
| `capabilities` | 否 | `read` / `write` / `inject` / `mcp`，UI 与 doctor 用 |
| `install_mode` | 否 | 安装档位，见下节；缺省时由 `hooks.install_kind` 推导 |
| `docs` | 否 | 该家官方文档 / ai-memory 文档小节链接，UI「查看说明」与维护者用 |
| `notes` | 否 | 一句话摘要（进入 README 矩阵的「说明 / 坑」）；**必须写依据**（文档路径或源码文件），不能编造 |

### `instruction_file`（T1 指令投影）

| 字段 | 说明 |
|---|---|
| `global` | 用户级指令文件路径（如 `~/.agents/AGENTS.md`） |
| `project` | 项目级指令文件路径模板，`{workspace}` 由当前仓库替换 |
| `marker` | MarkToast 受管区块标记；只替换 marker 之间的内容，块外字节不动 |

### `native_memory` / `session`（T2 原生记忆与会话）

| 字段 | 说明 |
|---|---|
| `native_memory.root` | harness 自己的记忆目录；没有原生记忆就留空 |
| `native_memory.index` | 该目录里的索引文件名（通常是 `MEMORY.md`） |
| `session.roots` | 会话记录目录/文件列表 |
| `session.format` | `jsonl` / `sqlite` / `json` / `unknown` |
| `session_logs` | 兼容字段：最常见的一条会话路径，供旧代码读取 |
| `session.notes` | 版本/字段注意点（例如"不要依赖其内部 schema"） |

### `hooks`（生命周期接线）

| 字段 | 说明 |
|---|---|
| `events` | 要接的事件名，按该 harness 的真实事件名书写（如 `SessionStart`、`on_session_start`）；auto 模式必填 |
| `install_kind` | 安装形态：`json_merge` / `toml_merge` / `json_bundle` / `yaml_snippet` / `patch_fragment` / `plugin_bundle` / `mcp_only` / `manual` / `none` |
| `schema` | 落盘 hooks 的 schema 变体，决定合并引擎怎么读/写：`claude` / `claude_no_matcher` / `flat` / `flat_no_matcher` / `event_map` / `named_group` / `exec` / `exec_list` / `bundle` / `toml` / `yaml` / `plugin` / `none`（支持 `bundle:<modifier>` 复合形式钉死独立 hooks 文件的形状；auto 模式必填） |
| `scope` | `global`（用户配置，默认）或 `project`（仓库内配置；安装时用 `--workspace` 解析 `{workspace}`） |
| `target` | hooks 写入的目标文件；auto 模式必填 |
| `injection` | 注入机制：`stdout_additional_context`（hook stdout 的结构化 JSON）/ `pre_llm_call_context`（每回合前打印 `{"context": ...}`）/ `mcp_handoff`（走 MCP 兜底）/ `none` |
| `needs_trust` | 写入配置后还需 harness 内手动确认信任时为 `true` |
| `notes` | 该家的坑与字段级依据：`Stop` 是否只是回合边界、`SessionStart` 是否 detached、token 是否只能字面量、是否需要 trust 确认等。维护时以这里为准 |

### `mcp`（MCP 注册）

| 字段 | 说明 |
|---|---|
| `kind` | `http`（写 URL + headers）/ `stdio_bridge`（写本地命令）/ `patch_fragment`（配置是补丁文件，只能生成片段）/ `project_json`（项目级 `.mcp.json`）/ `none` |
| `shape` | 配置键形状：`mcpServers`（Claude/Cursor 风格）/ `mcp.servers`（ZCode 风格）/ `context_servers`（Zed 风格）/ `mcp_servers`（Hermes/Python 风格）/ `servers` / `mcp` / `extensions`（各家变体） |
| `target` | MCP 配置写入目标文件 |
| `notes` | schema 约束（token 是否需要字面量、是否拒绝未知键、shim 命令等） |

## 三级安装模式（`install_mode`）

| 模式 | 判定标准 | MarkToast 的行为 | UI 动作 |
|---|---|---|---|
| `auto` | 配置是 JSON/TOML，schema 明确且能安全合并 | 按 `hooks.schema` 合并 hooks、按 `mcp.kind`/`shape` 注册 MCP；写前备份、保留第三方条目、原子写、幂等；`uninstall` 精确回滚 | **接入 / 修复** |
| `staged` | 需要插件安装、补丁合并或人工粘贴（YAML/TOML 片段、`insert:` patch） | 在 `~/.marktoast/adapters/<id>/` 生成 `hooks.*`、`mcp.*`、`README.txt`（含粘贴步骤、hub URL、token、坑），打印步骤；不假装一键 | **生成片段** |
| `mcp_only` | 只有 MCP，没有生命周期 hooks | 只生成 MCP 片段与说明；不写 hooks；`installed` 恒为 false | **生成片段** |

缺省 `install_mode` 时按 `hooks.install_kind` 推导（与 `EffectiveInstallMode()` 一致）：

| `hooks.install_kind` | 推导模式 |
|---|---|
| `json_merge` / `toml_merge` / `json_bundle` | `auto` |
| `mcp_only` / `none` | `mcp_only` |
| 其它（`yaml_snippet` / `patch_fragment` / `plugin_bundle` / `manual`） | `staged` |

`install_mode` 显式声明优先于推导。`install_mode: auto` 必须同时提供 `hooks.target`、
`hooks.schema` 和至少一个 `hooks.events`，否则 `Validate()` 会拒绝该描述符
（`go test ./internal/adapter` 会失败）。auto 描述符如果落盘 schema 无法安全合并，
引擎会在安装时自动降级为 staged，并在 `adapters list` 里标 `auto*` 且说明原因。

## 分级（`tier`）

| tier | 含义 |
|---|---|
| `native` | 有稳定的原生记忆格式，可双向读写 |
| `projection` | 只能投影（指令文件 / MCP / hooks），不直接写原生记忆 |
| `readonly` | 只读取会话或记忆，不写回 |

## 如何新增一家 harness

1. **取证。** 读该 harness 官方文档；若 ai-memory 的 support matrix 有该家的一行，先
   以那一行为准（它记录了实际验证过的路径/事件/坑）。把依据写进 `notes` /
   `hooks.notes` / `mcp.notes`。**不要编造路径**——拿不准就降级为 `staged` 并在
   notes 里写"未端到端验证"。
2. **建文件。** `internal/adapter/defaults/<id>.yaml`，从最接近的一家复制字段再改。
3. **定模式。**
   - 配置文件是 JSON/TOML 且键位稳定、能保留第三方条目 → `auto`，补齐
     `hooks.schema` / `hooks.scope` / `hooks.target` / `mcp.shape`。
   - 配置是 YAML、插件清单、补丁文件，或写入后必须人工确认 → `staged`。
   - 只有 MCP 文档、没有 lifecycle hooks → `mcp_only`。
4. **填 hooks 事实。** 事件名用该家原文（PascalCase / snake_case 不要"统一"）；
   `injection` 写真实机制：stdout 注入不可靠的（如 DSH 的 detached SessionStart）用
   `mcp_handoff`；忽略 session-start 输出的（如 Hermes）用 `pre_llm_call_context`。
5. **校验。**

   ```bash
   go test ./internal/adapter                       # Validate、id 唯一、auto 字段齐全
   go run ./cmd/marktoast adapters list             # 全量列表（应包含新 id，并显示 mode）
   go run ./cmd/marktoast adapters status           # 有状态的家：detected / staged / installed
   go run ./cmd/marktoast adapters install <id>     # dry-run，打印将写什么
   go run ./cmd/marktoast adapters install <id> --apply   # auto：写入并备份
   go run ./cmd/marktoast adapters install <id> --apply   # staged/mcp_only：生成片段到 ~/.marktoast/adapters/<id>/
   go run ./cmd/marktoast adapters uninstall <id> --apply # 只回滚 MarkToast 写入的部分
   ```

6. **UI 验证。** `go run ./cmd/marktoast serve` 打开适配器页：应出现在正确分组
   （自动 / 片段 / 仅 MCP），状态徽章与「接入 / 生成片段 / 查看说明」按钮符合该模式；
   中英文切换后文案仍正确。
7. **补测试。** 若该家属于关键路径（原 5 家、auto 代表、已知踩坑），在
   `internal/adapter/adapter_test.go` 加字段断言；安装引擎的行为变化在
   `internal/harness` 的测试里覆盖。

## 约定

- 一个文件一家 harness；内置总数与 `internal/adapter/defaults/` 的文件数一致
  （当前 **40 家 = 19 auto + 12 staged + 9 mcp_only**；tier：native 2 / projection 26 /
  readonly 12，见 [HARNESS-INTEGRATION.md](../docs/HARNESS-INTEGRATION.md)）。
- `notes` 是给维护者看的"现场记录"：写清依据、版本、已知失效点，不要只写宣传语。
- 无法自动合并的 schema 一律降级 `staged`；宁可让用户粘贴，也不写坏别人的配置。
- `id` 一旦发布不要改名（UI、CLI、`~/.marktoast/adapters/<id>/` 都依赖它）。
