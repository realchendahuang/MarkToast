# MarkToast 技术栈

> v0 · 已定稿（2026-11-04）。技术选型以"独自开发、本地优先、单二进制、可长期维护"为第一原则。

## 一句话

**Go 1.26 + Wails v3 + 原生 HTML/CSS/JS（内嵌静态资源）+ modernc SQLite(FTS5) + git CLI + moejs 插件（后期）**
——与 Magpie 同一条技术路线，但只做记忆。

## 决策总表

| 层 | 选择 | 版本/库 | 理由 | 被否方案 |
|---|---|---|---|---|
| 核心语言 | **Go** | 1.26.x | 单二进制、跨平台交叉编译、IO/并发简单；作者已有 Go 经验；与 Wails 同栈 | Rust（学习+编译成本）、TypeScript/Node（分发依赖运行时） |
| 桌面壳 | **Wails v3**（已实现） | `github.com/wailsapp/wails/v3` **v3.1.0 稳定版** | 系统 WebView、单二进制、Mac/Windows/Linux；Magpie 同款 | Tauri 2（Rust 双语言）、Electron（体积/内存） |
| 前端 | **原生 HTML/CSS/JS** | 无框架；ES modules | **直接对齐 Magpie 的 design tokens、布局与动效**（浅色默认 + 暗色自动、212px 侧栏、`.seg` thumb 滑动、白色 `.list`/`.row` 卡片、view/fold/dialog 动画、reduced-motion）；系统 WebView 内足够快 | React / Svelte / Vue（都为复杂状态引入，v1 不需要） |
| 样式 | **手写 CSS + design tokens** | CSS 变量 + 一套 token（颜色/间距/字号） | 与 Magpie 的克制风格一致；可主题化、中英文自适应 | Tailwind（构建链更重，与"手写精修"路线冲突） |
| TUI | **Bubble Tea + Lipgloss**（已实现） | `charmbracelet/bubbletea` | Magpie 同款；给无 GUI 场景/远程使用 | 不做 TUI（少一个界面，但少一个卖点） |
| 数据库/索引 | **modernc.org/sqlite**（纯 Go） | 最新稳定版 | 无 CGo、交叉编译简单、内建 FTS5 | mattn/go-sqlite3（CGo）、Tantivy（额外索引层） |
| 全文检索 | **SQLite FTS5 + CJK bigram**（已实现） | FTS5 + BM25 + OR 查询；`internal/index` 的 `Tokenize` 自研 | 嵌入、零服务、可重建；两字中文可命中、零词典依赖；gse（Apache-2.0）留作可选精度路径 | Bleve + CJK analyzer（备选）、纯 grep（无排序） |
| Git | **调用 `git` CLI**（exec）+ 批量提交 | 系统 git | 行为可预期、实现简单、覆盖所有边界操作；开发者本机已有 git | go-git（纯 Go 但对复杂操作支持弱，后续可选） |
| Hub 协议 | **HTTP + MCP**（已实现） | 官方 `modelcontextprotocol/go-sdk` v1.8.0（stdio + streamable HTTP；备选 mark3labs/mcp-go） | 一套服务两用：UI/脚本走 HTTP，所有 agent 走 MCP；官方 SDK 覆盖多 client、token/session metadata | 仅 MCP（UI 不好走）、仅 HTTP（agent 接入面窄） |
| 适配器 | **YAML descriptor + Go 接口** | 自研 | 加一家 harness = 一个 YAML；社区可贡献 | 每家写死 Go 代码（不可扩展） |
| 插件引擎（后期） | **moejs**（Apache-2.0） | Magpie 同款 JS 网关插件引擎 | 复用成熟插件宿主，适配器可热插拔 | 自研插件系统（重复造轮子） |
| CLI | **cobra** | `spf13/cobra` | 子命令多、帮助/补全开箱即用 | 标准库 flag（子命令管理费劲） |
| 配置 | **TOML** | `BurntSushi/toml` | 可写注释、人读友好；Magpie 也用 TOML | JSON/YAML（注释/格式痛点） |
| 文件变更 | **手动 `reindex` + hub 写入路径** | 当前不引入文件监听依赖 | 行为明确、依赖少；手动编辑后由用户或 hub 请求重建索引 | 文件监听依赖（后续可选，需先实现并测试） |
| 日志 | **log/slog** | 标准库 | 够用、结构化、无依赖 | zap/zerolog（额外依赖） |
| 测试 | `go test` / `go vet` / `-race` + Node 语法检查 | Go 标准工具链 + `node --check` | 覆盖 store/index/git/hub；CI 同时检查前端 JS 语法，不引入前端测试框架 | Playwright（后续需要浏览器自动化时再加） |
| 分发 | 单二进制 + Wails app + 安装脚本；后期 Homebrew tap | — | macOS 优先 | 容器化（与本地桌面产品冲突） |

