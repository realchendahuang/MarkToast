# magpie 前端可借鉴清单（对 MarkToast）

> 参考对象：github.com/yetone/magpie（MIT），前端 `internal/gui/assets/`，零构建零依赖。
> 本清单只收录 MarkToast **尚未**采用的写法；已移植的 magpie token/组件不重复列举。
> 快照：2026-11-07 17:35。同一轮已落地的改动见 commit `a790184`（等网格卡片、popover 筛选、`/` 与方向键、阅读字号、吸顶筛选、复制变勾）；本清单只收**还没做**的部分，个别条目括注已落地的半边。

## 已吸收（一句话盘点，不展开）
- token 体系：`--seg-track`、`--seg-thumb`、`--pop-bg`、`--pill`、`--line`、`--ease`（已是 magpie 的 `cubic-bezier(.2,.8,.2,1)`）见 `frontend/style.css:9-73,1432`。
- `.seg` 分段控件 + `.thumb` 滑块（`app.js:2531-2551`）、`.side-hl` 侧栏滑块（`app.js:845-870`）、`view-in` 与 1200px 内容列（`style.css:1391-1392`）。
- dialog/遮罩/toast/空态/骨架、`focus-visible`、`inert` 与焦点归还；⌘K、三个弹层的 Tab 陷阱、`aria-live`/`aria-busy`（`style.css:721-802`、`app.js:1166-1175,2807-2811,2911-2985`、`index.html:230,261`）。
- 8px 细滚动条与 `prefers-reduced-motion`（`style.css:879-885,1457-1459`）；但 `--bar` 只被引用没有赋值，见第 1 条。

## 可借鉴清单（按性价比排序）
### 1. 滚动条宽度实测进 `--bar`
> 状态：`--bar` 的实测已落地（`measureScrollbarWidth()`，`app.js`）；`overflow-y: scroll` 常驻滚动槽与右 padding 归一仍未做。
- **问题/现状**：`frontend/style.css:1392` 的 `.view` 已经在用 `var(--bar)`，全项目却没有一处 JS 给它赋值；`.view` 仍是 `overflow-y:auto`（`style.css:361-364`），全局 8px 滚动条（`style.css:1457-1459`）出现/消失时，1200px 内容列会窄 8px（宽窗口下居中列还会左移约 4px），切换空列表与长列表会横向抖一下。
- **magpie 做法**：`layout.js` 在 body 开头放一个隐藏的 `.view`，量 `offsetWidth - clientWidth` 写进 `--bar`，resize 时重测；`.view` 用 `overflow-y: scroll` 常驻滚动槽，右 padding 用 `calc(16px - var(--bar))` 抵消（`app.css:588,602`、`layout.js:22-25`）。
```js
function bar() {
  var p = document.createElement("div");
  p.className = "view";
  p.style.cssText = "position:absolute;visibility:hidden;height:9px";
  document.body.appendChild(p);
  document.documentElement.style.setProperty("--bar", p.offsetWidth - p.clientWidth + "px");
  p.remove();
}
bar(); addEventListener("resize", bar);
```
- **落到 MarkToast**：新增 `frontend/layout.js` 放在 `<body>` 开头、`embed.go` 的 `//go:embed` 补一行；`.view` 基础规则改 `overflow-y: scroll`；`style.css:1392` 的右 padding 统一用 `max(0px, calc(16px - var(--bar, 0px)))`。
- **代价**：S。

### 2. 复制按钮就地变勾
> 状态：已落地（`flashCopied()`，设置页两个复制按钮与详情页两个 icon 按钮都走它）。
- **问题/现状**：`app.js:3007-3013` 的 `copyText()` 只发一条 toast；`#copy-root`（`index.html:353`）与 `#copy-token`（`index.html:396`）都是只有标题的图标按钮，复制成功后按钮本身没有变化，视线得从按钮跳到右下角。
- **magpie 做法**：`flashCopied` 把图标换成对勾、加 `.done` 绿色呼吸（`copy-pop .36s`），1.2s 后自动还原；页脚状态条仍说明“复制了什么”，按钮回答“落到了”（`app.js:6833-6850`、`app.css:2559-2562`）。
```js
function flashCopied(b) { if (!b) return;
  b.classList.remove("done"); void b.offsetWidth; // 再点一次重播
  b.classList.add("done");
  b.title = t("Copied");
  b.replaceChildren(svg(CHECK, 12, 1.7));
  clearTimeout(b.copiedT);
  b.copiedT = setTimeout(() => { b.classList.remove("done"); b.title = t("Copy"); b.replaceChildren(svg(COPY_ICON, 12, 1.5)); }, 1200);
}
```
- **落到 MarkToast**：`copyText(value,label,button)` 接收按钮，成功后 `use` 切到 `#icon-check`、加 `.done`，1.2s 还原；设置页的两个复制按钮与详情页“复制 Markdown”都走这一条；`.done` 用现有 `--green`/`--green-soft`。
- **代价**：S。

