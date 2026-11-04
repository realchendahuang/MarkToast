# MarkToast 品牌

## 名字

- **Mark** = Markdown，记忆的载体
- **Toast** = 一片烤吐司，也是"碰杯/致意"
- **马克吐司** 中文名致敬马克·吐温

## 标记

一片**宽吐司**（拱顶、圆底、暖黄渐变），面包上烙着 **M↓**：

- M = Mark
- ↓ = Markdown 的记号

圆头描边、没有尖角，整体是"圆润、可爱、安静"的调子。

## 颜色

| 用途 | 浅色 | 深色 |
| --- | --- | --- |
| 面包体渐变 | `#FFD978` → `#F1B747` | 同 |
| 焦边 | `#C98B2E` | 同 |
| 面包上的记号 | `#5A3A12` | 同 |
| 字标 Mark | `#1C1C21` | `#F4F4F6` |
| 字标 Toast | `#B87A26` | `#F1B747` |
| App 图标底 | `#2B1D0E` | 同 |

## 字体

- **Fredoka 600**（SIL OFL 1.1）只用于字标 `MarkToast`
- 正文/界面继续用系统字体（SF Pro / PingFang SC）
- 字体文件：`frontend/fonts/fredoka-600.woff2`，许可证：`frontend/fonts/OFL.txt`

## 资产

| 文件 | 用途 |
| --- | --- |
| `frontend/index.html` 的 `#icon-logo` symbol | 侧栏 30px、弹层/窄屏 20px |
| `frontend/logo.svg` | favicon（16/32px） |
| `cmd/marktoast-desktop/icon.svg` | 应用图标源文件（奶油底 + 吐司切片 + M↓） |
| `cmd/marktoast-desktop/icon.png` | 1024px Wails / Dock 图标（透明圆角，无白边） |
| `docs/assets/logo.png` | README / 对外预览 |

## 用法

- 最小尺寸：界面内 20px；favicon 16px 时只保留整体轮廓即可
- 留白：标记四周至少留 25% 的空白
- 不要把吐司拉扁、换色、加阴影；M↓ 不要改方向
- 字标只使用 Fredoka 600 的双色组合，不单独改写"Toast"颜色