## "匹配 Magpie"具体指什么

Magpie 的栈（实测自 `yetone/magpie`，MIT）：

- Go 1.26 + **Wails v3.1.0** 系统 WebView
- 前端 **原生 HTML/CSS/JS**（仓库无 React/Vue，TS 仅少量），静态资源内嵌进桌面应用
- **moejs**（Apache-2.0）跑 JS 网关插件；插件市场独立仓库
- **modernc.org/sqlite**（纯 Go）；Bubble Tea/Lipgloss TUI；TOML/YAML
- 单二进制 + 签名发布（`magpie-releases`）+ 自更新 + 备份 + i18n + 脱敏

MarkToast 沿用同一路线，差异只在领域：**不做模型网关，只做记忆**。

## 项目结构

```
MarkToast/
├── cmd/
│   ├── marktoast/            # CLI + serve（无 GUI）
│   └── marktoast-desktop/    # Wails v3 壳
├── internal/
│   ├── store/                # 仓库布局、frontmatter、原子写、flock
│   ├── index/                # FTS5、重建、CJK 分词
│   ├── gitx/                 # 提交、log/diff/rollback
│   ├── hub/                  # HTTP + MCP server
│   ├── adapter/              # descriptor 加载、T1–T4 能力
│   │   └── defaults/         # 内置 adapter YAML：internal/adapter/defaults/*.yaml
│   ├── config/               # TOML
│   └── version/
├── frontend/                 # 原生 HTML/CSS/JS，作为静态资源内嵌
├── adapters/                 # 外部适配器示例（如 Hermes provider）
├── docs/
└── Makefile / go.mod
```

## 工程约定（不可妥协）

1. **Markdown 是唯一真源**；`.marktoast/` 下的一切（索引/缓存/锁）可删除并从 Markdown 重建。
2. **单写者**：进程内串行 + 仓库 `flock`；Markdown 原子替换（temp + rename）。
3. **Git 批量提交**：默认 5s debounce，避免每轮一次 commit 撑爆仓库。
4. **降级**：hub 停止时记忆仍可读（AGENTS.md 投影 + 文件/git 直读），不静默失忆。
5. **无遥测、无云默认值**；默认只监听 `127.0.0.1`，带本地 bearer token。
6. **中文优先**：CJK 检索是验收项，不是后续优化。
7. **平台**：macOS 优先（Apple Silicon），Windows/Linux 在架构上保持可移植，但不作为 v1 验收项。

## 已知风险与对策

| 风险 | 对策 |
|---|---|
| Wails v3（已到稳定 3.1.0） | 锁 exact 版本；保留浏览器 Web UI（`marktoast serve`）做开发/兜底 |
| moejs 仍是 alpha | 插件引擎推迟到 M5；v1 适配器全部 Go+YAML |
| 上游 harness 格式变动 | descriptor 化；适配器带版本探测与 fixture 测试 |
| modernc 不支持自定义 FTS5 tokenizer（已实测） | 走"写入/查询两侧同一预分词"：bigram（零依赖）+ gse（可选）；trigram 仅回退；需要更强分词再评估 gojieba（CGO）或 Bleve |
| Git 仓库膨胀 | debounce + 合并提交；`.git/info/exclude` 排除索引/Token；大仓库归档策略 |

## 决策记录

- 2026-11-04：技术栈定稿；前端确定为原生 HTML/CSS/JS（对齐 Magpie），不上 React。
- 2026-11-04：许可证定为 MIT；v1 平台 macOS 优先；记忆模型暂定单一真源（见 PRODUCT.md）。
- 2026-11-04：复用调研完成（[RESEARCH-REUSE.md](RESEARCH-REUSE.md)）：v1 不引入第三方记忆引擎；检索走 SQLite FTS5 + 预分词；MCP 选官方 `modelcontextprotocol/go-sdk`（备选 mark3labs/mcp-go）；会话读取参考 deja-vu（MIT）；AGPL 项目不进入二进制。
- 2026-11-04：M1–M3 落地：bigram FTS5 索引、CLI 全套、ZCode 幂等安装器、hub API、MCP stdio+HTTP、Magpie 风格 Web UI；debounce 提交已实现，gse 留作后续。
