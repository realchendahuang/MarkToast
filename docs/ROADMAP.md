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
- [ ] debounce 批量提交（当前一次变更一次 commit）
- [ ] `diff` / `rollback` 子命令（当前用 git 原生）

## M2 — 适配器（先三家）🟡（骨架完成，端到端待验证）

- [x] descriptor YAML schema（instruction / native memory / hooks / mcp / session）
- [x] **ZCode**：幂等安装器（插件优先说明 + config.json hooks 合并 + MCP 合并 + 备份 + 卸载）
- [x] **DSH**：staged hooks.json + cordis patch 说明（半自动）
- [x] **Hermes**：staged hooks/mcp 片段 + 说明（半自动）
- [x] `marktoast adapters list|install|uninstall|status`
- [x] `marktoast hook`：事件记录 + SessionStart 注入
- [ ] 导入现有 525 条 ZCode 记忆 / Hermes MEMORY.md
- [ ] 端到端验证：三家读到同一份记忆（需要重启各 harness 实测）
- [ ] Hermes memory provider 插件（完整生命周期）

## M3 — Hub（HTTP + MCP）✅（2026-11-04）

- [x] `marktoast serve`（localhost，bearer token on /mcp）
- [x] MCP server：`memory_search` / `memory_get` / `memory_save` / `memory_list`
- [x] MCP stdio（`marktoast mcp`）+ Streamable HTTP（`/mcp`）
- [x] HTTP API：status / search / memory / memories / projects / adapters / git-log
- [x] 降级：hub 停止时 Markdown 仍可读，CLI 仍可搜索（线性扫描回退）
- [ ] 会话日志摄取 → `inbox/` 候选记忆（T2）
- [ ] 敏感信息脱敏（gitleaks 规则）

## M4 — 桌面 UI 🟡（Web UI 完成，Wails 壳待接）

- [x] 浏览器 Web UI（Magpie 风格）由 hub 托管：搜索、详情、frontmatter、Git 历史、适配器、新建记忆、Cmd+K
- [x] 中文界面 + 离线/错误状态
- [ ] Wails v3 桌面壳 + 菜单栏常驻
- [ ] 记忆详情里的 Git diff 与回滚按钮
- [ ] 中英 i18n 切换
- [ ] 桌面壳内嵌 hub（单进程启动）

## M5 — 打磨与开源准备

- [ ] 导入器：ai-memory / ZCode / Hermes / EverOS 格式
- [ ] 插件宿主（moejs）与 adapter 贡献文档
- [ ] TUI（Bubble Tea）作为无 GUI/远程场景的第二界面
- [ ] 自动备份 / 更新器 / 安装脚本
- [ ] 安全审计：脱敏、token、公开仓库前的数据清理
- [ ] 公开仓库 + MIT 许可证 + 文档站 + 发布首版

## 非目标（明确不做）

- 不做模型网关 / 账号路由 / 计费（Magpie 的领域）
- 不做云端记忆服务
- 不做 embedding 优先的重记忆引擎（向量仅作可选派生层）
