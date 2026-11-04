# MarkToast 技术栈

> v0 · 已定稿（2026-11-04）。技术选型以"独自开发、本地优先、单二进制、可长期维护"为第一原则。

## 一句话

**Go 1.26 + Wails v3 + 原生 HTML/CSS/JS（Vite 构建）+ modernc SQLite(FTS5) + git CLI + moejs 插件（后期）**
——与 Magpie 同一条技术路线，但只做记忆。

## 决策总表

| 层 | 选择 | 版本/库 | 理由 | 被否方案 |
|---|---|---|---|---|
| 核心语言 | **Go** | 1.26.x | 单二进制、跨平台交叉编译、IO/并发简单；作者已有 Go 经验；与 Wails 同栈 | Rust（学习+编译成本）、TypeScript/Node（分发依赖运行时） |
| 桌面壳 | **Wails v3** | `github.com/wailsapp/wails/v3`（beta，锁版本） | 系统 WebView、单二进制、Mac/Windows/Linux；Magpie 同款 | Tauri 2（Rust 双语言）、Electron（体积/内存） |
| 前端 | **原生 HTML/CSS/JS** | Vite 构建；无框架；ES modules | 与 Magpie 一致：系统 WebView 内足够快、无框架负担、UI 测试用 `.cjs` 直跑 | React / Svelte / Vue（都为复杂状态引入，v1 不需要） |
| 样式 | **手写 CSS + design tokens** | CSS 变量 + 一套 token（颜色/间距/字号） | 与 Magpie 的克制风格一致；可主题化、中英文自适应 | Tailwind（构建链更重，与"手写精修"路线冲突） |
| TUI | **Bubble Tea + Lipgloss**（后期） | `charmbracelet/bubbletea` | Magpie 同款；给无 GUI 场景/远程使用 | 不做 TUI（少一个界面，但少一个卖点） |
| 数据库/索引 | **modernc.org/sqlite**（纯 Go） | 最新稳定版 | 无 CGo、交叉编译简单、内建 FTS5 | mattn/go-sqlite3（CGo）、Tantivy（额外索引层） |
| 全文检索 | **SQLite FTS5** | FTS5 + BM25 | 嵌入、零服务、可重建；中文先 trigram，后续 jieba 预分词 | Meilisearch/Typesense（重）、纯 grep（无排序） |
| Git | **调用 `git` CLI**（exec）+ 批量提交 | 系统 git | 行为可预期、实现简单、覆盖所有边界操作；开发者本机已有 git | go-git（纯 Go 但对复杂操作支持弱，后续可选） |
| Hub 协议 | **HTTP + MCP** | 官方 MCP Go SDK（备选 `mark3labs/mcp-go`） | 一套服务两用：UI/脚本走 HTTP，所有 agent 走 MCP | 仅 MCP（UI 不好走）、仅 HTTP（agent 接入面窄） |
| 适配器 | **YAML descriptor + Go 接口** | 自研 | 加一家 harness = 一个 YAML；社区可贡献 | 每家写死 Go 代码（不可扩展） |
| 插件引擎（后期） | **moejs**（Apache-2.0） | Magpie 同款 JS 网关插件引擎 | 复用成熟插件宿主，适配器可热插拔 | 自研插件系统（重复造轮子） |
| CLI | **cobra** | `spf13/cobra` | 子命令多、帮助/补全开箱即用 | 标准库 flag（子命令管理费劲） |
| 配置 | **TOML** | `BurntSushi/toml` | 可写注释、人读友好；Magpie 也用 TOML | JSON/YAML（注释/格式痛点） |
| 文件监听 | **fsnotify** | `fsnotify/fsnotify` | 手动编辑 Markdown 后自动重索引 | 轮询（耗电、慢） |
| 日志 | **log/slog** | 标准库 | 够用、结构化、无依赖 | zap/zerolog（额外依赖） |
| 测试 | `go test` + golden 文件；UI 用 Node `.cjs` 直跑 | — | 与 Magpie 的 UI 测试风格一致；先覆盖 store/index/git | Playwright（后续需要浏览器自动化时再加） |
| 分发 | 单二进制 + Wails app + 安装脚本；后期 Homebrew tap | — | macOS 优先 | 容器化（与本地桌面产品冲突） |

## "匹配 Magpie"具体指什么

Magpie 的栈（实测自 `yetone/magpie`，MIT）：

- Go 1.26 + **Wails v3**（`v3.0.0-beta.x`）系统 WebView
- 前端 **原生 HTML/CSS/JS**（仓库无 React/Vue，TS 仅少量），Vite 构建资源
- **moejs**（Apache-2.0）跑 JS 网关插件；插件市场独立仓库
- **modernc.org/sqlite**（纯 Go）；Bubble Tea/Lipgloss TUI；fsnotify；TOML/YAML
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
│   ├── gitx/                 # 批量提交、log/diff/rollback、sync
│   ├── hub/                  # HTTP + MCP server
│   ├── adapter/              # descriptor 加载、T1–T4 能力
│   ├── config/               # TOML
│   └── version/
├── ui/                       # Wails 前端：原生 HTML/CSS/JS + Vite
├── adapters/                 # zcode.yaml / dsh.yaml / hermes.yaml
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
| Wails v3 仍是 beta | 锁 exact 版本；保留"浏览器 Web UI"模式（`marktoast ui --web`）做开发/兜底 |
| moejs 仍是 alpha | 插件引擎推迟到 M5；v1 适配器全部 Go+YAML |
| 上游 harness 格式变动 | descriptor 化；适配器带版本探测与 fixture 测试 |
| modernc FTS5 能力边界 | M1 先写 spike 验证 FTS5 + trigram（含中文两字词）；不行再换分词方案 |
| Git 仓库膨胀 | debounce + 合并提交；`.gitignore` 排除索引；大仓库归档策略 |

## 决策记录

- 2026-11-04：技术栈定稿；前端确定为原生 HTML/CSS/JS（对齐 Magpie），不上 React。
- 2026-11-04：许可证方向 MIT；v1 平台 macOS 优先；记忆模型暂定单一真源（见 PRODUCT.md）。
