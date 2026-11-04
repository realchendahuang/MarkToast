# JS 插件

MarkToast 的插件宿主加载 `<memory-root>/.marktoast/plugins/*.js`，使用纯 Go 的
[goja](https://github.com/dop251/goja) 执行。脚本在本机以用户权限运行——**这是可信扩展点，
不是沙箱**。

## API

```js
marktoast.registerAdapter({
  id: "my-harness",
  title: "My Harness",
  detect: ["~/.my-harness"],
  tier: "projection",              // native | projection | readonly
  capabilities: ["read", "inject"],
  notes: "一句话说明"
});

marktoast.on("memory.saved", function (payload) {
  marktoast.log("saved " + payload.name);
});

marktoast.log("hello", marktoast.version, marktoast.root);
```

- `marktoast.registerAdapter(schema)`：注册/覆盖一个 adapter descriptor，字段与 YAML 适配器一致；注册后出现在适配器页与 `/api/adapters`。
- `marktoast.on(event, fn)`：订阅事件；事件参数是普通对象。
- `marktoast.log(...)`：写入宿主日志（内存中保留，调试用）。
- `marktoast.root` / `marktoast.version`。

## 事件

| 事件 | payload |
|---|---|
| `memory.saved` | `{path, project, name, type}` |
| `memory.restored` | `{path, project, name}` |
| `inbox.promoted` | `{path}` |

## 安装

```bash
mkdir -p ~/Memory/.marktoast/plugins
cp examples/plugins/hello.js ~/Memory/.marktoast/plugins/
# restart MarkToast or the hub
```

插件适配器会出现在适配器列表里；如果它带 `detect` 路径，本机存在该路径时会显示「已检测」。
脚本错误不会阻止 hub 启动，会写入服务日志。`moejs` 作为后续可选宿主，当前实现是 goja。
