# MarkToast 路线图

> 私有开发期路线图。每完成一个里程碑，更新 checkbox 与日期。

## M0 — 地基 ✅（2026-11-04）

- [x] 私有仓库 `realchendahuang/MarkToast`
- [x] README / 中文 README / 架构文档 / 路线图 / 复用调研 / harness 接入参考
- [x] Go module + CLI 骨架
- [x] `.gitignore` / Makefile

## M1 — 核心（store + index + git）✅（2026-11-04）

- [x] `marktoast init [--root <dir>]`：创建 / 接管记忆仓库
- [x] frontmatter 解析与事实文件读写（ZCode 兼容布局）
- [x] 索引生成器：从事实文件重建 `MEMORY.md`
- [x] SQLite FTS5 索引：`reindex` / 单条 upsert / CJK bigram 分词
- [x] `marktoast search "<query>" [--project] [--type]`（OR + BM25）
- [x] **Spike：modernc FTS5 + trigram/bigram/gse 中文检索验证**（结论见 RESEARCH-REUSE.md）
- [x] `marktoast add` / `get` / `log`：写记忆 + 自动索引 + 自动提交
- [x] `marktoast status`：仓库、索引、Git、适配器状态
- [x] 测试：store / index / gitx 各一组
- [x] debounce 批量提交（2026-11-04，hub 默认 5s 静默后提交，退出前 Flush）
- [x] `diff` / `rollback` 子命令（2026-11-04）

## M2 — 适配器（全量 40 家）✅（通用安装引擎完成；端到端逐家验证进行中）

目标从"先接三家"升级为 ai-memory 支持矩阵全量：**40 家 = 19 auto + 12 staged +
9 mcp_only**（tier：native 2 / projection 26 / readonly 12），一家一个
`internal/adapter/defaults/<id>.yaml`。

- [x] descriptor YAML schema 扩展：`install_mode`、`hooks.install_kind/schema/scope`、
      `mcp.kind/shape`、`docs`
- [x] 40 家内置描述符（40 个 YAML；`internal/adapter/adapter_test.go` 校验数量、id 唯一、
      `Validate()` 与字段不变量）
- [x] 通用安装引擎 `internal/harness`（替换 ZCode/DSH/Hermes 硬编码）：
  - [x] auto：按 schema 合并 hooks（`claude` / `flat` / `event_map` / `named_group` /
        `exec` / `exec_list` / `bundle` / `toml` 等）+ 按 shape 注册 MCP；
        写前备份（`<file>.marktoast-backup-<时间戳>`）、幂等、保留第三方条目、原子写
  - [x] staged：生成 `~/.marktoast/adapters/<id>/` 下的 hooks/mcp 片段与 README
        （hub URL、token、手贴步骤、坑）
  - [x] mcp_only：只生成 MCP 片段与说明
  - [x] 无法安全合并的 schema 自动降级 staged（报告说明原因）
  - [x] uninstall 精确回滚 MarkToast 条目并清理 staged 目录
- [x] CLI：`marktoast adapters list / install / uninstall / status` 全量支持
      （mode / detected / staged / installed；`--apply`、`--workspace`）
- [x] hub `/api/adapters` 返回 `mode` / `install_kind` / `staged` / `staged_ready` /
      `installed`，`installable` 不再写死 ZCode
- [x] Web UI 适配器页：40 家按 auto / staged / mcp_only 分组、mode 徽章、状态徽章、
      「接入 / 修复」「生成片段」「查看说明」按模式显示；中英文案
- [x] ZCode 幂等安装器（hooks + MCP，保留第三方条目）
- [x] DSH / Hermes staged 片段（Cordis patch / Hermes YAML）
- [x] `marktoast hook`：事件记录 + SessionStart 注入
- [x] 导入现有 525 条 ZCode 记忆 / Hermes MEMORY.md（2026-11-04，含去重与 global 布局修正）
- [x] Hermes memory provider 插件（完整生命周期，2026-11-04；`adapters/hermes/marktoast`，
      需 `hermes memory setup` 激活）
- [ ] **端到端逐家验证（auto，19 家）**：重启 harness 实测 hooks 触发、SessionStart 注入
      可见、MCP 可连；逐家结果回写 YAML 的 `notes`/`hooks.notes`
