<div align="center">

# MarkToast · 马克吐司

**每个 agent 的记忆，一处存放，归你所有。**

本地优先、Git 版本化的 Markdown 记忆中枢，适配所有 coding agent。<br>
**40 家 harness 全部内置**——ZCode、Claude Code、Codex、Cursor、Gemini CLI、
DSH、Hermes、Pi 等。

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Wails](https://img.shields.io/badge/Wails-v3-DF0000?logo=wails&logoColor=white)
![macOS](https://img.shields.io/badge/macOS-first-000000?logo=apple&logoColor=white)
![Status](https://img.shields.io/badge/status-pre--alpha-d8b26a)
![License](https://img.shields.io/badge/license-MIT-green)
[![GitHub stars](https://img.shields.io/github/stars/realchendahuang/MarkToast?style=flat&color=111111)](https://github.com/realchendahuang/MarkToast/stargazers)

[English](README.md) · [产品定位](docs/PRODUCT.md) · [技术栈](docs/TECH-STACK.md) · [架构设计](docs/ARCHITECTURE.md) · [路线图](docs/ROADMAP.md) · [致敬](docs/CREDITS.md)

</div>

![MarkToast UI](docs/assets/ui-light.png)
![MarkToast UI · 详情](docs/assets/ui-detail.png)

---

## 为什么需要 MarkToast

每个 harness 都在记，但各记各的：Claude Code 有自己的一套笔记，Codex 是另一个文件，
ZCode 发明了第三种格式，DSH 的插件生态里又有第四种。你换个工具，记忆就留在上一个工具里
——它被工具绑架、被格式割裂、对下一个 agent 不可见。

MarkToast 把记忆还原成它本该有的样子：**一个你拥有的仓库。**

- **一份 Markdown 仓库就是唯一真源。** 可读、可 grep、可 diff，Obsidian 直接打开。
- **Git 就是工作流。** 每次记忆变更都是一次 diff；分支是记忆实验；历史是审计线；
  每条记忆都有自己的版本历史，可以在桌面端直接恢复旧版本（生成一次新提交）。
- **一个本地中枢服务所有 harness。** 能读指令文件的就投影进去，支持 MCP 的走 MCP，
  有插件机制的上原生插件。
- **先联邦，后搬家。** 适配器优先对接各家原生记忆；导入只是兜底，不是前提。
- **本地优先，零云端。** 不要账号、不上报数据；跨机同步用你自己的 Git remote。
- **中文优先。** CJK 分词与子串检索是一等公民。

## 架构

```
                        ┌──────────────────────────────┐
                        │      Markdown 记忆仓库        │  ← 唯一真源
                        │  projects/<key>/memory/      │    （Git 版本化、人类可读）
                        │  global/                     │
                        └──────────────┬───────────────┘
                                       │  原子写 / 仓库锁 / 索引自动刷新
                        ┌──────────────┴───────────────┐
                        │        core（Go）             │
                        │  store · index（FTS5+CJK）    │
                        │  git（批量提交）               │
                        │  hub（HTTP + MCP）            │
                        └──────┬───────────────┬───────┘
                               │               │
                      CLI `marktoast`     Wails v3 桌面（内嵌 UI）
                               │
             ┌─────────────────┴──────────────────┐
             │  适配器（YAML 描述符）                │
             │  T1 指令投影                        │  AGENTS.md / CLAUDE.md marker 块
             │  T2 会话摄取                        │  只读候选记忆
             │  T3 MCP 工具                        │  任意 MCP 客户端
             │  T4 原生插件                        │  ZCode hooks · Hermes provider · DSH plugin
             └────────────────────────────────────┘
                               │
                     可选：你自己的 Git remote（跨机 / 团队）
```

## 怎么运转

1. `marktoast init` 创建 `~/Memory`（或 `--root`）并 `git init`。
2. 每条记忆一个 Markdown 文件 + YAML frontmatter —— 采用 ZCode 项目记忆格式，
   现有记忆可无转换导入。
3. `MEMORY.md` 是生成的索引；SQLite FTS5 索引是派生物，可随时 `marktoast reindex` 重建。
4. `marktoast serve` 起本地 hub；每家 harness 的适配器把索引投影进它自己的
   指令/记忆接口。
5. `git log` 就是记忆的历史，`git revert` 就是撤销。
6. 本地 hub 只监听 loopback。浏览器 API 使用 HttpOnly + SameSite 会话 Cookie，
   并校验 Host/Origin；脚本客户端与 MCP 使用 `.marktoast/` 下的 bearer token。

## 现状

**开源 pre-alpha —— 核心闭环、加固后的本地 hub、内嵌 UI 桌面壳、CI 与签名发布校验均已实现。**

- [x] Go module + CLI（`version` / `init` / `add` / `get` / `search` / `reindex` /
      `log` / `status` / `hook` / `mcp` / `adapters` / `serve` / `ui`）
- [x] SQLite FTS5 索引 + 零依赖 CJK bigram 分词（两字中文可命中；OR + BM25 排序）
- [x] Markdown 原子写、仓库级写锁、文件名防覆盖、只提交受管记忆路径、退出时 flush 防抖提交
- [x] **40 家内置 harness 适配器**：19 家 `auto`（MarkToast 直接合并配置）、
      12 家 `staged`（生成可粘贴的 hooks/MCP 片段）、9 家 `mcp_only`（只注册 MCP）；
      一家一个 YAML 描述符
- [x] 通用安装引擎：JSON/TOML 合并、写前备份、幂等写入、保留第三方条目；
      staged / mcp_only 生成片段与手贴说明
- [x] 仅监听 loopback 的本地 hub：Cookie/Bearer 认证、CSRF/Host 校验、分页 API；
      MCP server（stdio + streamable HTTP，`memory_search/get/save/list`）
- [x] Web UI 采用 Magpie 的设计语言（MIT）：同一套 design tokens（浅色默认 + 自动暗色、靛蓝强调色）、212px 侧栏、面包屑头部、白色列表卡片与行、**Magpie 同款交互动画**（视图淡入、分段控件 thumb 滑动 280ms、行展开折叠 420ms、弹层进出、骨架 shimmer、reduced-motion）、弹层、Cmd/Ctrl+K
- [x] Wails v3.1 桌面壳 + 内嵌 Web UI（`make desktop`；原生窗口加载同一个 hub）
- [x] 适配器页展示全部 40 家，按安装模式分组，带检测/接入状态与
      「接入 / 生成片段 / 查看说明」操作

### 支持矩阵

40 家适配器全部随二进制内置。「已内置」指描述符已编译进二进制、
`marktoast adapters list` 可见、安装引擎支持该模式；逐家端到端实测进度见
[路线图](docs/ROADMAP.md)。† 表示上游 ai-memory 只按文档接线、未做端到端验证。

#### 自动接入 · `auto` · 19 家

MarkToast 直接合并配置，写前备份、保留第三方条目。

| Harness | ID | 接入方式 | 层级 | 状态 |
|---|---|---|---|---|
| Antigravity CLI (agy) | `antigravity-cli` | 自动 | projection | 已内置 |
| Claude Code | `claude-code` | 自动 | projection | 已内置 |
| Claude Desktop (Code tab) | `claude-desktop-code` | 自动 | projection | 已内置 |
| CodeBuddy Code | `codebuddy` | 自动 | projection | 已内置 |
| Codex | `codex` | 自动 | projection | 已内置 |
| Command Code | `command-code` | 自动 | projection | 已内置 |
| Cursor | `cursor` | 自动 | projection | 已内置 |
| Devin CLI | `devin` | 自动 | projection | 已内置 |
| Factory Droid | `factory-droid` | 自动 | projection | 已内置 |
| Gemini CLI | `gemini-cli` | 自动 | projection | 已内置 |
| GitHub Copilot CLI | `copilot-cli` | 自动 | projection | 已内置 |
| Grok Build CLI | `grok` | 自动 | projection | 已内置 |
| IBM Bob | `ibm-bob` | 自动 | projection | 已内置 |
| Kimi Code | `kimi-code` | 自动 | projection | 已内置 |
| Meta Muse Code | `meta-muse` | 自动 | projection | 已内置 |
| Qoder | `qoder` | 自动 | projection | 已内置 |
| Qwen Code | `qwen-code` | 自动 | projection | 已内置 |
| ZCode | `zcode` | 自动 | native | 已内置 |
| Zero (Gitlawb/zero) | `zero` | 自动 | projection | 已内置 |

#### 片段安装 · `staged` · 12 家

生成可粘贴的 hooks / MCP 片段，由你手动合并或安装插件。

| Harness | ID | 接入方式 | 层级 | 状态 |
|---|---|---|---|---|
| Cline CLI | `cline` | hooks 片段 | projection | 已内置 |
| Crush | `crush` | hooks 片段 | readonly | 已内置 |
| DeepSeek Harness | `dsh` | hooks 片段 | projection | 已内置 |
| GrizzyBot | `grizzybot` | hooks 片段 | readonly | 已内置 |
| Hermes Agent | `hermes` | hooks 片段 | native | 已内置 |
| Kiro CLI | `kiro-cli` | hooks 片段 | projection | 已内置 |
| Oh My Pi (OMP) | `oh-my-pi` | hooks 片段 | projection | 已内置 |
| OpenClaw | `openclaw` | hooks 片段 | projection | 已内置 |
| OpenCode | `opencode` | hooks 片段 | projection | 已内置 |
| Pi | `pi` | hooks 片段 | projection | 已内置 |
| Pool (Poolside Agent CLI) | `pool` | hooks 片段 | projection | 已内置 |
| Swival CLI | `swival-cli` | hooks 片段 | readonly | 已内置 |

#### 仅 MCP · `mcp_only` · 9 家

只生成 MCP 片段，不安装生命周期 hooks。

| Harness | ID | 接入方式 | 层级 | 状态 |
|---|---|---|---|---|
| Claude Desktop (Chat) | `claude-desktop-chat` | MCP | readonly | 已内置 |
| FreeBuff | `freebuff` | MCP | readonly | 已内置 |
| Goose | `goose` | MCP | readonly | 已内置 † |
| Kilo Code | `kilo-code` | MCP | readonly | 已内置 † |
| OpenHuman | `openhuman` | MCP | readonly | 已内置 † |
| Prime Agent | `prime-agent` | MCP | readonly | 已内置 |
| VS Code Copilot | `vs-code-copilot` | MCP | readonly | 已内置 |
| Warp | `warp` | MCP | readonly | 已内置 † |
| Zed | `zed` | MCP | readonly | 已内置 |

**层级：** `native`（2 家）——harness 有自己的记忆格式，MarkToast 可读写；
`projection`（26 家）——只走指令文件 / hooks / MCP 投影；
`readonly`（12 家）——只读取或注入，不写回 harness 自己的记忆。

**接入方式：** 「自动」= MarkToast 合并配置（写前备份）；「hooks 片段」=
生成可粘贴的 hooks/MCP 文件；「MCP」= 只注册 MCP，不装生命周期 hooks。

详见 [docs/ROADMAP.md](docs/ROADMAP.md)。

## 安装

CLI 的唯一入口是 `cmd/marktoast`（根目录不再有 `main.go`）。在仓库内安装：

```bash
go install ./cmd/marktoast
```

或安装最新发布版本：

```bash
go install github.com/realchendahuang/MarkToast/cmd/marktoast@latest
```

## 快速开始（开发）

依赖：Go 1.26+、git；`make verify` 中的 JavaScript 语法检查需要 Node.js 20+。
macOS 是主要目标，构建 Wails 桌面壳还需要 Xcode command-line tools。Web UI
已内嵌进原生应用，不需要单独的前端构建步骤。

```bash
make verify                                  # 格式、tidy、vet、race 测试、JS 语法、构建
./bin/marktoast version
./bin/marktoast init                         # 创建 ~/Memory + git 仓库
./bin/marktoast add --name release-rollback \
  --description "2026-09 发版回滚原因" --type project --body "发版后首页闪屏，决定回滚。"
./bin/marktoast search "回滚"
./bin/marktoast serve                        # 内嵌 Web UI：http://127.0.0.1:3777
make desktop                                 # 内嵌 UI 的原生 Wails 窗口
./bin/marktoast-desktop
./bin/marktoast mcp                          # 给 harness 的 stdio MCP server
./bin/marktoast adapters install zcode       # 默认 dry-run；加 --apply 写入
make install                                 # CLI + MarkToast.app（由安装脚本构建）
```

## 致敬 · Standing on shoulders

MarkToast 站在这些项目肩上——具体借鉴了什么见 [docs/CREDITS.md](docs/CREDITS.md)。

- **[magpie](https://github.com/yetone/magpie)**（MIT）——我们想成为的产品形态：
  Go + Wails v3、系统 WebView、原生前端、本地 hub、给每个 agent 写配置。
- **[ai-memory](https://github.com/akitaonrails/ai-memory)**（MIT）——Markdown 为源、
  派生索引、Git-backed 记忆引擎的思路；其跨 harness 支持矩阵与 hook 接线方式
  是 MarkToast 40 份 YAML 描述符的事实来源（见 [docs/CREDITS.md](docs/CREDITS.md)）。
- **[ZCode](https://github.com/zai-org/ZCode)**（Apache-2.0）——MarkToast 采用的项目
  记忆格式（`MEMORY.md` 索引 + 一条事实一个 Markdown + frontmatter）。
- **[Hermes Agent](https://github.com/NousResearch/hermes-agent)**（MIT）与
  **[DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)**（MIT）——
  harness 接入面（memory provider、插件/MCP 接缝）。
- **[EverOS](https://github.com/EverMind-AI/EverOS)**（Apache-2.0）、
  **[memsearch](https://github.com/zilliztech/memsearch)**（MIT）——本地优先 Markdown
  记忆与混合检索的先例。
- **[Wails](https://github.com/wailsapp/wails)**（MIT）、
  **[moejs](https://github.com/MoeForge/moejs)**（Apache-2.0）——桌面壳与未来的插件引擎。

## 文档

| 文档 | 内容 |
|---|---|
| [品牌规范](docs/BRAND.md) | Logo、颜色、字体、资产清单 |
| [插件](docs/PLUGINS.md) | JS 插件 API 与事件 |
| [发布](docs/RELEASE.md) | 签名、公证、发布清单 |
| [产品定位](docs/PRODUCT.md) | 用户、场景、非目标、成功指标 |
| [技术栈](docs/TECH-STACK.md) | 每个选型的理由、备选与风险 |
| [架构设计](docs/ARCHITECTURE.md) | 组件、存储布局、适配器、故障模式 |
| [路线图](docs/ROADMAP.md) | M0–M5 |
| [复用调研](docs/RESEARCH-REUSE.md) | 开源记忆生态里哪些能复用、哪些不能碰 |
| [harness 接入](docs/HARNESS-INTEGRATION.md) | 40 家 harness 怎么接：三级安装模式、逐家目标/事件/注入/坑 |
| [致敬](docs/CREDITS.md) | 向谁学习、在什么许可下 |
| [桌面壳](docs/DESKTOP.md) | Wails v3 桌面壳、打包与发布说明 |
| [适配器](adapters/README.md) | 如何新增一家 harness |

## 许可证

采用 **MIT**（见 [LICENSE](LICENSE)）。

欢迎提 issue 和 PR。如果 MarkToast 对你有用，点一个 star，能让更多用 agent 的人找到它。

## Star history

[![Star history chart](https://api.star-history.com/svg?repos=realchendahuang/MarkToast&type=Date)](https://star-history.com/#realchendahuang/MarkToast&Date)
