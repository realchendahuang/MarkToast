# 界面截图

这些 PNG 是 README 用的界面图，**全部来自一份 demo 记忆仓库**，不是任何人的真实记忆。

- 内容：35 条虚构事实，分 6 个项目（aurora / lumen / harbor / kepler / willow / atlas）
  加 `global`，覆盖 用户 / 反馈 / 项目 / 参考 / 决策 五种类型
- 尺寸：1280 × 800，浅色与深色各一张；页面本身是 1x CSS 像素
- 拍法：把 demo 仓库挂到 hub 上——`marktoast --root <demo目录> serve --addr 127.0.0.1:3799`，
  浏览器开 `http://127.0.0.1:3799/`，视口设 1280×800，再按页面截图

| 文件 | 页面 |
| --- | --- |
| `ui-light.png` | 记忆墙（浅色） |
| `ui-dark.png` | 记忆墙（深色） |
| `ui-detail.png` | 单条记忆的文档阅读视图（`release-pipeline`：有序列表 + 表格 + 行内码） |
| `ui-settings.png` | 设置 → 常规（外观 / 语言 / 快捷键 / 阅读字号） |
| `ui-history.png` | 历史（demo 仓库的 Git 提交） |
| `ui-adapters.png` | 适配器页（这一页反映本机安装的 harness，不是记忆内容） |

**不要把真实 `~/Memory` 的截图放进仓库**：事实正文里有客户名、内网地址、密钥片段。
