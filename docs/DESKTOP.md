# 桌面壳（Wails v3）

> 状态：**已实现**（Wails **v3.1.0 稳定版**）。`make desktop` 产出 `bin/marktoast-desktop`，
> 启动时在进程内拉起（或复用）本地 hub，并打开原生窗口加载同一套 Web UI。

## 为什么是这个方案

- 与 Magpie 同一条路：Go 后端 + 系统 WebView + 原生 HTML/CSS/JS，**没有 Electron**。
- Wails v3 已有稳定版 `v3.1.0`（之前担心的 beta 风险已消除；nightly 仍是 `v3.2.0-nightly.*`）。
- 桌面壳只是"带窗口的 hub 客户端"：业务逻辑全在 `internal/*`，UI 与浏览器模式完全一致。

## 构建与运行

```bash
make desktop
./bin/marktoast-desktop                    # 默认 root 来自 config（~/Memory）
./bin/marktoast-desktop --root ~/Memory --addr 127.0.0.1:3777
```

- 壳内会启动 hub；如果 3777 已被 `marktoast serve` 占用，壳直接加载已存在的 hub。
- UI 已通过 `go:embed` 编进 CLI/桌面二进制；开发时设置 `MARKTOAST_UI_DIR` 可覆盖为 live 目录。
- 开发前端时用 `MARKTOAST_UI_DIR=./frontend marktoast serve` + 浏览器刷新更快；壳用于最终体验验证。

## 已实现

- [x] 原生窗口（1280×820，可缩放，深色背景）加载 Web UI
- [x] 单进程：壳内启动/连接 hub（HTTP + MCP 同进程）
- [x] `--root` / `--addr` 参数；config 默认值复用
- [x] Wails v3.1.0 锁版本；macOS 12.0 deployment target 与 CGO 构建已固定

- [x] 菜单栏托盘（显示窗口 / 浏览器打开 / 退出）
- [x] `.app` 打包脚本（`scripts/install.sh`、`make install`，含图标与 Info.plist）
- [x] 应用图标（吐司）与 Dock 图标

## 后续可选

- [x] 签名/公证门禁（脚本已支持 `CODESIGN_IDENTITY` / `NOTARY_PROFILE`；实际发布仍需证书）
- [ ] 自动更新（Wails updater）
- [x] `make release` / `scripts/release.sh`（签名校验、zip/dmg、SHA256、版本断言）
- [ ] 菜单里的"打开记忆仓库 / 重新索引"
- [ ] About 面板
