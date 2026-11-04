# 桌面壳（Wails v3）

> 状态：M4 才开工。技术栈已定（见 [TECH-STACK.md](TECH-STACK.md)），Wails v3 的 beta
> 风险已被接受：**锁版本 + 保留 Web UI 兜底**。

## 为什么现在不写

v0 的优先级是核心（store / index / git / CLI）与适配器；桌面壳是表现层，等 hub 的 API
稳定后再接，避免 Wails beta 的 API 变动把主线拖慢。因此：

- `cmd/marktoast-desktop/main.go` 目前带 `//go:build wails` 标签，默认不参与编译；
- 界面先用 `frontend/`（原生 HTML/CSS/JS，无框架）+ `marktoast serve` 的 Web UI 验证；
- Wails 依赖在 M4 加入 go.mod，并锁定 exact beta 版本。

## M4 开工清单

1. 安装 Wails v3 CLI：
   ```bash
   go install github.com/wailsapp/wails/v3/cmd/wails3@latest
   ```
2. 生成工程骨架（在本目录或临时目录）：
   ```bash
   cd cmd/marktoast-desktop
   wails3 init -n MarkToast -t vanilla
   ```
3. 把 `frontend/` 的静态资源接入 Wails 的 AssetServer；窗口指向 `/`。
4. 复用 `internal/hub` 启动本地服务；壳只做展示与交互，不复制业务逻辑。
5. 菜单栏常驻 + 窗口关闭行为（参考 Magpie 的产品体验）。
6. 锁定 `github.com/wailsapp/wails/v3` 版本，并记录在 TECH-STACK.md 的风险表。
7. 打包/签名/更新器在公开前处理（M5）。

## 构建命令（M4 完成后）

```bash
# 开发
wails3 dev

# 发布
wails3 build -tags wails
```

## 兜底

任何 Wails 阻塞都不影响使用：`marktoast serve` 的 Web UI 始终可用，CLI 始终可用。
