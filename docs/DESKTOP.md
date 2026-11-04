# 桌面壳（Wails v3）

> 状态：**已实现**（Wails **v3.1.0 稳定版**）。`make desktop` 产出 `bin/MarkToast`，
> 启动时在进程内拉起（或复用）本地 hub，并打开原生窗口加载同一套 Web UI。

## 为什么是这个方案

- 与 Magpie 同一条路：Go 后端 + 系统 WebView + 原生 HTML/CSS/JS，**没有 Electron**。
- Wails v3 已有稳定版 `v3.1.0`（之前担心的 beta 风险已消除；nightly 仍是 `v3.2.0-nightly.*`）。
- 桌面壳只是"带窗口的 hub 客户端"：业务逻辑全在 `internal/*`，UI 与浏览器模式完全一致。

## 构建与运行

```bash
make desktop
./bin/MarkToast                    # 默认 root 来自 config（~/Memory）
./bin/MarkToast --root ~/Memory --addr 127.0.0.1:3777
```

- 壳内会启动 hub；如果 3777 已被 `marktoast serve` 占用，壳直接加载已存在的 hub。
- UI 目录查找顺序：`MARKTOAST_UI_DIR` → `./frontend` → 可执行文件旁的 `frontend/`。
- 开发前端时用 `marktoast serve` + 浏览器热刷新更快；壳用于最终体验验证。

## 已实现

- [x] 原生窗口（1280×820，可缩放，深色背景）加载 Web UI
- [x] 单进程：壳内启动/连接 hub（HTTP + MCP 同进程）
- [x] `--root` / `--addr` 参数；config 默认值复用
- [x] Wails v3.1.0 锁版本；链接时的 macOS 版本警告来自上游对象文件，不影响运行

## 待办（M4 剩余）

- [ ] 菜单栏 / 托盘常驻与全局快捷键
- [ ] 应用打包（`.app` + Info.plist）、签名、公证
- [ ] 自动更新（Wails updater）
- [ ] 菜单里的"打开记忆仓库 / 重新索引 / 退出 hub"
- [ ] 图标与 About 面板