### 3. `⌘,` 与 `⌃⌘S`
- **问题/现状**：只有 ⌘K（`app.js:2807-2811`）；`#open-settings`（`index.html:154`）与两个 `#side-toggle`（`index.html:107,162`）都没有键位；标题里也不带键位。
- **magpie 做法**：`⌘,` 从窗口、快面板、输入框里都能打开设置（`app.js:19306-19311`）；侧栏按钮的 title 里带 `⌃⌘S`（`app.js:23698-23705`）；`data-keys` 在 i18n 翻译 title 后按平台追加键位（`i18n.js:18911-18918`）。
```js
if (!web) addEventListener("keydown", (e) => {
  const mod = /^Mac/.test(navigator.platform) ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey;
  if (!mod || e.altKey || e.shiftKey || e.repeat || (e.key !== "," && e.code !== "Comma")) return;
  e.preventDefault();
  openSettings();
}, true);
```
- **落到 MarkToast**：在现有全局 keydown 里加 `⌘,`（`switchView("settings")`）与 `⌃⌘S`（点 `#side-toggle`）；给三处按钮加 `data-keys`，在 `i18n.js` 的 `translateAttributes` 里“翻译后追加”——先把原文存 `data-title`，别直接往 `title` 追加，否则切语言会越加越长。
- **代价**：S。

### 4. 滚动端点胶囊
- **问题/现状**：没有回顶/回底控件；`.view` 与记忆详情的 `#detail-scroll`（`style.css:1928`）都只能手滚，长记忆读到一半想回顶部要一路滚。
- **magpie 做法**：`scrollends.js` 造一个右下角固定胶囊，`scrollTop > clientHeight` 才出现；无右侧留白时接近底部自动隐藏，悬停/聚焦时保持；用 `aria-disabled` 表示两端；`scroll` 捕获 + rAF 合并（`app.css:2257-2263`、`scrollends.js:19-38,48-49`）。
```js
const open = () => [...document.querySelectorAll(".view")].find((v) => !v.hidden);
const atEnd = (v) => v.scrollTop + v.clientHeight >= v.scrollHeight - 2;
const v = open(), modal = document.getElementById("modal");
if (v) watch(v);
const held = box.contains(document.activeElement) || box.matches(":hover");
let on = !!v && (!modal || modal.hidden) && v.scrollTop > v.clientHeight;
if (!v || !on && !held) { box.classList.remove("on"); return; }
```
- **落到 MarkToast**：在 `app.js` 末尾建一个 `.view-ends`，同时 watch 当前 `.view` 与 `#detail-scroll`；按钮点击 `scrollTo({top: 0} / {top: scrollHeight})`，`behavior` 在 reduced motion 下用 `"auto"`；样式照 `app.css:2257-2263` 的胶囊精简成两个按钮 + 分隔线。
- **代价**：M。

### 5. 单条居中状态胶囊
- **问题/现状**：`toast()` 把消息追加到右下角堆栈（`app.js:340-350`、`style.css:775`），连续复制/保存会叠一排；消息只属于角落，长文案被 340px 宽限制。
- **magpie 做法**：窗口里只有一条 `.status.lift`，底部居中、`color-mix` 同色底 + 内描边 + 阴影；非错误 `pointer-events:none` 不挡下面的行，错误可选中复制；长文案换行到 560px、超出滚动（`app.css:2240-2244,2253-2254,2267`）。
```css
.status.lift {
  position: fixed; left: 50%; bottom: 18px; transform: translateX(-50%);
  padding: 3px 10px; border-radius: 11px;
  background: color-mix(in srgb, currentColor 16%, var(--card));
  box-shadow: inset 0 0 0 1px color-mix(in srgb, currentColor 30%, transparent), 0 6px 20px rgba(0, 0, 0, .28);
  animation: fade-in .2s var(--ease);
}
```
- **落到 MarkToast**：加 `singleStatus(text, kind)`：先清空 `#toast-region` 再放一条，居中；复制/保存/刷新成功用它，错误保留 bottom-right 的 `toast` 或让它可选中；`aria-live` 区域不变。
- **代价**：S。

