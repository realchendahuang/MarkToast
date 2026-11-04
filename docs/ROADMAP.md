# MarkToast 路线图

> 私有开发期路线图。每完成一个里程碑，更新 checkbox 与日期。

## M0 — 地基 ✅（2026-11-04）

- [x] 私有仓库 `realchendahuang/MarkToast`
- [x] README / 中文 README / 架构文档 / 路线图
- [x] Go module + CLI 骨架（`marktoast version`）
- [x] `.gitignore` / Makefile

## M1 — 核心 CLI（store + index + git）

- [ ] `marktoast init [--root <dir>]`：创建 / 接管记忆仓库
- [ ] frontmatter 解析与事实文件读写（ZCode 兼容）
- [ ] 索引生成器：从事实文件重建 `MEMORY.md`
- [ ] SQLite FTS5 索引：`reindex` / 增量更新 / CJK 分词
- [ ] `marktoast search "<query>" [--project] [--type]`
- [ ] **Spike：modernc FTS5 + trigram/jieba 中文检索验证**（两字词/中英混排）
- [ ] Git：debounce 批量提交、`log` / `diff` / `rollback`
- [ ] `marktoast status`：仓库、索引、Git 积压、适配器状态
- [ ] 测试：store / index / git 各一组

## M2 — 适配器（先三家）

- [ ] descriptor YAML schema + 加载器
- [ ] **ZCode**：T1 指令投影 + 原生记忆目录读写 + 导入现有 525 条
- [ ] **DSH**：cordis.patch 注入 + MCP 配置生成
- [ ] **Hermes**：provider / hooks 配置生成 + MEMORY.md/USER.md 导入
- [ ] `marktoast adapters status|install|uninstall`
- [ ] 端到端验证：三家读到同一份记忆

## M3 — Hub（HTTP + MCP）

- [ ] `marktoast serve`（localhost，bearer token）
- [ ] MCP server：`memory_search` / `memory_get` / `memory_save` / `memory_list`
- [ ] HTTP API（给 UI 与脚本用）
- [ ] 会话日志摄取 → `inbox/` 候选记忆（T2）
- [ ] 降级：hub 停止时只读可用

## M4 — 桌面 UI（Wails v3）

- [ ] Wails 壳 + 菜单栏常驻
- [ ] 前端：**原生 HTML/CSS/JS + Vite（对齐 Magpie，无框架）**
- [ ] 先做浏览器 Web UI 兜底（`marktoast ui --web`），再接 Wails（把壳当风险，不当前提）
- [ ] 记忆浏览器（项目 / 类型 / 搜索）
- [ ] Git 历史与 diff、回滚
- [ ] 适配器状态页（每家 harness 的接入健康度）
- [ ] 中文 / 英文 i18n

## M5 — 打磨与开源准备

- [ ] 导入器：ai-memory / ZCode / Hermes / EverOS 格式
- [ ] 插件宿主（moejs）与 adapter 贡献文档
- [ ] TUI（Bubble Tea）作为无 GUI/远程场景的第二界面
- [ ] 自动备份 / 更新器 / 安装脚本
- [ ] 安全审计：脱敏、token、公开仓库前的数据清理
- [ ] 公开仓库 + 选许可证（MIT 倾向）+ 文档站 + 发布首版

## 非目标（明确不做）

- 不做模型网关 / 账号路由 / 计费（Magpie 的领域）
- 不做云端记忆服务
- 不做 embedding 优先的重记忆引擎（向量仅作可选派生层）
