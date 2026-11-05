# 发布清单

发布脚本只支持 macOS；其他系统会直接报错退出。正式分发要求 Developer ID
签名与 Apple 公证，本地验证包可使用显式的 unsigned 模式。

## 1. 安装 CLI（用户侧）

在仓库内：

```bash
go install ./cmd/marktoast
```

或安装最新发布版本：

```bash
go install github.com/realchendahuang/MarkToast/cmd/marktoast@latest
```

唯一 CLI 入口是 `cmd/marktoast`；仓库根目录没有 `main.go`。

## 2. 前置条件

- macOS 12.0+，Xcode Command Line Tools（`codesign`、`plutil`、`ditto`、`hdiutil`、
  `iconutil`、`sips`、`xcrun`）
- Go 1.26+ 与 Node.js 20+（`make verify` 会检查三个前端 JS 文件的语法）
- git 工作树干净：tracked、staged 和未忽略的 untracked 文件都不能存在
- 正式签名：Keychain 中可用的 **Developer ID Application** 证书，并设置
  `CODESIGN_IDENTITY`
- 可选公证：`xcrun notarytool` 的 keychain profile，并设置 `NOTARY_PROFILE`
- 发布用的 `VERSION` 必须是显式的 Apple 数字版本（1–3 段点分整数），例如
  `0.1.0`；不能省略，也不能使用 Makefile 的默认值。Git tag 仍使用 `v0.1.0`。

## 3. 构建与签名

```bash
make release VERSION=0.1.0
```

`release` 先运行 `make verify`；`scripts/release.sh` 也会再次运行 `make verify`
并检查 git 工作树。若未提供 `CODESIGN_IDENTITY`，脚本会失败，除非显式允许本地
unsigned 包：

```bash
ALLOW_UNSIGNED=1 make release VERSION=0.1.0
```

unsigned 模式仍会做 ad-hoc 签名并执行 `codesign --verify --deep --strict`，但不会
通过 Gatekeeper 分发检查。

正式签名并公证：

```bash
CODESIGN_IDENTITY="Developer ID Application: Your Name (TEAMID)" \
NOTARY_PROFILE="marktoast-notary" \
make release VERSION=0.1.0
```

也可以直接调用脚本；版本必须是单个显式参数，没有默认值：

```bash
CODESIGN_IDENTITY="Developer ID Application: Your Name (TEAMID)" \
NOTARY_PROFILE="marktoast-notary" \
./scripts/release.sh 0.1.0
```

脚本会：

1. 从 `VERSION` 渲染 `CFBundleShortVersionString` 与 `CFBundleVersion`，并设置
   `LSMinimumSystemVersion=12.0`
2. 构建 CLI 和 Wails 桌面壳；UI 已内嵌，不复制 `frontend/`
3. 把 `LICENSE` 和 `docs/CREDITS.md` 复制到 app 的 `Contents/Resources/`，分别命名为
   `LICENSE` 与 `THIRD_PARTY_NOTICES`
4. 真实签名失败即终止；无 identity 时 ad-hoc 签名；之后必须通过
   `codesign --verify --deep --strict`
5. 断言 CLI 输出与 Info.plist 的版本都等于请求的 `VERSION`
6. `NOTARY_PROFILE` 存在时提交 zip、staple app，再重新生成 zip/dmg 和校验和
7. 仅对完成公证的真实 Developer ID 签名运行 `spctl --assess --type execute`

产出：

```text
dist/MarkToast-0.1.0-macos-<goarch>.zip
dist/MarkToast-0.1.0-macos-<goarch>.dmg
dist/SHA256SUMS
```

`<goarch>` 来自 `go env GOARCH`（Apple Silicon 为 `arm64`）。`SHA256SUMS` 使用
`dist/` 内的 basename，便于下载后直接校验。

## 4. 验证

```bash
cd dist
shasum -a 256 -c SHA256SUMS
codesign --verify --deep --strict MarkToast.app
plutil -extract CFBundleShortVersionString raw MarkToast.app/Contents/Info.plist
./bin/marktoast version
```

只有正式签名且已公证的包才应运行 Gatekeeper 检查：

```bash
spctl --assess --type execute --verbose=4 MarkToast.app
```

## 5. 发布

1. 提交发布内容，确认工作树干净
2. `git tag v0.1.0 && git push origin v0.1.0`
3. GitHub Release 上传 `.zip`、`.dmg`、`SHA256SUMS`
4. 更新检查：`GITHUB_TOKEN=... marktoast update`
5. 公开仓库 / 文档站需 owner 明确同意

## 当前阻塞

- 若本机没有 Developer ID 证书与 notary profile，只能使用
  `ALLOW_UNSIGNED=1` 生成本地包，不能作为正式分发版本
- 未创建 GitHub Release 前，`marktoast update` 会返回 404
- 公开仓库尚未获得 owner 授权

## 6. CI 门禁

`.github/workflows/ci.yml` 在 push / pull request 时运行：

- Ubuntu：`go mod download`、`go mod verify`、`make fmt-check tidy-check vet
  test-race build`、三个前端 JS 语法检查、shell 语法检查
- macOS：使用 CGO 和 macOS 12.0 deployment target 构建 Wails 桌面二进制

发布仍建议在干净的、已签名的 macOS 本机或受控 release runner 上执行；CI 不会
替代证书、公证凭据和人工发布确认。
