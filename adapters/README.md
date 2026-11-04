# 适配器目录

MarkToast 的适配器是**声明式 YAML**：加一家 harness 是配置工作，不是核心代码。

- **内置默认值**在 `internal/adapter/defaults/*.yaml`（编译进二进制）。
- **用户覆盖**放在：
  - 开发时：仓库根目录 `adapters/`（本目录，存在时优先加载）
  - 安装后：`$(os.UserConfigDir)/marktoast/adapters/`（即将支持，见 ROADMAP M2）

同 `id` 的用户适配器会整体替换内置默认值。

## 示例

```yaml
id: my-harness
title: My Harness
detect:
  - ~/.my-harness
instruction_file:
  global: ~/.my-harness/AGENTS.md   # 会话启动时读取的指令文件
  project: "{workspace}/AGENTS.md"
  marker: "<!-- marktoast:begin -->" # 只维护 marker 之间的块
native_memory:
  root: ~/.my-harness/memories      # 原生记忆目录（没有就留空）
  index: MEMORY.md
session_logs: ~/.my-harness/sessions
tier: projection                    # native | projection | readonly
capabilities:
  - read
  - inject
notes: "一句话说明接入方式与已知限制。"
```

## 分级

| tier | 含义 |
|---|---|
| `native` | 有稳定的原生记忆格式，可双向读写 |
| `projection` | 只能投影（指令文件 / MCP / hooks），不直接写原生记忆 |
| `readonly` | 只读取会话或记忆，不写回 |
