# 致敬与来源

MarkToast 不是从石头里蹦出来的。它的产品形态、存储格式和工程纪律来自下面这些
开源项目与团队。我们**借鉴的是设计与模式**；任何直接复用的代码都会按原许可证保留
版权与许可声明。本文件随实现持续更新。

> 若你是这些项目的维护者，发现这里的表述或引用方式不妥，请开 issue，我们会改。

## 形态与工程纪律

| 项目 | 许可 | 我们借鉴了什么 |
|---|---|---|
| [magpie](https://github.com/yetone/magpie)（yetone，**29k★**） | MIT | 桌面产品的整体形态：Go + Wails v3 单二进制、系统 WebView、原生 HTML/CSS/JS 无框架、菜单栏 + TUI 多界面、插件引擎（moejs）、配置写入器（"只改每个 agent 最关键的那一个键"）、备份/更新/i18n 等产品化细节。MarkToast 只做记忆，不做模型网关。 |
| [ai-memory](https://github.com/akitaonrails/ai-memory)（**15k★**） | MIT | "Markdown 是源、索引是派生、Git 是版本"的记忆引擎思路；批量提交/降级/零云端等工程取舍。 |
| [Wails](https://github.com/wailsapp/wails) | MIT | 桌面壳框架（v3 beta）。 |
| [moejs](https://github.com/MoeForge/moejs) | Apache-2.0 | 后期插件引擎候选（适配器插件化）。 |

## 记忆格式与协议

| 项目 | 许可 | 我们借鉴了什么 |
|---|---|---|
| [ZCode](https://github.com/zai-org/ZCode)（智谱，**8.4k★**） | Apache-2.0 | **项目记忆文件格式**：`memories/projects/<slug>-<sha256[:16]>/memory/`、`MEMORY.md` 索引、一条事实一个 Markdown 文件、YAML frontmatter（`name` / `description` / `metadata.type` / `originSessionId`）、指令文件投影与索引注入。 |
| [Hermes Agent](https://github.com/NousResearch/hermes-agent)（Nous Research） | MIT | `MEMORY.md` / `USER.md` 双库、`§` 分隔、按字符预算注入、external memory provider 插件接口。 |
| [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) | MIT | 插件/工具/MCP 接缝与实验性会话搜索；MarkToast 作为其外部记忆插件接入。 |
| [EverOS](https://github.com/EverMind-AI/EverOS)（**13.8k★**） | Apache-2.0 | "local-first + Markdown + 多 harness 插件"的生态组织方式。 |
| [memsearch](https://github.com/zilliztech/memsearch)（Zilliz） | MIT | "Markdown 源 + 派生语义索引"的混合检索思路。 |
| [deja-vu](https://github.com/vshulcz/deja-vu) | MIT | Go 写的会话读取器（43 agents，含 ZCode/Hermes/DSH）、`cjkfold` 繁简折叠、`atomicfile` 原子写——会话摄取与 CJK 细节的重要参考。 |
| [Letta / memfs](https://github.com/letta-ai/letta-code) | Apache-2.0 | 根 `MEMORY.md` 作索引、子目录各自 `MEMORY.md`、git 冲突四态与 attempt token。 |
| [go-ego/gse](https://github.com/go-ego/gse) | Apache-2.0 | 纯 Go 中文分词（可选精度路径；bigram 快路径之外的选择）。 |
| [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) | MIT | MCP server 的 Go 实现（stdio + streamable HTTP）。 |
| [gitleaks](https://github.com/gitleaks/gitleaks) | MIT | 写入记忆前的密钥/隐私脱敏规则集。 |

## 依赖

Go 依赖的许可证以各自仓库为准：`spf13/cobra`（Apache-2.0）、`BurntSushi/toml`（MIT）、
`gopkg.in/yaml.v3`（MIT/Apache-2.0）、`modernc.org/sqlite`（BSD-3-Clause）、
`fsnotify`（BSD-3-Clause）、`charmbracelet/bubbletea`（MIT）。

## 我们刻意不做的

- 不做模型网关 / 账号路由 / 计费（Magpie 的领域）
- 不做云端记忆 API（Mem0 / Honcho / Zep 的领域）
- 不做会话压缩型记忆（claude-mem 的方向）