### 6. 侧栏滚动条（`sideBars`）
- **问题/现状**：`.side-scroll` 与 `.project-list` 都把原生条藏了（`style.css:169-170,1226-1233`），项目列表 `max-height:300px`，项目一多就不知道滚到哪，也不能拖。
- **magpie 做法**：`sideBars` 在侧栏滚动盒上盖一条 6px 绝对定位细条，滚动时 `.on` 淡入、停 900ms 后淡出；按下条子上/下翻页，按住 thumb 拖动滚动；ResizeObserver + MutationObserver 重排（`app.css:402-409`、`app.js:23635-23695`）。
```js
const ch = box.clientHeight, sh = box.scrollHeight;
bar.hidden = !ch || sh <= ch + 1;
if (bar.hidden) return;
const h = Math.max(24, Math.round(ch * ch / sh));
const y = Math.round((ch - h) * box.scrollTop / (sh - ch));
thumb.style.height = h + "px";
thumb.style.transform = `translateY(${y}px)`;
```
- **落到 MarkToast**：在 `app.js` 给 `#project-list` 与 `.side-scroll` 各建一个 `.side-bar`；CSS 照 `app.css:404-409`；`.sidebar` 已是 `position:fixed`，`.side-scroll` 在 `style.css:1463` 已有 `position:relative`，容器就位。
- **代价**：M。

### 7. 侧栏拖拽调宽
- **问题/现状**：`--side-w: 212px` 固定（`style.css:39`），`body` 的 `padding-left` 与 `.sidebar` 的宽都吃这个变量（`style.css:81,133`）；只有折叠按钮（`app.js:2969-2988`），不能拖。
- **magpie 做法**：`.side-edge` 8px 命中区（粗指针 24px），`setPointerCapture` 拖动，clamp 168–320 且不超过窗宽 36%；拖过最小值 64px 折叠；宽度存 localStorage；方向键 ±8、Shift ±32；写 `aria-valuenow/min/max`（`app.css:380-384`、`app.js:23572-23630`）。
```js
edge.addEventListener("pointerdown", (e) => {
  e.preventDefault();
  edge.setPointerCapture(e.pointerId);
  const from = side.getBoundingClientRect().width - e.clientX;
  const move = (m) => {
    const w = Math.max(168, Math.min(from + m.clientX, 320, innerWidth * 0.36));
    document.body.style.setProperty("--side-w", w + "px");
  };
});
```
- **落到 MarkToast**：在 `.sidebar` 右缘加 `.side-edge`（`role="separator"`、`tabindex="0"`、`aria-orientation="vertical"`），JS 指针捕获 + localStorage `marktoast-side-w`；折叠阈值复用 `side-collapsed`；键盘左右调宽。
- **代价**：M。

### 8. navlamp 悬停火炬
- **问题/现状**：`side-hl` 只跟着“选中行”滑（`app.js:845-870`），悬停只是换背景（`style.css:198,1429`）；侧栏没有 magpie 那种“光跟着指针走”的手感。
- **magpie 做法**：`navlamp.js` 在 pointermove 里找行与词，rAF 把 `--si` 向 0/1 lerp、把 `--lx` 设为指针在词内的 x；CSS 用 36px 径向渐变裁到文字，热区随指针移动，移开慢慢灭；触摸不点亮，reduced motion 直接到位（`navlamp.js:14-35,39-46`、`app.css:466-472`）。
```js
document.addEventListener("pointermove", (e) => {
  const row = e.pointerType === "touch" || !e.target.closest ? null : e.target.closest(".nav-sub > .nav-s, body.side-nav :is(#nav button[data-view], #prefs, #sideGw)");
  const word = row && row.querySelector(":scope > span");
  if (word && !at) x = e.clientX;
  tx = e.clientX; if (word !== at) at = word;
  if (at || lit.size) go();
}, { passive: true });
```
- **落到 MarkToast**：给 `.side-row` 的 `.side-name` 加 `--si`/`--lx`，在 `.sidebar` 上挂一个 passive `pointermove`，rAF 里只更新未选中的行；CSS 用 `background-clip:text`；`prefers-reduced-motion` 时跳过 lerp。
- **代价**：S（约 40 行）。