- [ ] **端到端逐家验证（staged，12 家）**：按 `~/.marktoast/adapters/<id>/README.txt`
      手贴后验证；重点 DSH cordis patch、Hermes `config.yaml` + trust gate、插件类
      （openclaw / oh-my-pi / pi / opencode / cline）
- [ ] **端到端逐家验证（mcp_only，9 家）**：客户端 MCP 工具列表与调用实测
- [ ] Claude Desktop 两个 id 分别验证：Chat（`npx -y mcp-remote http://127.0.0.1:3777/mcp`
      shim）与 Code tab（复用 `~/.claude/settings.json`）
- [ ] 注入缺口补齐：`pre_llm_call_context`（Hermes）输出 `{"context": ...}`、
      `mcp_handoff` 家的上下文兜底（DSH/Cline/Pi/Zero/Pool）
- [ ] 三家读到同一份记忆的联调复测（ZCode / Hermes / DSH）

## M3 — Hub（HTTP + MCP）✅（2026-11-04）

- [x] `marktoast serve`（localhost，bearer token on /mcp）
- [x] MCP server：`memory_search` / `memory_get` / `memory_save` / `memory_list`
- [x] MCP stdio（`marktoast mcp`）+ Streamable HTTP（`/mcp`）
- [x] HTTP API：status / search / memory / memories / projects / adapters / git-log
- [x] 降级：hub 停止时 Markdown 仍可读，CLI 仍可搜索（线性扫描回退）
- [x] 会话日志摄取 → `inbox/` 候选记忆（T2，2026-11-04；保留/丢弃 UI + CLI）
- [x] 敏感信息脱敏（高置信度规则：私钥 / OpenAI / GitHub / AWS / Google / Slack / Stripe / JWT / Bearer，2026-11-04）

## M4 — 桌面 UI ✅（Web UI + Wails 壳；diff/回滚与中英 i18n 已完成）

- [x] 浏览器 Web UI（Magpie 风格）由 hub 托管：搜索、详情、frontmatter、Git 历史、适配器、新建记忆、Cmd+K
- [x] 中文界面 + 离线/错误状态
- [x] Wails v3.1 桌面壳（`make desktop`；壳内启动/连接 hub，单进程）
- [x] 桌面壳加载同一 Web UI（原生窗口，1280×820）
- [x] 菜单栏常驻 / 托盘（2026-11-04）
- [x] 记忆详情里的 Git diff 与回滚按钮（2026-11-04）
- [x] 中英 i18n 切换（2026-11-04，设置页语言开关 + DOM 翻译层）

## M5 — 打磨与开源准备

- [x] 导入器：ZCode / Hermes（2026-11-04）；ai-memory / EverOS 待做
- [x] 插件宿主（goja，2026-11-04）：JS 注册 adapter + 事件；`docs/PLUGINS.md` + 示例
- [x] TUI（Bubble Tea）作为无 GUI/远程场景的第二界面（2026-11-04，搜索/列表/详情/滚动）
- [x] 手动备份（`marktoast backup` + 设置页按钮，tar.gz 到 ~/.marktoast/backups，2026-11-04）
- [x] 自动备份（启动时每日一次，2026-11-04）/ 安装脚本（`scripts/install.sh` + `make install`）
- [x] 更新检查（`marktoast update`，GitHub Releases，2026-11-04）
- [ ] 自动下载安装更新（需要已签名的 release）
- [x] 安全审计：脱敏上线、doctor 诊断（2026-11-04）
- [x] Hub 安全边界：loopback、Host/Origin、CSRF、Cookie/Bearer 认证、MCP fail-closed
- [x] 数据完整性：仓库写锁、原子写、文件名防覆盖、MEMORY.md 真实链接、受管 Git 提交
- [x] 前端可靠性：分页、请求超时、详情缓存刷新、大正文限制、modal/ARIA/i18n 修正
- [x] 发布工程：CI、非破坏性 verify、签名校验、版本统一、前端嵌入二进制
- [ ] 公开仓库前的数据清理
- [x] MIT LICENSE（2026-11-04）
- [ ] 公开仓库 + 文档站 + 发布首版

## 非目标（明确不做）

- 不做模型网关 / 账号路由 / 计费（Magpie 的领域）
- 不做云端记忆服务
- 不做 embedding 优先的重记忆引擎（向量仅作可选派生层）
