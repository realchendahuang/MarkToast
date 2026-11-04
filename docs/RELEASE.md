# 发布清单

## 1. 前置条件

- Apple **Developer ID Application** 证书（Keychain 中可见）
- `xcrun notarytool` 的 keychain profile（Apple ID + app-specific password）
- GitHub `realchendahuang/MarkToast` 的发布权限；私有仓库的更新检查需要 PAT
- 决定仓库何时公开（MIT 已就绪，但公开是独立的动作）

## 2. 构建

```bash
make release VERSION=0.1.0
# 产出 dist/MarkToast-0.1.0-macos-arm64.{zip,dmg} + SHA256SUMS
```

正式签名与公证：

```bash
CODESIGN_IDENTITY="Developer ID Application: ..." \
NOTARY_PROFILE="marktoast-notary" \
./scripts/release.sh 0.1.0
```

## 3. 验证

```bash
shasum -a 256 -c dist/SHA256SUMS
codesign --verify --deep --strict dist/MarkToast.app
spctl --assess --type execute dist/MarkToast.app
./bin/marktoast update --repo realchendahuang/MarkToast
```

## 4. 发布

1. `git tag v0.1.0 && git push origin v0.1.0`
2. GitHub Release 上传 `.zip`、`.dmg`、`SHA256SUMS`
3. 更新检查：`GITHUB_TOKEN=... marktoast update`
4. 公开仓库 / 文档站（需 owner 明确同意）

## 当前阻塞

- 本机没有 Developer ID 证书与 notary profile，无法完成签名/公证
- 仓库还没有任何 GitHub Release，更新检查会返回 404
- 公开仓库尚未获得 owner 授权