### 9. 折叠/追加列表的逐行“卷轴”
- **问题/现状**：设置页切 tab 是瞬间 `hidden`（`app.js:2372-2374`）；“加载更多”会 `clear` 后整块重画（`app.js:1071-1129`），没有逐行延迟，没有高度展开的 soft edge。
- **magpie 做法**：`grid-template-rows: 0fr → 1fr` 做高度折叠，展开 `.62s` 弹簧、收起 `.42s` 更快；每行 `transition-delay: calc(70ms + var(--i) * 48ms)`，边缘 32px 的 mask 让“卷”的边是软的（`app.css:662-668`）。
```css
.agent-fold { display: grid; grid-template-rows: 0fr; transition: grid-template-rows .42s cubic-bezier(.4, 0, .2, 1); }
.agent-fold.open { grid-template-rows: 1fr; transition-duration: .62s; transition-timing-function: cubic-bezier(.22, 1, .36, 1); }
.agent-fold-inner { min-height: 0; overflow: hidden; }
.agent-fold .row { opacity: 0; transform: translateY(-10px) scale(.985); }
.agent-fold.open .row {
  opacity: 1; transform: none;
  transition-delay: calc(70ms + var(--i) * 48ms);
}
```
- **落到 MarkToast**：设置页切 tab 时给可见 `.row.pref` 编 `--i` 并跑 `.34s` 的 row-in；“加载更多”不要重播全部卡片——在 `renderMemories` 前记住旧路径集，只给新增批次设 `--i`；若以后加折叠区，直接复用 0fr→1fr。
- **代价**：M。

### 10. 弹层入场分两段
- **问题/现状**：新加的 `.pop` 是单段 `pop-in .14s`（`style.css:423-425`）；`.dialog` 是 `dialog-in .22s`（`style.css:1605`）；`.toast` 是 `.18s`——都是“一条曲线走完”，没有 magpie 那种“先答到、再长尾落定”的手感。
- **magpie 做法**：透明度 `.18s ease-out` 先到，transform 用 `.42s cubic-bezier(.16,1,.3,1)` 长尾；`transform-origin: var(--ox)` 从触发点长出，向上翻时原点在底部；离场更快 `.22s forwards`；reduced motion 只留淡出（`app.css:2279-2291`）。
```css
.pop {
  transform-origin: var(--ox, 24px) top;
  animation: pop-fade .18s ease-out, pop-in .42s cubic-bezier(.16, 1, .3, 1);
}
@keyframes pop-fade { from { opacity: 0; } }
@keyframes pop-in { from { transform: translateY(var(--pop-dy, -6px)) scale(.94); } }
.pop.leaving { animation: pop-out .22s cubic-bezier(.4, 0, .7, .2) forwards; }
@keyframes pop-out { to { opacity: 0; transform: translateY(calc(var(--pop-dy, -6px) * .6)) scale(.96); } }
```
- **落到 MarkToast**：`.pop` 与 `.dialog` 都拆成两段；`.dialog` 原点 `50% 18%`，popover 用触发按钮的 x 写 `--ox`；出场用更短的 `.18s`；在 `.pop` 的 JS 关闭路径上先加 `.leaving` 再移除。
- **代价**：S。

## 明确不借的
- **多平台/多外壳分支**：`body.win|linux|web|panel`、WebView2/GTK 兼容、Safari 15 fallback、`view-transition` 分路径（`app.css:212-214,345-357,555-571,625-636`）。MarkToast 只跑 macOS Wails 窗口，抄来是永久维护成本。
- **插件页 UI**：`plugins.css`（318 行）、`pluginpages.js`、`plugins/` 是给第三方整页的宿主；MarkToast 的 goja 插件只注册 adapter，没有第三方页面，CSP `script-src 'self'` 也不允许注入。
- **Usage 图表**：`.led-chart`、`.heat-grid`、quota curve（`app.css:1320-1530`）依赖 token 用量数据；MarkToast 没有这份数据，组件无落点。
- **`view-room` 滚动锚定**：`app.js:22940-23120` 近 200 行，为“列表上方增删时保持阅读位置”；MarkToast 详情在独立 modal、列表只追加，问题不存在。

## 出处
- magpie：`github.com/yetone/magpie`，MIT License（`LICENSE`，Copyright 2026 yetone）；本机 shallow clone HEAD `191f3c0af1934fbbec87dc589fddf26913699aa9`（2026-11-07）。
- 前端：`internal/gui/assets/app.css`（4902 行）、`app.js`（24463 行）、`index.html`、`layout.js`、`navlamp.js`、`scrollends.js`、`i18n.js`、`theme.js`。
- 视觉核对：`site/public/img/agents-light.png`、`panel-light.png`（2x）。
- MarkToast 现状：`frontend/style.css`、`frontend/index.html`、`frontend/app.js`、`frontend/i18n.js`；CSP 见 `internal/hub/security.go:28`。
