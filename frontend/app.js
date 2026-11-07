// MarkToast web UI — dependency-free and served directly by the local hub.

"use strict";

const $ = (id) => document.getElementById(id);
const tr = (text) => (window.MarkToastI18n ? window.MarkToastI18n.t(text) : text);
// UI strings that leave the DOM (window.confirm) or are mixed into larger
// dynamic strings need the current language directly; plain DOM text is
// translated by the i18n MutationObserver instead.
const localized = (text) => (window.MarkToastI18n?.language === "en" ? window.MarkToastI18n.t(text) : text);

const API_TIMEOUT_MS = 15_000;
const MAX_RENDERED_BODY_CHARS = 200_000;
const MEMORY_PAGE_SIZE = 50;
const SEARCH_PAGE_SIZE = 20;
const MAX_DETAIL_CACHE_ENTRIES = 80;

const state = {
  status: null,
  root: "",
  view: "memories",
  project: "",
  type: "",
  query: "",
  sort: "relevance",
  results: [],
  total: 0,
  totalKnown: false,
  hasMore: false,
  offset: 0,
  limit: MEMORY_PAGE_SIZE,
  loadingMore: false,
  selectedPath: "",
  detailScrollPath: "",
  details: new Map(),
  detailErrors: new Map(),
  detailLoading: false,
  adapters: [],
  adapterInfo: null,
  inbox: [],
  history: [],
  aliases: {},
  settings: null,
  memoryHistory: {
    path: "",
    open: false,
    loading: false,
    commits: [],
    error: null,
    selectedHash: "",
    patch: "",
    patchError: null,
    patchLoading: false,
    restoring: false,
  },
  searchController: null,
  searchTimer: 0,
  searchGeneration: 0,
  requestId: 0,
  newFrom: 0,
  lastFocus: null,
};

const els = {
  hubAlert: $("hub-alert"),
  hubAlertText: $("hub-alert-text"),
  hubStateText: $("hub-state-text"),
  statusDot: $("side-status-dot"),
  search: $("global-search"),
  searchShell: $("search-shell"),
  searchToggle: $("search-toggle"),
  searchClear: $("search-clear"),
  projectList: $("project-list"),
  projectFilter: $("project-filter"),
  typeFilter: $("type-filter"),
  resultRegion: $("memory-results"),
  resultTotal: $("result-total"),
  adapterRegion: $("adapter-results"),
  adapterCount: $("adapter-count"),
  historyRegion: $("history-results"),
  modal: $("memory-modal"),
  detailModal: $("memory-detail-modal"),
  adapterModal: $("adapter-modal"),
  adapterDialogTitle: $("adapter-dialog-title"),
  adapterDialogBody: $("adapter-dialog-body"),
  adapterDialogAction: $("adapter-dialog-action"),
  detailContent: $("detail-content"),
  form: $("memory-form"),
  formError: $("form-error"),
  submit: $("submit-memory"),
};

if (/Mac|iPhone|iPad/.test(navigator.platform)) document.body.classList.add("mac");
const desktopShell = Boolean(
  window._wails ||
  /^wails:/.test(location.protocol) ||
  location.hostname.includes("wails") ||
  /Wails/i.test(navigator.userAgent),
);
if (desktopShell) document.body.classList.add("desktop");
try {
  if (localStorage.getItem("marktoast-sidebar") === "collapsed") document.body.classList.add("side-collapsed");
} catch (error) {}

const THEME_KEY = "marktoast-theme";
const themeMedia = matchMedia("(prefers-color-scheme: dark)");

function themePreference() {
  try {
    return localStorage.getItem(THEME_KEY) || "auto";
  } catch {
    return "auto";
  }
}

function applyTheme(preference = themePreference()) {
  const dark = preference === "dark" || (preference === "auto" && themeMedia.matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
  document.documentElement.style.colorScheme = dark ? "dark" : "light";
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute("content", dark ? "#1a1a1e" : "#f4f4f6");
  document.querySelectorAll("[data-theme-choice]").forEach((button) => {
    const active = button.dataset.themeChoice === preference;
    button.classList.toggle("on", active);
    button.setAttribute("aria-pressed", String(active));
  });
}

themeMedia.addEventListener("change", () => {
  if (themePreference() === "auto") applyTheme("auto");
});

function setTheme(preference) {
  try {
    localStorage.setItem(THEME_KEY, preference);
  } catch {
    // Private mode or a locked-down shell: apply for this session only.
  }
  document.documentElement.classList.add("theming");
  applyTheme(preference);
  requestAnimationFrame(positionSetThumbs);
  window.setTimeout(() => document.documentElement.classList.remove("theming"), 380);
}

function icon(name, className = "ic") {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("class", className);
  svg.setAttribute("aria-hidden", "true");
  const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
  use.setAttribute("href", `#icon-${name}`);
  svg.appendChild(use);
  return svg;
}

function element(tag, className = "", text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined && text !== null) node.textContent = String(text);
  return node;
}

function clear(node) {
  while (node.firstChild) node.removeChild(node.firstChild);
}

function shortPath(path) {
  if (!path) return "—";
  const root = state.root.replace(/\/+$/, "");
  if (root && path === root) return ".";
  if (root && path.startsWith(`${root}/`)) return path.slice(root.length + 1);
  return String(path).replace(/^\/+/, "");
}

function fileName(path) {
  return String(path || "未命名记忆").split("/").pop().replace(/\.md$/i, "");
}

function formatError(error, fallback = "请求失败，请稍后重试。") {
  if (error instanceof TypeError && /fetch|network|load/i.test(error.message)) {
    return "无法连接本地 hub。请确认 `marktoast serve` 正在运行。";
  }
  return error?.message ? String(error.message) : fallback;
}

function formatDate(value, withTime = false) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  const options = withTime
    ? { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }
    : { year: "numeric", month: "2-digit", day: "2-digit" };
  const locale = window.MarkToastI18n?.language === "en" ? "en-US" : "zh-CN";
  return new Intl.DateTimeFormat(locale, options).format(date).replaceAll("/", "-");
}

function relativeDate(value) {
  if (!value) return "未知时间";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  const diff = Date.now() - date.getTime();
  if (diff < 60_000) return "刚刚";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`;
  if (diff < 604_800_000) return `${Math.floor(diff / 86_400_000)} 天前`;
  return formatDate(value);
}

async function api(path, options = {}) {
  const {
    headers: callerHeaders,
    signal: callerSignal,
    timeoutMs: requestedTimeout,
    ...requestOptions
  } = options;
  const timeoutMs = Number.isFinite(requestedTimeout) && requestedTimeout > 0 ? requestedTimeout : API_TIMEOUT_MS;
  const headers = new Headers(callerHeaders || {});
  if (!headers.has("Accept")) headers.set("Accept", "application/json");
  if (requestOptions.body !== undefined && requestOptions.body !== null && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const controller = new AbortController();
  let timedOut = false;
  const abortFromCaller = () => {
    if (!controller.signal.aborted) controller.abort(callerSignal?.reason);
  };
  if (callerSignal) {
    if (callerSignal.aborted) abortFromCaller();
    else callerSignal.addEventListener("abort", abortFromCaller, { once: true });
  }
  const timeoutId = window.setTimeout(() => {
    timedOut = true;
    if (!controller.signal.aborted) controller.abort();
  }, timeoutMs);

  const makeError = (message, name, status) => {
    const error = new Error(message);
    error.name = name;
    if (status) error.status = status;
    return error;
  };

  try {
    let response;
    try {
      response = await fetch(path, {
        ...requestOptions,
        headers,
        signal: controller.signal,
      });
    } catch (error) {
      if (timedOut) throw makeError("请求超时，请稍后重试。", "TimeoutError");
      if (callerSignal?.aborted || error?.name === "AbortError") {
        throw makeError("请求已取消。", "AbortError");
      }
      throw error;
    }

    if (!response.ok) {
      let detail = "";
      try {
        if (typeof response.text === "function") {
          const text = await response.text();
          if (text.trim()) {
            try {
              const payload = JSON.parse(text);
              detail = String(payload?.error || payload?.message || "");
            } catch {
              detail = text.trim();
            }
          }
        } else if (typeof response.json === "function") {
          const payload = await response.json();
          detail = String(payload?.error || payload?.message || "");
        }
      } catch {
        // The hub sometimes returns an empty or unreadable error body.
      }
      let message = detail || `HTTP ${response.status} ${response.statusText}`.trim();
      if (response.status === 401) message = "未授权，请重新连接本地 hub。";
      else if (response.status === 403) message = "没有权限执行此操作。";
      else if (response.status >= 500 && response.status <= 599) message = "本地 hub 暂时不可用，请稍后重试。";
      const error = makeError(message, "ApiError", response.status);
      error.detail = detail;
      throw error;
    }

    if (response.status === 204 || response.status === 205) return null;
    if (typeof response.text !== "function") {
      if (typeof response.json === "function") {
        try {
          return await response.json();
        } catch {
          throw makeError("本地 hub 返回了无法解析的响应。", "ParseError", response.status);
        }
      }
      return null;
    }
    let text;
    try {
      text = await response.text();
    } catch (error) {
      if (timedOut) throw makeError("请求超时，请稍后重试。", "TimeoutError");
      if (callerSignal?.aborted || error?.name === "AbortError") {
        throw makeError("请求已取消。", "AbortError");
      }
      throw error;
    }
    if (!text.trim()) return null;
    const contentType = (response.headers?.get?.("content-type") || "").toLowerCase();
    const looksLikeJson = /^[\s]*[\[{]/.test(text);
    if (contentType.includes("json") || looksLikeJson) {
      try {
        return JSON.parse(text);
      } catch {
        throw makeError("本地 hub 返回了无法解析的响应。", "ParseError", response.status);
      }
    }
    return text;
  } finally {
    window.clearTimeout(timeoutId);
    if (callerSignal) callerSignal.removeEventListener("abort", abortFromCaller);
  }
}

function setHubState(kind, text) {
  els.statusDot.classList.remove("is-online", "is-offline");
  if (kind) els.statusDot.classList.add(`is-${kind}`);
  els.hubStateText.textContent = kind === "online" ? "在线" : kind === "offline" ? "离线" : "连接中";
}

function showHubAlert(message) {
  els.hubAlertText.textContent = message;
  els.hubAlert.hidden = false;
}

function hideHubAlert() {
  els.hubAlert.hidden = true;
}

function toast(message, kind = "success") {
  if (kind !== "error") {
    showStatusPill(message, kind);
    return;
  }
  const node = element("div", "toast is-error");
  node.appendChild(icon(kind === "error" ? "warning" : "check"));
  node.appendChild(element("span", "", message));
  $("toast-region").appendChild(node);
  window.setTimeout(() => {
    node.style.opacity = "0";
    node.style.transform = "translateY(5px)";
    window.setTimeout(() => node.remove(), 180);
  }, 3400);
}

function toastDataResult(result, successText) {
  const warning = String(result?.commitWarning || "").trim();
  if (warning) {
    toast(`${successText}，但 Git 提交失败：${warning}`, "error");
    return;
  }
  toast(successText);
}

function setButtonLoading(button, loading, loadingText = "处理中…") {
  if (!button) return;
  const label = button.querySelector("span");
  button.classList.toggle("is-loading", Boolean(loading));
  if (loading) {
    button.dataset.original = label ? label.textContent : button.textContent.trim();
    button.disabled = true;
    if (label) label.textContent = loadingText;
    else button.textContent = loadingText;
    return;
  }
  button.disabled = false;
  if (button.dataset.original) {
    if (label) label.textContent = button.dataset.original;
    else button.textContent = button.dataset.original;
  }
}

function emptyState(title, message, actionLabel, onAction) {
  const empty = element("div", "empty-state");
  empty.append(element("b", "", title), element("span", "", message));
  if (actionLabel && onAction) {
    const button = element("button", "button quiet", actionLabel);
    button.type = "button";
    button.addEventListener("click", onAction);
    empty.appendChild(button);
  }
  return empty;
}

/* ---- Popover menus: Magpie's .pop --------------------------------------
   A filter used to be a native <select>; macOS draws that in a look of its
   own, so the pill opens a list this file owns instead: same hairlines,
   same hover, same keys as the rest of the page. */
let popMenu = null;

function closePopMenu({ restoreFocus = false } = {}) {
  const current = popMenu;
  if (!current) return;
  popMenu = null;
  current.anchor.setAttribute("aria-expanded", "false");
  current.pop.classList.add("leaving");
  const leaving = current.pop;
  window.setTimeout(() => leaving.remove(), 200);
  document.removeEventListener("pointerdown", current.onOutside, true);
  document.removeEventListener("scroll", current.onDismiss, true);
  window.removeEventListener("resize", current.onDismiss);
  if (restoreFocus) current.anchor.focus();
}

function openPopMenu(anchor, config) {
  if (popMenu && popMenu.anchor === anchor) {
    closePopMenu();
    return;
  }
  closePopMenu();

  const pop = element("div", "pop");
  pop.setAttribute("role", "listbox");
  pop.tabIndex = -1;
  if (config.label) pop.setAttribute("aria-label", config.label);

  let filter = null;
  if (config.searchable) {
    filter = element("input", "pop-search");
    filter.type = "search";
    filter.placeholder = config.placeholder || "搜索";
    filter.setAttribute("aria-label", config.placeholder || "搜索");
    filter.autocomplete = "off";
    pop.appendChild(filter);
  }

  const list = element("div", "pop-list");
  pop.appendChild(list);
  const rows = [];
  (config.items || []).forEach((item, index) => {
    const row = element("button", "pop-item");
    row.type = "button";
    row.id = `pop-item-${index}`;
    row.setAttribute("role", "option");
    row.setAttribute("aria-selected", String(Boolean(item.selected)));
    row.dataset.label = `${item.label} ${item.meta ?? ""}`.trim();
    const check = element("span", "pop-check");
    check.appendChild(icon("check"));
    const name = element("span", "pop-name", item.label);
    if (item.raw) name.dataset.noI18n = "true";
    row.append(check, name);
    if (item.meta !== undefined && item.meta !== null) {
      const meta = element("span", "pop-meta", item.meta);
      meta.dataset.noI18n = "true";
      row.appendChild(meta);
    }
    row.style.setProperty("--i", String(Math.min(index, 10)));
    row.addEventListener("click", () => {
      closePopMenu({ restoreFocus: true });
      config.onPick(item.value);
    });
    row.addEventListener("pointerenter", () => setActive(row));
    list.appendChild(row);
    rows.push(row);
  });
  const empty = element("div", "pop-empty", config.emptyText || "没有匹配项");
  empty.hidden = Boolean(rows.length);
  list.appendChild(empty);

  let active = rows.find((row) => row.getAttribute("aria-selected") === "true") || rows[0] || null;
  function setActive(row) {
    if (row && row.hidden) return;
    rows.forEach((item) => item.removeAttribute("data-active"));
    active = row || null;
    if (!active) {
      pop.removeAttribute("aria-activedescendant");
      return;
    }
    active.setAttribute("data-active", "true");
    pop.setAttribute("aria-activedescendant", active.id);
    active.scrollIntoView({ block: "nearest" });
  }
  function visibleRows() {
    return rows.filter((row) => !row.hidden);
  }

  pop.addEventListener("keydown", (event) => {
    const shown = visibleRows();
    const index = shown.indexOf(active);
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (!shown.length) return;
      setActive(event.key === "ArrowDown"
        ? shown[Math.min(shown.length - 1, index + 1)]
        : shown[Math.max(0, index - 1)]);
    } else if (event.key === "Home" || event.key === "End") {
      event.preventDefault();
      setActive(event.key === "Home" ? shown[0] : shown[shown.length - 1]);
    } else if (event.key === "Enter") {
      if (!active || active.hidden) return;
      event.preventDefault();
      active.click();
    } else if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      closePopMenu({ restoreFocus: true });
    }
  });

  if (filter) {
    filter.addEventListener("input", () => {
      const needle = filter.value.trim().toLowerCase();
      let hits = 0;
      rows.forEach((row) => {
        const hit = !needle || row.dataset.label.toLowerCase().includes(needle);
        row.hidden = !hit;
        if (hit) hits += 1;
      });
      empty.hidden = hits > 0;
      list.scrollTop = 0;
      setActive(visibleRows()[0] || null);
    });
  }

  const rect = anchor.getBoundingClientRect();
  document.body.appendChild(pop);
  const size = pop.getBoundingClientRect();
  const gap = 6;
  const left = Math.min(
    Math.max(10, Math.round(rect.left)),
    Math.max(10, window.innerWidth - size.width - 10),
  );
  let top = Math.round(rect.bottom + gap);
  let opensUp = false;
  if (top + size.height > window.innerHeight - 10) {
    top = Math.max(10, Math.round(rect.top - gap - size.height));
    opensUp = true;
  }
  pop.style.left = `${left}px`;
  pop.style.top = `${top}px`;
  pop.style.minWidth = `${Math.round(Math.max(rect.width, 180))}px`;
  pop.style.setProperty("--ox", `${Math.round(rect.left + rect.width / 2 - left)}px`);
  pop.style.setProperty("--oy", opensUp ? "bottom" : "top");
  pop.style.setProperty("--pop-dy", opensUp ? "6px" : "-6px");

  const onOutside = (event) => {
    if (pop.contains(event.target) || anchor.contains(event.target)) return;
    closePopMenu();
  };
  const onDismiss = (event) => {
    if (event && event.target && (event.target === pop || pop.contains(event.target))) return;
    closePopMenu();
  };
  document.addEventListener("pointerdown", onOutside, true);
  document.addEventListener("scroll", onDismiss, true);
  window.addEventListener("resize", onDismiss);
  window.setTimeout(() => {
    rows.forEach((row) => row.classList.add("settled"));
  }, 280);
  anchor.setAttribute("aria-expanded", "true");
  popMenu = { anchor, pop, onOutside, onDismiss };

  setActive(active);
  if (filter) filter.focus();
  else pop.focus();
}

/* ---- filters, as pills that open a list -------------------------------- */
function setFilterLabel(node, text, raw = false) {
  if (!node) return;
  if (raw) node.dataset.noI18n = "true";
  else delete node.dataset.noI18n;
  node.textContent = text;
}

function syncFilterLabels() {
  const projectButton = $("project-filter-button");
  const typeButton = $("type-filter-button");
  if (!projectButton || !typeButton) return;
  setFilterLabel($("project-filter-label"), state.project ? projectLabel(state.project) : "全部项目", Boolean(state.project));
  projectButton.classList.toggle("on", Boolean(state.project));
  projectButton.title = state.project || "";
  setFilterLabel($("type-filter-label"), state.type ? typeLabel(state.type) : "全部类型");
  typeButton.classList.toggle("on", Boolean(state.type));
  typeButton.title = state.type ? typeLabel(state.type) : "";
}

function projectMenuItems() {
  const items = [{ value: "", label: "全部项目", selected: !state.project }];
  const projects = state.status?.projects || [];
  projects
    .slice()
    .sort((a, b) => (Number(b.facts) || 0) - (Number(a.facts) || 0) || projectLabel(a.key).localeCompare(projectLabel(b.key), "zh-CN"))
    .forEach((project) => {
      items.push({
        value: project.key,
        label: projectLabel(project.key),
        meta: project.facts ?? 0,
        raw: true,
        selected: state.project === project.key,
      });
    });
  return items;
}

function openProjectMenu() {
  openPopMenu($("project-filter-button"), {
    label: "项目筛选",
    searchable: true,
    placeholder: "搜索项目",
    emptyText: "没有匹配的项目",
    items: projectMenuItems(),
    onPick: (value) => {
      if (value === (state.project || "")) return;
      cancelPendingSearch();
      state.project = value;
      els.projectFilter.value = value;
      renderProjects(state.status?.projects || []);
      $("all-memories").classList.toggle("is-active", !value);
      syncFilterLabels();
      loadMemories({ preserveSelection: true });
    },
  });
}

function openTypeMenu() {
  const types = Object.keys(TYPE_LABELS);
  const items = [{ value: "", label: "全部类型", selected: !state.type }];
  types.forEach((type) => {
    items.push({ value: type, label: typeLabel(type), selected: state.type === type });
  });
  openPopMenu($("type-filter-button"), {
    label: "类型筛选",
    items,
    onPick: (value) => {
      if (value === (state.type || "")) return;
      cancelPendingSearch();
      state.type = value;
      els.typeFilter.value = value;
      syncFilterLabels();
      loadMemories({ preserveSelection: true });
    },
  });
}

function sortMenuItems() {
  const items = [];
  if (state.query) items.push({ value: "relevance", label: "相关度", selected: state.sort === "relevance" });
  items.push({
    value: "updated",
    label: "最近更新",
    selected: state.sort === "updated" || (!state.query && state.sort === "relevance"),
  });
  items.push({ value: "title", label: "标题", selected: state.sort === "title" });
  return items;
}

function openSortMenu() {
  openPopMenu($("sort-button"), {
    label: "排序",
    items: sortMenuItems(),
    onPick: (value) => {
      if (value === state.sort) return;
      cancelPendingSearch();
      state.sort = value;
      updateHeading();
      loadMemories({ preserveSelection: true });
    },
  });
}

function flash(node) {
  if (!node) return;
  node.classList.remove("flash");
  void node.offsetWidth;
  node.classList.add("flash");
  window.setTimeout(() => node.classList.remove("flash"), 1300);
}

/* ---- reading size ------------------------------------------------------ */
const READ_SIZE_KEY = "marktoast-read-size";
const READ_SIZES = ["s", "m", "l"];

function stepReadSize(delta) {
  const index = Math.max(0, READ_SIZES.indexOf(readSizePreference()));
  const next = READ_SIZES[Math.min(READ_SIZES.length - 1, Math.max(0, index + delta))];
  applyReadSize(next);
}

function readSizePreference() {
  try {
    const saved = localStorage.getItem(READ_SIZE_KEY);
    return READ_SIZES.includes(saved) ? saved : "m";
  } catch (error) {
    return "m";
  }
}

function applyReadSize(size, { persist = true } = {}) {
  const next = READ_SIZES.includes(size) ? size : "m";
  document.body.dataset.read = next;
  if (persist) {
    try {
      localStorage.setItem(READ_SIZE_KEY, next);
    } catch (error) {
      // Private mode: the choice lasts the session only.
    }
  }
  document.querySelectorAll("[data-read-choice]").forEach((button) => {
    const on = button.dataset.readChoice === next;
    button.classList.toggle("on", on);
    button.setAttribute("aria-pressed", String(on));
  });
  requestAnimationFrame(positionSetThumbs);
}

/* how many cards a row of the wall holds: an arrow key moves by a row */
function wallColumns(cards) {
  if (!cards.length) return 1;
  const firstTop = cards[0].offsetTop;
  let count = 0;
  for (const card of cards) {
    if (card.offsetTop !== firstTop) break;
    count += 1;
  }
  return Math.max(1, count);
}

/* ---- Sidebar mechanics: magpie's width, bar and torch -------------------
   The sidebar is the part of a Mac app that people feel: an edge they can
   drag, a thin bar that shows only while the list scrolls, and the row under
   the pointer lit as by a torch. All three are borrowed from magpie's. */
const SIDE_WIDTH_KEY = "marktoast-side-w";
const SIDE_MIN = 168;
const SIDE_MAX = 320;
let sideWidthValue = 0;
try {
  sideWidthValue = Math.round(Number(localStorage.getItem(SIDE_WIDTH_KEY) || 0)) || 0;
} catch (error) {}

function sideFitWidth(width) {
  const room = Math.max(SIDE_MIN, Math.min(SIDE_MAX, Math.round(window.innerWidth * 0.36)));
  return Math.round(Math.max(SIDE_MIN, Math.min(Number(width) || SIDE_MIN, room)));
}

function applySideWidth() {
  if (sideWidthValue) document.body.style.setProperty("--side-w", `${sideFitWidth(sideWidthValue)}px`);
  const edge = $("side-edge");
  const sidebar = document.querySelector(".sidebar");
  if (!edge || !sidebar) return;
  edge.setAttribute("aria-valuenow", String(Math.round(sidebar.getBoundingClientRect().width)));
  edge.setAttribute("aria-valuemax", String(sideFitWidth(SIDE_MAX)));
  positionSideBars();
}

function keepSideWidth(width) {
  sideWidthValue = sideFitWidth(width);
  try {
    localStorage.setItem(SIDE_WIDTH_KEY, String(sideWidthValue));
  } catch (error) {}
  applySideWidth();
}

function toggleSidebarCollapsed() {
  const button = document.getElementById("side-toggle");
  if (button) button.click();
}

function initSidebarEdge() {
  const edge = $("side-edge");
  const sidebar = document.querySelector(".sidebar");
  if (!edge || !sidebar) return;
  edge.addEventListener("pointerdown", (event) => {
    if (event.button !== 0) return;
    event.preventDefault();
    edge.setPointerCapture(event.pointerId);
    document.body.classList.add("side-drag");
    const from = sidebar.getBoundingClientRect().width - event.clientX;
    let width = 0;
    const finish = () => {
      edge.removeEventListener("pointermove", move);
      edge.removeEventListener("pointerup", finish);
      edge.removeEventListener("pointercancel", finish);
      if (edge.hasPointerCapture(event.pointerId)) edge.releasePointerCapture(event.pointerId);
      document.body.classList.remove("side-drag");
      if (width) keepSideWidth(width);
    };
    const move = (moveEvent) => {
      const want = from + moveEvent.clientX;
      // dragged well past its narrowest, it folds away, as a Mac app's does
      if (want < SIDE_MIN - 64) {
        width = 0;
        finish();
        applySideWidth();
        toggleSidebarCollapsed();
        return;
      }
      width = sideFitWidth(want);
      document.body.style.setProperty("--side-w", `${width}px`);
      positionSideBars();
    };
    edge.addEventListener("pointermove", move);
    edge.addEventListener("pointerup", finish);
    edge.addEventListener("pointercancel", finish);
  });
  edge.addEventListener("keydown", (event) => {
    const step = event.shiftKey ? 32 : 8;
    const delta = event.key === "ArrowLeft" ? -step : event.key === "ArrowRight" ? step : 0;
    if (!delta || document.body.classList.contains("side-collapsed")) return;
    event.preventDefault();
    keepSideWidth(sidebar.getBoundingClientRect().width + delta);
  });
}

const sideBars = [];

function attachSideBar(box) {
  const sidebar = document.querySelector(".sidebar");
  if (!box || !sidebar) return;
  const bar = element("div", "side-bar");
  bar.setAttribute("aria-hidden", "true");
  bar.hidden = true;
  const thumb = element("i");
  bar.appendChild(thumb);
  sidebar.appendChild(bar);
  let fadeTimer = 0;
  const place = () => {
    const height = box.clientHeight;
    const total = box.scrollHeight;
    bar.hidden = !height || total <= height + 1;
    if (bar.hidden) return;
    bar.style.top = `${box.offsetTop}px`;
    bar.style.height = `${height}px`;
    const thumbHeight = Math.max(24, Math.round(height * height / total));
    const y = Math.round((height - thumbHeight) * box.scrollTop / (total - height));
    thumb.style.height = `${thumbHeight}px`;
    thumb.style.transform = `translateY(${y}px)`;
  };
  const rest = () => {
    window.clearTimeout(fadeTimer);
    fadeTimer = window.setTimeout(() => {
      if (!bar.matches(":hover") && !bar.classList.contains("drag")) bar.classList.remove("on");
    }, 900);
  };
  box.addEventListener("scroll", () => {
    place();
    if (bar.hidden) return;
    bar.classList.add("on");
    rest();
  }, { passive: true });
  bar.addEventListener("pointerleave", rest);
  // a press on the track pages toward it, as a Mac's bar does
  bar.addEventListener("pointerdown", (event) => {
    if (event.button !== 0) return;
    event.preventDefault();
    if (event.target !== thumb) {
      const rect = thumb.getBoundingClientRect();
      box.scrollTop += (event.clientY < rect.top ? -1 : 1) * box.clientHeight * 0.9;
      return;
    }
    bar.setPointerCapture(event.pointerId);
    bar.classList.add("drag");
    const startY = event.clientY;
    const startTop = box.scrollTop;
    const per = (box.scrollHeight - box.clientHeight) / Math.max(1, box.clientHeight - thumb.offsetHeight);
    const end = () => {
      bar.removeEventListener("pointermove", move);
      bar.removeEventListener("pointerup", end);
      bar.removeEventListener("pointercancel", end);
      if (bar.hasPointerCapture(event.pointerId)) bar.releasePointerCapture(event.pointerId);
      bar.classList.remove("drag");
      rest();
    };
    const move = (moveEvent) => {
      box.scrollTop = startTop + (moveEvent.clientY - startY) * per;
    };
    bar.addEventListener("pointermove", move);
    bar.addEventListener("pointerup", end);
    bar.addEventListener("pointercancel", end);
  });
  const observer = new ResizeObserver(place);
  observer.observe(box);
  sideBars.push({ bar, place });
}

function positionSideBars() {
  sideBars.forEach((entry) => entry.place());
}

/* the row under the pointer lights as a torch, then fades as it moves on */
function initNavLamp() {
  const sidebar = document.querySelector(".sidebar");
  if (!sidebar) return;
  document.documentElement.classList.add("navlamp");
  const lit = new Map();
  let at = null;
  let targetX = 0;
  let x = 0;
  let frame = 0;
  const calm = () => matchMedia("(prefers-reduced-motion: reduce)").matches;
  const tick = () => {
    frame = 0;
    const still = calm();
    const row = at && at.isConnected && !at.parentElement.classList.contains("is-active") ? at : null;
    if (row && !lit.has(row)) lit.set(row, 0);
    x = still ? targetX : x + (targetX - x) * 0.22;
    let busy = Math.abs(targetX - x) > 0.3;
    for (const [node, value] of lit) {
      const to = node === row ? 1 : 0;
      const now = still ? to : value + (to - value) * (to > value ? 0.22 : 0.1);
      if (!to && (now < 0.01 || !node.isConnected)) {
        lit.delete(node);
        node.classList.remove("lit");
        node.style.removeProperty("--si");
        node.style.removeProperty("--lx");
        continue;
      }
      if (Math.abs(to - now) > 0.002) busy = true;
      lit.set(node, now);
      node.classList.add("lit");
      node.style.setProperty("--si", now.toFixed(3));
      node.style.setProperty("--lx", `${(x - node.getBoundingClientRect().left).toFixed(1)}px`);
    }
    if (busy) frame = requestAnimationFrame(tick);
  };
  const go = () => {
    if (!frame) frame = requestAnimationFrame(tick);
  };
  sidebar.addEventListener("pointermove", (event) => {
    if (event.pointerType === "touch") return;
    const row = event.target && event.target.closest ? event.target.closest(".side-row") : null;
    const word = row && row.querySelector(".side-name");
    if (word && !at) x = event.clientX;
    targetX = event.clientX;
    at = word || null;
    if (at || lit.size) go();
  }, { passive: true });
  sidebar.addEventListener("pointerleave", () => {
    at = null;
    go();
  });
}

/* ---- the app's own tooltip, in place of the system's slow `title` ------ */
const TIP_KEYCAPS = {
  "search-toggle": "⌘K",
  "open-settings": "⌘,",
  "side-toggle": "⌃⌘S",
  "side-toggle-top": "⌃⌘S",
};
let tipBox = null;
let tipTimer = 0;
let tipOwner = null;
let tipShownAt = 0;

function tooltipButton(node) {
  const button = node && node.closest ? node.closest("button") : null;
  if (!button || button.disabled || !button.querySelector(".ic")) return null;
  if (button.textContent.trim()) return null;
  return button.getAttribute("aria-label") || button.getAttribute("title") ? button : null;
}

function hideTip() {
  window.clearTimeout(tipTimer);
  tipTimer = 0;
  if (tipBox) tipBox.classList.remove("on");
  if (tipOwner && tipOwner.dataset.tipTitle) {
    tipOwner.setAttribute("title", tipOwner.dataset.tipTitle);
    delete tipOwner.dataset.tipTitle;
  }
  tipOwner = null;
}

function showTip(button) {
  if (!button || !button.isConnected) return;
  if (!tipBox) {
    tipBox = element("div", "tooltip");
    tipBox.id = "app-tooltip";
    tipBox.setAttribute("role", "tooltip");
    tipBox.setAttribute("aria-hidden", "true");
    document.body.appendChild(tipBox);
  }
  const label = button.getAttribute("aria-label") || button.getAttribute("title") || "";
  if (!label) return;
  // take the native title out of the way, so two tooltips never show at once
  if (!button.dataset.tipTitle && button.getAttribute("title")) {
    button.dataset.tipTitle = button.getAttribute("title");
    button.removeAttribute("title");
  }
  tipOwner = button;
  clear(tipBox);
  tipBox.appendChild(element("span", "", label));
  const cap = TIP_KEYCAPS[button.id];
  if (cap) tipBox.appendChild(element("kbd", "", cap));
  const rect = button.getBoundingClientRect();
  tipBox.classList.add("on");
  const size = tipBox.getBoundingClientRect();
  const left = Math.round(Math.min(
    Math.max(10, rect.left + rect.width / 2 - size.width / 2),
    window.innerWidth - size.width - 10,
  ));
  let top = Math.round(rect.bottom + 7);
  if (top + size.height > window.innerHeight - 10) top = Math.round(rect.top - size.height - 7);
  tipBox.style.left = `${left}px`;
  tipBox.style.top = `${top}px`;
  tipShownAt = performance.now();
}

function initTooltip() {
  document.addEventListener("pointerover", (event) => {
    if (event.pointerType === "touch") return;
    const button = tooltipButton(event.target);
    if (!button || button === tipOwner) return;
    hideTip();
    if (performance.now() - tipShownAt < 400) showTip(button);
    else tipTimer = window.setTimeout(() => showTip(button), 450);
  });
  document.addEventListener("pointerout", (event) => {
    const button = tooltipButton(event.target);
    if (!button || (event.relatedTarget && button.contains(event.relatedTarget))) return;
    hideTip();
  });
  document.addEventListener("focusin", (event) => {
    const button = tooltipButton(event.target);
    if (button) showTip(button);
  });
  document.addEventListener("focusout", hideTip);
  document.addEventListener("pointerdown", hideTip, true);
  document.addEventListener("keydown", hideTip, true);
  document.addEventListener("scroll", hideTip, true);
  window.addEventListener("blur", hideTip);
}

/* ---- one way back up and down a long page ------------------------------ */
function initScrollEnds() {
  const box = element("div", "view-ends");
  const up = element("button", "ve-up");
  up.type = "button";
  up.title = "回到顶部";
  up.setAttribute("aria-label", "回到顶部");
  up.appendChild(icon("chevron-up"));
  const down = element("button", "ve-down");
  down.type = "button";
  down.title = "滚到底部";
  down.setAttribute("aria-label", "滚到底部");
  down.appendChild(icon("chevron-down"));
  box.append(up, element("span", "ve-sep"), down);
  document.body.appendChild(box);

  const scroller = () => {
    const detail = $("detail-content");
    if (detail && memoryDetailOpen()) return detail;
    return document.querySelector(".view");
  };
  const paint = () => {
    const node = scroller();
    const dialog = document.querySelector(".detail-dialog");
    if (memoryDetailOpen() && dialog) {
      if (box.parentElement !== dialog) dialog.appendChild(box);
      box.classList.add("in-dialog");
    } else {
      if (box.parentElement !== document.body) document.body.appendChild(box);
      box.classList.remove("in-dialog");
    }
    if (!node) {
      box.classList.remove("on");
      return;
    }
    const room = node.scrollHeight - node.clientHeight;
    const held = box.contains(document.activeElement) || box.matches(":hover");
    const on = room > node.clientHeight * 0.6 && node.scrollTop > node.clientHeight * 0.6;
    if (!on && !held) {
      box.classList.remove("on");
      return;
    }
    box.classList.add("on");
    up.setAttribute("aria-disabled", String(node.scrollTop <= 2));
    down.setAttribute("aria-disabled", String(node.scrollTop + node.clientHeight >= node.scrollHeight - 2));
  };
  const roll = (toBottom) => {
    const node = scroller();
    if (!node) return;
    node.scrollTo({
      top: toBottom ? node.scrollHeight : 0,
      behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth",
    });
  };
  up.addEventListener("click", () => roll(false));
  down.addEventListener("click", () => roll(true));
  // The capture listener covers the page; the open sheet is wired again
  // below, because its scroll does not always reach a document listener.
  document.addEventListener("scroll", paint, true);
  window.addEventListener("resize", paint);
  return paint;
}

/* ---- one outcome, centred, as magpie's status ------------------------- */
function showStatusPill(message, kind = "success") {
  let region = $("status-region");
  if (!region) {
    region = element("div", "status-region");
    region.id = "status-region";
    region.setAttribute("role", "status");
    region.setAttribute("aria-live", "polite");
    document.body.appendChild(region);
  }
  clear(region);
  const pill = element("div", "status-pill");
  pill.appendChild(icon(kind === "warning" ? "warning" : "check"));
  pill.appendChild(element("span", "", message));
  region.appendChild(pill);
  window.clearTimeout(showStatusPill.timer);
  showStatusPill.timer = window.setTimeout(() => {
    pill.style.transition = "opacity .18s var(--ease)";
    pill.style.opacity = "0";
    window.setTimeout(() => {
      if (region.contains(pill)) pill.remove();
    }, 220);
  }, 2600);
}

async function loadStatus({ announce = false } = {}) {
  setHubState("", "正在连接本地 hub");
  try {
    const data = await api("/api/status");
    state.status = data;
    state.root = data.root || "";
    hideHubAlert();
    setHubState("online", "在线");
    await loadAliases();
    $("all-count").textContent = String(data.facts ?? 0);
    $("inbox-count").textContent = Number(data.inbox) ? String(data.inbox) : "";
  requestAnimationFrame(positionSideHighlight);
    $("repo-count").textContent = `${Number(data.facts) || 0} 条`;
    $("repo-count").title = `${Number(data.facts) || 0} 条记忆`;
    $("repo-status").title = data.root || "";
    renderProjects(data.projects || []);
    renderProjectOptions(data.projects || []);
    requestAnimationFrame(positionSideBars);
    if (announce) toast("已连接到本地 hub");
    return data;
  } catch (error) {
    state.status = null;
    setHubState("offline", "离线");
    showHubAlert(formatError(error));
    $("repo-count").textContent = "";
    renderProjects([]);
    renderProjectOptions([]);
    if (state.view === "memories" && !state.results.length) renderMemoryError(error);
    if (announce) toast(formatError(error), "error");
    return null;
  }
}

const TYPE_LABELS = { user: "用户", feedback: "反馈", project: "项目", reference: "参考", decision: "决策" };

function typeLabel(type) {
  return TYPE_LABELS[type] || type || "记忆";
}

function commitLabel(subject) {
  const value = String(subject || "");
  const mem = value.match(/^mem\(([^)]+)\):\s*(.*)$/);
  if (mem) {
    const project = projectLabel(mem[1]);
    const action = mem[2]
      .replace(/^add\s+/, "新增 ")
      .replace(/^save\s+/, "保存 ")
      .replace(/^reindex$/, "重建索引");
    return `${project} · ${action}`;
  }
  const restore = value.match(/^restore\(([^)]+)\):\s*(.*)$/);
  if (restore) return `${projectLabel(restore[1])} · 恢复 ${restore[2]}`;
  return value;
}

function projectLabel(key) {
  const value = String(key || "");
  if (state.aliases && state.aliases[value]) return state.aliases[value];
  const match = value.match(/^(.*)-[0-9a-f]{16}$/);
  return match ? match[1] : value;
}

async function loadAliases() {
  try {
    const data = await api("/api/aliases");
    state.aliases = data.aliases || {};
  } catch {
    state.aliases = {};
  }
  return state.aliases;
}

function renderProjects(projects) {
  clear(els.projectList);
  const total = $("project-total");
  if (total) total.textContent = projects.length ? String(projects.length) : "";
  if (!projects.length) {
    els.projectList.appendChild(element("div", "side-label", state.status ? "暂无项目" : "等待本地 hub"));
    return;
  }
  projects
    .slice()
    .sort((a, b) => (Number(b.facts) || 0) - (Number(a.facts) || 0) || projectLabel(a.key).localeCompare(projectLabel(b.key), "zh-CN"))
    .forEach((project) => {
      const button = element("button", `side-row project-row${state.project === project.key ? " is-active" : ""}`);
      button.type = "button";
      button.dataset.project = project.key;
      button.dataset.noI18n = "true";
      button.title = project.key;
      button.setAttribute("aria-pressed", String(state.project === project.key));
      const projectName = element("span", "side-name", projectLabel(project.key));
      projectName.dataset.noI18n = "true";
      button.append(
        icon("folder"),
        projectName,
        element("span", "side-count", project.facts ?? 0),
      );
      button.addEventListener("click", () => selectProject(project.key));
      els.projectList.appendChild(button);
    });
}

function renderProjectOptions(projects) {
  const current = state.project || els.projectFilter.value;
  clear(els.projectFilter);
  const all = element("option", "", "全部项目");
  all.value = "";
  els.projectFilter.appendChild(all);
  const projectOptions = $("memory-project-options");
  if (projectOptions) {
    clear(projectOptions);
    const globalOption = document.createElement("option");
    globalOption.value = "global";
    globalOption.textContent = "global";
    projectOptions.appendChild(globalOption);
  }
  projects.forEach((project) => {
    const option = element("option", "", projectLabel(project.key));
    option.value = project.key;
    option.dataset.noI18n = "true";
    els.projectFilter.appendChild(option);
    if (projectOptions) {
      const suggestion = document.createElement("option");
      suggestion.value = project.key;
      suggestion.textContent = projectLabel(project.key);
      projectOptions.appendChild(suggestion);
    }
  });
  els.projectFilter.value = projects.some((project) => project.key === current) ? current : "";
  syncFilterLabels();
}

function selectProject(project) {
  cancelPendingSearch();
  state.project = project;
  state.query = "";
  els.search.value = "";
  els.searchClear.hidden = true;
  els.projectFilter.value = project;
  renderProjects(state.status?.projects || []);
  syncFilterLabels();
  $("all-memories").classList.toggle("is-active", !project);
  switchView("memories", { autoLoadMemories: false });
  loadMemories({ preserveSelection: true });
}

// positionSideHighlight slides one white row shape to the active sidebar row,
// the way Magpie's navigation does, instead of toggling a background per row.
function positionSideHighlight() {
  const highlight = document.getElementById("side-hl");
  const scroll = document.querySelector(".side-scroll");
  if (!highlight || !scroll) return;
  const active = scroll.querySelector(".side-row.is-active, .side-row.on");
  if (!active || !active.getClientRects().length) {
    highlight.style.opacity = "0";
    return;
  }
  const scrollRect = scroll.getBoundingClientRect();
  const rowRect = active.getBoundingClientRect();
  const first = !highlight.dataset.ready;
  if (first) highlight.classList.add("still");
  highlight.style.opacity = "1";
  highlight.style.transform = `translate(${rowRect.left - scrollRect.left + scroll.scrollLeft}px, ${rowRect.top - scrollRect.top + scroll.scrollTop}px)`;
  highlight.style.width = `${rowRect.width}px`;
  highlight.style.height = `${rowRect.height}px`;
  highlight.style.borderRadius = getComputedStyle(active).borderRadius;
  document.body.classList.add("has-side-hl");
  if (first) {
    requestAnimationFrame(() => {
      highlight.dataset.ready = "1";
      highlight.classList.remove("still");
    });
  }
}

function syncSidebarControls() {
  const collapsed = document.body.classList.contains("side-collapsed");
  const label = collapsed ? "展开侧栏" : "收起侧栏";
  const topToggle = document.getElementById("side-toggle-top");
  if (topToggle) topToggle.hidden = !collapsed;
  document.querySelectorAll("#side-toggle, #side-toggle-top").forEach((button) => {
    button.setAttribute("aria-label", label);
    button.setAttribute("title", label);
    button.setAttribute("aria-expanded", String(!collapsed));
  });
  document.querySelectorAll("#side-toggle use, #side-toggle-top use").forEach((use) => {
    use.setAttribute("href", collapsed ? "#icon-sidebar-open" : "#icon-sidebar");
  });
}

function updateBreadcrumb() {
  const crumb = document.getElementById("breadcrumb-current");
  if (!crumb) return;
  const userProject = state.view === "memories" && !state.query && Boolean(state.project);
  if (userProject) crumb.dataset.noI18n = "true";
  else delete crumb.dataset.noI18n;
  if (state.view !== "memories") crumb.textContent = VIEW_LABELS[state.view] || "全部记忆";
  else if (state.query) crumb.textContent = "搜索结果";
  else if (state.project) crumb.textContent = projectLabel(state.project);
  else crumb.textContent = "全部记忆";
}

function updateHeading() {
  const effectiveSort = !state.query && state.sort === "relevance" ? "updated" : state.sort;
  $("sort-label").textContent = effectiveSort === "updated" ? "最近更新" : effectiveSort === "title" ? "标题" : "相关度";
  syncFilterLabels();
  updateBreadcrumb();
}

function renderMemoryLoading(count = 5) {
  clear(els.resultRegion);
  const list = element("div", "list");
  for (let index = 0; index < count; index += 1) {
    const row = element("div", "row loading-row");
    row.append(
      icon("file"),
      (() => {
        const who = element("div", "who");
        who.append(element("span", "skeleton title"), element("span", "skeleton line"));
        return who;
      })(),
    );
    list.appendChild(row);
  }
  els.resultRegion.setAttribute("aria-busy", "true");
  els.resultRegion.appendChild(list);
}

function currentPageLimit() {
  return state.query ? SEARCH_PAGE_SIZE : MEMORY_PAGE_SIZE;
}

function currentApiSort() {
  if (!state.query) return state.sort === "title" ? "title" : "updated";
  return state.sort === "title" || state.sort === "updated" ? state.sort : "relevance";
}

function mergePageResults(existing, incoming) {
  const seen = new Set(existing.map((item) => item.path).filter(Boolean));
  const merged = existing.slice();
  incoming.forEach((item) => {
    const key = item.path;
    if (key && seen.has(key)) return;
    if (key) seen.add(key);
    merged.push(item);
  });
  return merged;
}

async function loadMemories({ preserveSelection = true, silent = false, append = false } = {}) {
  const requestId = ++state.requestId;
  if (state.searchController) state.searchController.abort();
  state.searchController = new AbortController();
  updateHeading();
  const limit = currentPageLimit();
  const offset = append ? state.offset : 0;
  if (append) {
    state.loadingMore = true;
    renderMemories();
  } else {
    state.loadingMore = false;
    state.offset = 0;
    state.hasMore = false;
    if (!silent) renderMemoryLoading();
  }
  els.resultRegion.setAttribute("aria-busy", "true");

  const params = new URLSearchParams();
  params.set("limit", String(limit));
  params.set("offset", String(offset));
  params.set("sort", currentApiSort());
  let endpoint;
  if (state.query) {
    params.set("q", state.query);
    if (state.project) params.set("project", state.project);
    if (state.type) params.set("type", state.type);
    endpoint = `/api/search?${params.toString()}`;
  } else {
    if (state.project) params.set("project", state.project);
    if (state.type) params.set("type", state.type);
    endpoint = `/api/memories?${params.toString()}`;
  }

  try {
    const data = await api(endpoint, { signal: state.searchController.signal });
    if (requestId !== state.requestId) return;
    const hasPaginationMeta = Boolean(
      data && typeof data === "object" &&
      ["total", "offset", "limit"].some((key) => Object.prototype.hasOwnProperty.call(data, key)),
    );
    let pageResults = Array.isArray(data?.results)
      ? data.results
      : Array.isArray(data?.memories)
        ? data.memories
        : [];
    if (!hasPaginationMeta && state.type) {
      pageResults = pageResults.filter((item) => String(item.type || "").toLowerCase() === state.type);
    }
    if (!hasPaginationMeta) pageResults = sortResults(pageResults);

    const totalNumber = Number(data?.total);
    const totalKnown = Boolean(
      data && data.total !== undefined && data.total !== null && Number.isFinite(totalNumber) && totalNumber >= 0,
    );
    const responseOffsetRaw = Number(data?.offset);
    const responseOffset = Number.isFinite(responseOffsetRaw) ? responseOffsetRaw : offset;
    const receivedCount = pageResults.length;
    const previousCount = state.results.length;
    const merged = append ? mergePageResults(state.results, pageResults) : pageResults;
    state.results = merged;
    state.totalKnown = totalKnown;
    state.total = totalKnown ? totalNumber : merged.length;
    state.limit = limit;
    state.offset = responseOffset + receivedCount;
    state.hasMore = totalKnown ? state.offset < totalNumber : receivedCount >= limit;
    state.newFrom = append ? previousCount : 0;
    if (append && (receivedCount === 0 || merged.length === previousCount)) state.hasMore = false;

    const selectedCanStay = Boolean(
      state.selectedPath && state.results.some((item) => item.path === state.selectedPath),
    );
    if (!preserveSelection || !selectedCanStay) {
      state.selectedPath = "";
      state.detailLoading = false;
      hideMemoryDetail();
    }
    state.loadingMore = false;
    renderMemories();
  } catch (error) {
    if (error.name === "AbortError" || requestId !== state.requestId) return;
    state.loadingMore = false;
    if (append) {
      toast(formatError(error), "error");
      renderMemories();
    } else {
      state.results = [];
      state.total = 0;
      state.totalKnown = false;
      state.hasMore = false;
      state.offset = 0;
      renderMemoryError(error);
    }
  } finally {
    if (requestId === state.requestId) {
      state.loadingMore = false;
      els.resultRegion.setAttribute("aria-busy", "false");
    }
  }
}

function sortResults(results) {
  const copy = results.slice();
  if (state.sort === "title") {
    copy.sort((a, b) => String(a.name || fileName(a.path)).localeCompare(String(b.name || fileName(b.path)), "zh-CN"));
  } else if (state.sort === "updated" || !state.query) {
    copy.sort((a, b) => new Date(b.updated || 0).getTime() - new Date(a.updated || 0).getTime());
  } else {
    copy.sort((a, b) => Number(b.score || 0) - Number(a.score || 0));
  }
  return copy;
}

/* The page never asks to be clicked through: it loads the next page as the
   reader comes near the bottom, and keeps filling while the wall is still
   shorter than the window. */
let autoLoadFrame = 0;

function maybeLoadMore() {
  autoLoadFrame = 0;
  if (state.view !== "memories" || !state.hasMore || state.loadingMore) return;
  if (!state.results.length) return;
  const viewport = document.querySelector(".view");
  if (!viewport) return;
  const rest = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight;
  if (rest < viewport.clientHeight * 1.2) {
    loadMemories({ preserveSelection: true, silent: true, append: true });
  }
}

function queueLoadMoreCheck() {
  if (autoLoadFrame) return;
  autoLoadFrame = requestAnimationFrame(maybeLoadMore);
}

function appendLoadMoreHint() {
  if (!state.hasMore && !state.loadingMore) return;
  const hint = element("div", "load-more-hint", state.loadingMore ? "正在加载…" : "");
  hint.setAttribute("aria-hidden", "true");
  els.resultRegion.appendChild(hint);
  queueLoadMoreCheck();
}

function renderMemories() {
  const newFrom = state.newFrom || 0;
  state.newFrom = 0;
  clear(els.resultRegion);
  els.resultTotal.textContent = state.totalKnown ? String(state.total) : String(state.results.length);
  if (!state.selectedPath) hideMemoryDetail();
  if (!state.results.length) {
    const list = element("div", "list");
    list.appendChild(emptyState(
      state.query ? "没有匹配" : "还没有记忆",
      state.query ? "换个关键词试试。" : "先写一条。",
      state.query ? "清除筛选" : "新建",
      state.query ? clearFilters : openDialog,
    ));
    els.resultRegion.appendChild(list);
    return;
  }

  const grid = element("div", "memory-grid");
  state.results.forEach((result, index) => {
    const open = result.path === state.selectedPath;
    const type = result.type || "memory";
    const card = element("button", `memory-card type-${type}${open ? " is-open" : ""}`);
    card.type = "button";
    card.dataset.path = result.path;
    card.setAttribute("aria-expanded", String(open));
    card.setAttribute("aria-label", `${open ? "收起" : "打开"}记忆：${result.name || fileName(result.path)}`);

    const head = element("div", "card-head");
    const typePill = element("span", `pill type-pill type-${type}`, typeLabel(type));
    typePill.title = type;
    head.append(typePill, element("span", "card-time", relativeDate(result.updated)));
    card.appendChild(head);

    const title = element("div", "card-title", result.name || fileName(result.path));
    title.dataset.noI18n = "true";
    card.appendChild(title);

    const snippet = element("div", "card-snippet");
    snippet.dataset.noI18n = "true";
    renderCardPreview(snippet, result);
    card.appendChild(snippet);

    const foot = element("div", "card-foot");
    const project = element("span", "card-project", result.project ? projectLabel(result.project) : "未归类");
    if (result.project) {
      project.dataset.noI18n = "true";
      project.title = result.project;
    }
    const openIcon = element("span", "card-open");
    openIcon.appendChild(icon("chevron-right"));
    foot.append(project, openIcon);
    card.appendChild(foot);

    if (newFrom && index >= newFrom) {
      card.classList.add("is-new");
      card.style.setProperty("--i", String(Math.min(index - newFrom, 12)));
    }
    card.addEventListener("click", () => selectMemory(result.path));
    grid.appendChild(card);
  });
  els.resultRegion.appendChild(grid);
  appendLoadMoreHint();
  if (memoryDetailOpen()) renderMemoryDetail();
}

const CARD_PREVIEW_CHARS = 360;

function renderCardPreview(container, result) {
  const source = String(result.preview || result.description || cleanSnippet(result.snippet) || shortPath(result.path) || "");
  const text = source
    .split("\n")
    .map(cardPreviewLine)
    .filter((line) => line.trim())
    .join(" ")
    .replace(/\s{2,}/g, " ")
    .trim();
  renderInline(container, text.slice(0, CARD_PREVIEW_CHARS));
}

function cardPreviewLine(line) {
  let text = String(line || "").replace(/^\s{0,3}#{1,6}\s+/, "").trim();
  if (!text) return "";
  // a card shows the body, not its markup: fences and rules are structure
  if (/^(`{3,}|~{3,})/.test(text) || /^(-{3,}|\*{3,}|_{3,})$/.test(text)) return "";
  if (/^\s*[|:\-\s]+$/.test(text) && text.includes("-")) return "";
  if (text.includes("|")) {
    const cells = text
      .replace(/^\s*\|/, "")
      .replace(/\|\s*$/, "")
      .split("|")
      .map((cell) => cell.trim())
      .filter(Boolean);
    if (cells.length > 1) return cells.join(" · ");
  }
  text = text.replace(/^\s{0,3}>\s?/, "").replace(/^\s*(?:[-*+])\s+/, "· ").replace(/^\s*(\d+)[.)]\s+/, "$1. ");
  return text;
}

function isAnyModalOpen() {
  return [els.modal, els.detailModal, els.adapterModal].some((modal) => Boolean(modal && !modal.hidden));
}

function syncPageInert() {
  const open = isAnyModalOpen();
  [document.querySelector("header.top"), document.querySelector("aside.sidebar"), document.querySelector("main.view")]
    .filter(Boolean)
    .forEach((node) => {
      node.inert = open;
      if (open) node.setAttribute("inert", "");
      else node.removeAttribute("inert");
    });
  document.body.classList.toggle("modal-open", open);
}

function showModal(modal) {
  if (!modal) return;
  modal.hidden = false;
  syncPageInert();
}

function hideModal(modal) {
  if (!modal) return;
  modal.hidden = true;
  syncPageInert();
}

function memoryDetailOpen() {
  return Boolean(els.detailModal && !els.detailModal.hidden);
}

function hideMemoryDetail() {
  if (els.detailModal && !els.detailModal.hidden) hideModal(els.detailModal);
  state.detailLoading = false;
  requestAnimationFrame(() => window.paintScrollEnds?.());
}

function renderMemoryDetail() {
  if (!els.detailModal || els.detailModal.hidden) return;
  const path = state.selectedPath;
  const result = state.results.find((item) => item.path === path) || state.details.get(path);
  if (!result) {
    hideMemoryDetail();
    return;
  }
  clear(els.detailContent);
  els.detailContent.appendChild(renderExpandedMemory(result));
  if (els.detailContent.dataset.endsWired !== "1") {
    els.detailContent.dataset.endsWired = "1";
    els.detailContent.addEventListener("scroll", () => {
      requestAnimationFrame(() => window.paintScrollEnds?.());
    }, { passive: true });
  }
  if (state.detailScrollPath !== path) {
    state.detailScrollPath = path;
    els.detailContent.scrollTop = 0;
    requestAnimationFrame(() => window.paintScrollEnds?.());
  }
}

function openMemoryDetail() {
  if (!els.detailModal) return;
  showModal(els.detailModal);
  renderMemoryDetail();
  requestAnimationFrame(() => {
    $("close-detail")?.focus({ preventScroll: true });
  });
}

function closeMemoryDetail() {
  const path = state.selectedPath;
  hideMemoryDetail();
  state.selectedPath = "";
  renderMemories();
  if (path) {
    requestAnimationFrame(() => {
      const card = [...document.querySelectorAll(".memory-card")].find((node) => node.dataset.path === path);
      card?.focus({ preventScroll: true });
    });
  }
}

function cleanSnippet(snippet) {
  return String(snippet || "").replace(/\s+/g, " ").trim();
}

function renderExpandedMemory(result) {
  const box = element("div", "memory-document");
  if (state.detailLoading) {
    box.classList.add("loading");
    const loading = element("div", "detail-loading");
    loading.append(
      element("span", "skeleton"),
      element("span", "skeleton"),
      element("span", "skeleton"),
    );
    box.appendChild(loading);
    return box;
  }

  if (state.detailErrors.has(result.path)) {
    const error = element("div", "expanded-error");
    error.append(
      icon("warning"),
      element("span", "", formatError(state.detailErrors.get(result.path))),
      (() => {
        const retry = element("button", "text-button", "重试");
        retry.type = "button";
        retry.addEventListener("click", () => selectMemory(result.path, true));
        return retry;
      })(),
    );
    box.appendChild(error);
    return box;
  }

  const detail = state.details.get(result.path);
  if (!detail) {
    box.classList.add("loading");
    box.appendChild(element("div", "detail-loading"));
    return box;
  }

  const path = detail.path || result.path;
  const type = detail.type || result.type || "memory";
  const header = element("header", "doc-head");

  const meta = element("div", "doc-meta");
  const typePill = element("span", `pill type-pill type-${type}`, typeLabel(type));
  typePill.title = type;
  meta.appendChild(typePill);
  if (detail.project || result.project) {
    const project = element("span", "doc-project", projectLabel(detail.project || result.project));
    project.title = detail.project || result.project;
    meta.appendChild(project);
  }
  meta.appendChild(element("span", "doc-time", relativeDate(detail.updated || result.updated)));

  const actions = element("div", "doc-actions");
  const history = element("button", "icon", "");
  history.type = "button";
  history.title = "版本历史";
  history.setAttribute("aria-label", "查看这条记忆的版本历史");
  history.appendChild(icon("branch"));
  history.addEventListener("click", () => toggleMemoryHistory(path));
  const copyMd = element("button", "icon", "");
  copyMd.type = "button";
  copyMd.title = "复制 Markdown";
  copyMd.setAttribute("aria-label", "复制这条记忆的 Markdown");
  copyMd.appendChild(icon("file"));
  copyMd.addEventListener("click", () => copyMarkdown(path, copyMd));
  const copy = element("button", "icon", "");
  copy.type = "button";
  copy.title = path;
  copy.setAttribute("aria-label", "复制记忆路径");
  copy.appendChild(icon("copy"));
  copy.addEventListener("click", () => copyPath(path, copy));
  actions.append(history, copyMd, copy);
  meta.appendChild(actions);
  header.appendChild(meta);

  const title = element("h1", "doc-title", detail.name || result.name || fileName(path));
  title.id = "detail-title";
  title.dataset.noI18n = "true";
  title.title = path;
  header.appendChild(title);

  const description = String(detail.description || result.description || "").trim();
  if (description) {
    const lead = element("p", "doc-description", description);
    lead.dataset.noI18n = "true";
    header.appendChild(lead);
  }
  header.appendChild(element("div", "doc-rule"));
  box.appendChild(header);

  const fullBody = String(detail.body || "");
  const body = element("div", "markdown-body doc-body");
  renderMarkdown(body, fullBody.slice(0, MAX_RENDERED_BODY_CHARS));
  if (!fullBody.trim()) {
    body.appendChild(element("p", "doc-empty", "没有正文。"));
  } else if (fullBody.length > MAX_RENDERED_BODY_CHARS) {
    body.appendChild(element("p", "body-truncation-note", `正文较长，仅显示前 ${MAX_RENDERED_BODY_CHARS.toLocaleString("en-US")} 个字符。`));
  }
  box.appendChild(body);

  if (state.memoryHistory.open && state.memoryHistory.path === path) {
    box.appendChild(renderMemoryHistory(path));
  }
  return box;
}

async function copyMarkdown(path, button) {
  const detail = state.details.get(path);
  if (!detail || !detail.raw) {
    toast("这条记忆还没加载完", "error");
    return;
  }
  try {
    await navigator.clipboard.writeText(String(detail.raw));
    toast("Markdown 已复制到剪贴板");
    flashCopied(button);
  } catch {
    toast("复制失败，请稍后再试", "error");
  }
}

function cacheMemoryDetail(path, detail) {
  state.details.delete(path);
  state.details.set(path, detail);
  while (state.details.size > MAX_DETAIL_CACHE_ENTRIES) {
    const oldest = state.details.keys().next().value;
    if (oldest === undefined) break;
    state.details.delete(oldest);
  }
}

async function selectMemory(path, force = false) {
  if (!path) return;
  if (path === state.selectedPath && !force && memoryDetailOpen()) {
    closeMemoryDetail();
    return;
  }
  state.selectedPath = path;
  state.detailErrors.delete(path);
  state.detailLoading = !state.details.has(path);
  renderMemories();
  openMemoryDetail();
  if (state.details.has(path) && !force) return;

  try {
    const detail = await api(`/api/memory?path=${encodeURIComponent(path)}`);
    cacheMemoryDetail(path, detail);
    state.detailErrors.delete(path);
    if (state.selectedPath === path && state.view === "memories") {
      state.detailLoading = false;
      renderMemories();
      renderMemoryDetail();
    }
  } catch (error) {
    if (state.selectedPath === path && state.view === "memories") {
      state.detailLoading = false;
      state.detailErrors.set(path, error);
      renderMemories();
      renderMemoryDetail();
    }
  }
}

// ---- Markdown -----------------------------------------------------------
// The UI ships without dependencies, so this is a small GFM-ish renderer
// built from DOM nodes only: memory bodies can never inject markup.
// Blocks: headings, fenced code, tables, blockquotes, ordered/unordered
// lists (nested, task items), horizontal rules, paragraphs.
// Inline: bold, italic, strikethrough, code spans, links, autolinks, images.

const MD_FENCE_RE = /^\s{0,3}(`{3,}|~{3,})\s*([^`\s]*)\s*$/;
const MD_HEADING_RE = /^\s{0,3}(#{1,6})\s+(.+?)\s*#*\s*$/;
const MD_RULE_RE = /^\s{0,3}(?:(\*\s*){3,}|(-\s*){3,}|(_\s*){3,})$/;
const MD_QUOTE_RE = /^\s{0,3}>/;
const MD_INLINE_SOURCE = "(`[^`\\n]+`|\\*\\*[^*\\n]+\\*\\*|~~[^~\\n]+~~|!\\[[^\\]\\n]*\\]\\([^)\\n]+\\)|\\[[^\\]\\n]+\\]\\([^)\\n]+\\)|\\*[^*\\s][^*\\n]*\\*|<(?:https?:\\/\\/|mailto:)[^<>\\s]+>)";

function renderMarkdown(container, markdown) {
  renderMarkdownBlocks(container, normalizeMarkdown(markdown));
}

function normalizeMarkdown(value) {
  const lines = String(value || "").replace(/\r\n?/g, "\n").replace(/\t/g, "    ").split("\n");
  while (lines.length && !lines[lines.length - 1].trim()) lines.pop();
  return lines;
}

function renderMarkdownBlocks(parent, lines) {
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (!line.trim()) {
      i += 1;
      continue;
    }

    const fence = line.match(MD_FENCE_RE);
    if (fence) {
      const ch = fence[1][0];
      const lang = fence[2] || "";
      const closeRe = new RegExp(`^\\s{0,3}${ch}{3,}\\s*$`);
      const code = [];
      i += 1;
      while (i < lines.length && !closeRe.test(lines[i])) {
        code.push(lines[i]);
        i += 1;
      }
      if (i < lines.length) i += 1;
      parent.appendChild(markdownCodeBlock(code.join("\n"), lang));
      continue;
    }

    const heading = line.match(MD_HEADING_RE);
    if (heading) {
      const node = document.createElement(`h${heading[1].length}`);
      renderInline(node, heading[2]);
      parent.appendChild(node);
      i += 1;
      continue;
    }

    if (MD_RULE_RE.test(line)) {
      parent.appendChild(element("hr", "md-rule"));
      i += 1;
      continue;
    }

    if (line.includes("|") && isTableSeparator(lines[i + 1])) {
      i = renderMarkdownTable(parent, lines, i);
      continue;
    }

    if (MD_QUOTE_RE.test(line)) {
      const quoted = [];
      while (i < lines.length) {
        const current = lines[i];
        if (!current.trim()) {
          if (i + 1 < lines.length && MD_QUOTE_RE.test(lines[i + 1])) {
            quoted.push("");
            i += 1;
            continue;
          }
          break;
        }
        if (!MD_QUOTE_RE.test(current)) break;
        quoted.push(current.replace(/^\s{0,3}>\s?/, ""));
        i += 1;
      }
      const quote = element("blockquote", "");
      renderMarkdownBlocks(quote, quoted);
      parent.appendChild(quote);
      continue;
    }

    if (listMarker(line)) {
      i = renderMarkdownList(parent, lines, i);
      continue;
    }

    const paragraph = [line.trim()];
    i += 1;
    while (i < lines.length) {
      const current = lines[i];
      if (!current.trim()) break;
      if (startsMarkdownBlock(current)) break;
      if (current.includes("|") && isTableSeparator(lines[i + 1])) break;
      paragraph.push(current.trim());
      i += 1;
    }
    const node = document.createElement("p");
    appendParagraphContent(node, paragraph);
    parent.appendChild(node);
  }
}

function startsMarkdownBlock(line) {
  const text = line.replace(/^\s{0,3}/, "");
  return (
    /^(`{3,}|~{3,})/.test(text) ||
    /^#{1,6}\s/.test(text) ||
    /^>/.test(text) ||
    listMarker(text) !== null ||
    MD_RULE_RE.test(text)
  );
}

function listMarker(line) {
  const match = String(line || "").match(/^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$/);
  if (!match) return null;
  if (/(^|\s)([*_-])(\s*[*_-]){1,}/.test(match[3]) && !match[3].trim()) return null;
  return {
    indent: match[1].replace(/\t/g, "    ").length,
    ordered: /\d/.test(match[2]),
    number: parseInt(match[2], 10),
    content: match[3],
  };
}

function indentOf(line) {
  return String(line || "").match(/^\s*/)[0].replace(/\t/g, "    ").length;
}

function renderMarkdownList(parent, lines, start) {
  const first = listMarker(lines[start]);
  const list = document.createElement(first.ordered ? "ol" : "ul");
  if (first.ordered && first.number !== 1) list.setAttribute("start", String(first.number));
  const ordered = first.ordered;
  const baseIndent = first.indent;

  let i = start;
  while (i < lines.length) {
    const marker = listMarker(lines[i]);
    if (!marker || marker.indent !== baseIndent || marker.ordered !== ordered) break;

    const item = document.createElement("li");
    const task = marker.content.match(/^\[([ xX])\]\s+(.*)$/);
    if (task) {
      const box = document.createElement("input");
      box.type = "checkbox";
      box.disabled = true;
      box.checked = task[1] !== " ";
      box.className = "md-checkbox";
      item.appendChild(box);
      renderInline(item, task[2]);
    } else {
      renderInline(item, marker.content);
    }

    i += 1;
    const continuation = [];
    while (i < lines.length) {
      const current = lines[i];
      if (!current.trim()) {
        let next = i + 1;
        while (next < lines.length && !lines[next].trim()) next += 1;
        if (next < lines.length) {
          const nextMarker = listMarker(lines[next]);
          const nextIndent = indentOf(lines[next]);
          if (nextMarker && nextMarker.indent === baseIndent) break;
          if (nextIndent > baseIndent) {
            continuation.push("");
            i += 1;
            continue;
          }
        }
        break;
      }
      const currentMarker = listMarker(current);
      if (currentMarker && currentMarker.indent <= baseIndent) break;
      if (indentOf(current) > baseIndent) {
        continuation.push(current);
        i += 1;
        continue;
      }
      if (startsMarkdownBlock(current)) break;
      continuation.push(current);
      i += 1;
    }

    if (continuation.length) {
      const dedented = dedentLines(continuation);
      renderMarkdownBlocks(item, dedented);
    }
    list.appendChild(item);
  }

  parent.appendChild(list);
  return i;
}

function dedentLines(lines) {
  const widths = lines.filter((line) => line.trim()).map((line) => indentOf(line));
  const strip = widths.length ? Math.min(...widths) : 0;
  if (!strip) return lines.slice();
  return lines.map((line) => {
    if (!line.trim()) return "";
    let removed = 0;
    let out = "";
    for (const ch of line) {
      if (removed < strip && (ch === " " || ch === "\t")) {
        removed += ch === "\t" ? 4 : 1;
        continue;
      }
      out += ch;
    }
    return out;
  });
}

function appendParagraphContent(parent, lines) {
  lines.forEach((line, index) => {
    const hardBreak = /\s{2,}$/.test(line) || line.endsWith("\\");
    renderInline(parent, line.replace(/\\$/, "").replace(/\s+$/, ""));
    if (hardBreak) parent.appendChild(document.createElement("br"));
    else if (index < lines.length - 1) {
      const join = softBreakJoiner(lines[index], lines[index + 1]);
      if (join) parent.appendChild(document.createTextNode(join));
    }
  });
}

// Chinese prose is usually hard-wrapped without spaces, so joining two CJK
// lines with the CommonMark space would leave a visible gap in every fact.
function softBreakJoiner(previous, next) {
  const last = String(previous || "").trim().slice(-1);
  const first = String(next || "").trim().charAt(0);
  if (isCJK(last) && isCJK(first)) return "";
  return " ";
}

function isCJK(ch) {
  return /[\u3000-\u303f\u3400-\u4dbf\u4e00-\u9fff\uff00-\uffef]/.test(ch);
}

function isTableSeparator(line) {
  if (!line || !line.includes("-")) return false;
  const cells = splitTableRow(line);
  if (!cells.length) return false;
  return cells.every((cell) => /^:?-{1,}:?$/.test(cell.replace(/\s+/g, "")));
}

function splitTableRow(line) {
  let text = String(line || "").trim();
  if (text.startsWith("|")) text = text.slice(1);
  if (text.endsWith("|")) text = text.slice(0, -1);
  return text.split("|").map((cell) => cell.trim());
}

function tableAlignment(cell) {
  const text = cell.replace(/\s+/g, "");
  const left = text.startsWith(":");
  const right = text.endsWith(":");
  if (left && right) return "center";
  if (right) return "right";
  if (left) return "left";
  return "";
}

function renderMarkdownTable(parent, lines, start) {
  const header = splitTableRow(lines[start]);
  const alignments = splitTableRow(lines[start + 1]).map(tableAlignment);
  const table = element("table", "md-table");
  const head = document.createElement("thead");
  const headRow = document.createElement("tr");
  header.forEach((cell, index) => {
    const th = document.createElement("th");
    if (alignments[index]) th.style.textAlign = alignments[index];
    renderInline(th, cell);
    headRow.appendChild(th);
  });
  head.appendChild(headRow);
  table.appendChild(head);

  const body = document.createElement("tbody");
  let i = start + 2;
  while (i < lines.length && lines[i].trim() && lines[i].includes("|")) {
    const cells = splitTableRow(lines[i]);
    if (!cells.length) break;
    const row = document.createElement("tr");
    for (let column = 0; column < header.length; column += 1) {
      const td = document.createElement("td");
      if (alignments[column]) td.style.textAlign = alignments[column];
      renderInline(td, cells[column] || "");
      row.appendChild(td);
    }
    body.appendChild(row);
    i += 1;
  }
  table.appendChild(body);
  parent.appendChild(table);
  return i;
}

function markdownCodeBlock(code, lang) {
  const wrap = element("div", "md-code");
  if (lang) wrap.appendChild(element("span", "md-code-lang", lang));
  const pre = document.createElement("pre");
  const node = document.createElement("code");
  if (lang) node.className = `language-${lang}`;
  node.textContent = code;
  pre.appendChild(node);
  wrap.appendChild(pre);
  return wrap;
}

function renderInline(parent, source) {
  const text = String(source === undefined || source === null ? "" : source);
  const pattern = new RegExp(MD_INLINE_SOURCE, "g");
  let cursor = 0;
  let match;
  while ((match = pattern.exec(text)) !== null) {
    if (match.index > cursor) parent.appendChild(document.createTextNode(text.slice(cursor, match.index)));
    renderInlineToken(parent, match[0]);
    cursor = pattern.lastIndex;
  }
  if (cursor < text.length) parent.appendChild(document.createTextNode(text.slice(cursor)));
}

function renderInlineToken(parent, token) {
  if (token.startsWith("`")) {
    parent.appendChild(element("code", "", token.slice(1, -1)));
    return;
  }
  if (token.startsWith("![")) {
    const image = token.match(/^!\[([^\]]*)\]\(([^)]+)\)$/);
    parent.appendChild(document.createTextNode(image && image[1] ? image[1] : "图片"));
    return;
  }
  if (token.startsWith("[")) {
    const link = token.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
    if (!link) {
      parent.appendChild(document.createTextNode(token));
      return;
    }
    const href = safeUrl(link[2]);
    if (!href) {
      renderInline(parent, link[1]);
      return;
    }
    const anchor = document.createElement("a");
    anchor.href = href;
    anchor.target = "_blank";
    anchor.rel = "noreferrer noopener";
    renderInline(anchor, link[1]);
    parent.appendChild(anchor);
    return;
  }
  if (token.startsWith("**")) {
    const strong = element("strong", "");
    renderInline(strong, token.slice(2, -2));
    parent.appendChild(strong);
    return;
  }
  if (token.startsWith("~~")) {
    const strike = element("del", "");
    renderInline(strike, token.slice(2, -2));
    parent.appendChild(strike);
    return;
  }
  if (token[0] === "*") {
    const emphasis = element("em", "");
    renderInline(emphasis, token.slice(1, -1));
    parent.appendChild(emphasis);
    return;
  }
  if (token.startsWith("<")) {
    const url = token.slice(1, -1);
    const href = safeUrl(url);
    if (!href) {
      parent.appendChild(document.createTextNode(token));
      return;
    }
    const anchor = document.createElement("a");
    anchor.href = href;
    anchor.target = "_blank";
    anchor.rel = "noreferrer noopener";
    anchor.textContent = url;
    parent.appendChild(anchor);
    return;
  }
  parent.appendChild(document.createTextNode(token));
}

function safeUrl(raw) {
  try {
    const url = new URL(raw, window.location.origin);
    if (url.protocol === "http:" || url.protocol === "https:") return url.href;
  } catch {
    return "";
  }
  return "";
}

async function copyPath(path, button) {
  if (!path) return;
  try {
    await navigator.clipboard.writeText(path);
    toast("路径已复制到剪贴板");
    flashCopied(button);
  } catch {
    toast("复制失败，请手动选择路径", "error");
  }
}

function renderMemoryError(error) {
  clear(els.resultRegion);
  els.resultTotal.textContent = "不可用";
  const list = element("div", "list");
  list.appendChild(emptyState(
    state.status ? "记忆加载失败" : "本地 hub 未连接",
    formatError(error),
    "重新连接",
    retryAll,
  ));
  els.resultRegion.appendChild(list);
}

// ---- Adapters -----------------------------------------------------------
// The hub reports one row per built-in descriptor (~40 harnesses). Older hub
// builds have no `mode` field, so derive the install mode from hooks.install_kind
// the same way the Go side does, and keep the three groups stable.
const ADAPTER_MODES = ["auto", "staged", "mcp_only"];
const ADAPTER_MODE_LABELS = { auto: "自动", staged: "片段", mcp_only: "仅 MCP" };
const ADAPTER_MODE_GROUP_LABELS = { auto: "自动接入", staged: "片段安装", mcp_only: "仅 MCP" };
const ADAPTER_MODE_HINTS = {
  auto: "MarkToast 直接合并这家 harness 的配置；写前备份，保留第三方条目。",
  staged: "MarkToast 生成可直接粘贴的 hooks / MCP 片段，由你手动合并。",
  mcp_only: "这家只支持 MCP：生成 MCP 片段，不安装生命周期 hooks。",
};
const ADAPTER_TIER_LABELS = { native: "原生", projection: "投影", readonly: "只读" };
const ADAPTER_INJECTION_LABELS = {
  stdout_additional_context: "stdout additionalContext",
  pre_llm_call_context: "pre_llm_call context",
  mcp_handoff: "MCP handoff",
  none: "none",
};

function adapterMode(adapter) {
  const explicit = String(adapter?.mode || adapter?.install_mode || "").trim();
  if (ADAPTER_MODES.includes(explicit)) return explicit;
  const kind = String(adapter?.hooks?.install_kind || "").trim();
  if (kind === "json_merge" || kind === "toml_merge" || kind === "json_bundle") return "auto";
  if (kind === "mcp_only" || kind === "none") return "mcp_only";
  return "staged";
}

function adapterHasExplicitMode(adapter) {
  return ADAPTER_MODES.includes(String(adapter?.mode || "").trim());
}

function adapterStagedReady(adapter) {
  return adapter?.staged_ready === true;
}

function adapterState(adapter) {
  const mode = adapterMode(adapter);
  if (mode === "auto" && adapter?.installed) {
    return { key: "installed", label: "已接入", className: " is-on" };
  }
  if (mode !== "auto" && adapterStagedReady(adapter)) {
    return { key: "staged", label: "已生成片段", className: " is-on" };
  }
  if (adapter?.detected) return { key: "detected", label: "已检测", className: "" };
  return { key: "missing", label: "未检测", className: "" };
}

function adapterCanInstall(adapter) {
  if (adapterMode(adapter) !== "auto" || !adapter?.detected) return false;
  // New hub builds make every auto-mode adapter installable; legacy builds only
  // exposed `installable` for ZCode.
  if (adapterHasExplicitMode(adapter)) return true;
  return Boolean(adapter?.installable);
}

function adapterCanStage(adapter) {
  return adapterMode(adapter) !== "auto" && Boolean(adapter?.detected);
}

async function runAdapterAction(adapter, button) {
  const title = adapter.title || adapter.id || "Adapter";
  const verb = adapter.installed ? "重新写入" : "写入";
  if (!window.confirm(localized(`${verb}这家 harness 的配置？会先备份原文件。`))) return;
  setButtonLoading(button, true);
  try {
    const data = await api("/api/adapters/action", {
      method: "POST",
      body: JSON.stringify({ id: adapter.id, action: "install" }),
      timeoutMs: 60_000,
    });
    toast(`${title} · ${localized(data?.installed ? "已接入" : "配置已更新")}`);
    await loadAdapters();
    await loadStatus();
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    setButtonLoading(button, false);
  }
}

async function stageAdapter(adapter, button) {
  const title = adapter.title || adapter.id || "Adapter";
  setButtonLoading(button, true);
  try {
    const data = await api("/api/adapters/action", {
      method: "POST",
      body: JSON.stringify({ id: adapter.id, action: "stage" }),
      timeoutMs: 60_000,
    });
    const dir = String(data?.dir || "").trim();
    if (dir) {
      try {
        await navigator.clipboard.writeText(dir);
        toast(`${title} · ${localized("片段已生成，路径已复制")}`);
      } catch {
        toast(`${title} · ${localized("片段已生成")}：${dir}`);
      }
    } else {
      toast(`${title} · ${localized("片段已生成")}`);
    }
    await loadAdapters();
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    setButtonLoading(button, false);
  }
}

function adapterActionButton(adapter, extraClass = "") {
  if (adapterCanInstall(adapter)) {
    const install = element(
      "button",
      `button quiet adapter-action${extraClass}`,
      adapter.installed ? "修复" : "接入",
    );
    install.type = "button";
    install.addEventListener("click", (event) => runAdapterAction(adapter, event.currentTarget));
    return install;
  }
  if (adapterCanStage(adapter)) {
    const stage = element("button", `button quiet adapter-action${extraClass}`, "生成片段");
    stage.type = "button";
    stage.title = "生成合并片段";
    stage.addEventListener("click", (event) => stageAdapter(adapter, event.currentTarget));
    return stage;
  }
  return null;
}

function adapterInfoRows(adapter) {
  const mode = adapterMode(adapter);
  const hooks = adapter.hooks || {};
  const mcp = adapter.mcp || {};
  const rows = [
    ["接入方式", `${localized(ADAPTER_MODE_LABELS[mode])} · ${localized(ADAPTER_MODE_GROUP_LABELS[mode])}`],
    ["层级", localized(ADAPTER_TIER_LABELS[adapter.tier] || adapter.tier || "—")],
  ];
  const detectedPaths = adapter.detected_path || (Array.isArray(adapter.detect) ? adapter.detect.join(", ") : "");
  if (detectedPaths) rows.push(["检测路径", detectedPaths]);
  if (Array.isArray(hooks.events) && hooks.events.length) rows.push(["Hook 事件", hooks.events.join(" · ")]);
  if (hooks.target) rows.push(["Hook 目标文件", hooks.target]);
  if (hooks.injection) rows.push(["注入方式", localized(ADAPTER_INJECTION_LABELS[hooks.injection] || hooks.injection)]);
  if (adapter.install_kind || hooks.install_kind) {
    rows.push(["安装类型", adapter.install_kind || hooks.install_kind]);
  }
  if (mcp.kind) rows.push(["MCP 方式", mcp.kind]);
  if (mcp.target) rows.push(["MCP 目标文件", mcp.target]);
  return rows;
}

function renderAdapterInfo(adapter) {
  if (!els.adapterDialogBody) return;
  const body = els.adapterDialogBody;
  clear(body);
  const mode = adapterMode(adapter);
  els.adapterDialogTitle.textContent = adapter.title || adapter.id || "Adapter";

  const intro = element("div", "adapter-info-intro");
  intro.append(
    element("span", `pill adapter-mode is-${mode.replace("_", "-")}`, ADAPTER_MODE_LABELS[mode]),
    element("p", "adapter-info-hint", ADAPTER_MODE_HINTS[mode]),
  );
  body.appendChild(intro);

  const table = element("table", "frontmatter-table adapter-info-table");
  adapterInfoRows(adapter).forEach(([key, value]) => {
    const trNode = element("tr");
    trNode.append(element("th", "", key), element("td", "", value));
    table.appendChild(trNode);
  });
  body.appendChild(table);

  if (adapter.notes) {
    const notes = element("section", "adapter-info-notes");
    notes.appendChild(element("h3", "", "说明"));
    const text = element("p", "", adapter.notes);
    text.dataset.noI18n = "true";
    notes.appendChild(text);
    body.appendChild(notes);
  }

  const stagedDir = String(adapter.staged_dir || "").trim();
  if (stagedDir) {
    const dir = element("section", "adapter-info-notes");
    dir.appendChild(element("h3", "", "片段目录"));
    const code = element("code", "", stagedDir);
    code.dataset.noI18n = "true";
    dir.appendChild(code);
    body.appendChild(dir);
  }
}

function adapterDialogActionDef(adapter) {
  if (adapterCanInstall(adapter)) return { kind: "install", label: adapter.installed ? "修复" : "接入" };
  if (adapterCanStage(adapter)) return { kind: "stage", label: "生成片段" };
  return null;
}

function syncAdapterDialogAction(adapter) {
  const action = els.adapterDialogAction;
  if (!action) return;
  const def = adapterDialogActionDef(adapter);
  if (!def) {
    action.hidden = true;
    action.removeAttribute("data-adapter-action");
    return;
  }
  action.hidden = false;
  action.textContent = def.label;
  action.dataset.adapterAction = def.kind;
}

function openAdapterInfo(adapter) {
  if (!els.adapterModal) return;
  state.adapterInfo = adapter;
  state.lastFocus = document.activeElement;
  renderAdapterInfo(adapter);
  syncAdapterDialogAction(adapter);
  els.adapterModal.classList.remove("closing");
  showModal(els.adapterModal);
  requestAnimationFrame(() => $("close-adapter-dialog")?.focus({ preventScroll: true }));
}

function closeAdapterInfo() {
  if (!els.adapterModal || els.adapterModal.hidden || els.adapterModal.classList.contains("closing")) return;
  els.adapterModal.classList.add("closing");
  const restoreFocus = state.lastFocus;
  state.lastFocus = null;
  state.adapterInfo = null;
  window.setTimeout(() => {
    if (!els.adapterModal.classList.contains("closing")) return;
    hideModal(els.adapterModal);
    els.adapterModal.classList.remove("closing");
    if (!isAnyModalOpen() && restoreFocus && typeof restoreFocus.focus === "function") {
      restoreFocus.focus();
    }
  }, 260);
}

function runAdapterDialogAction() {
  const adapter = state.adapterInfo;
  if (!adapter) return;
  const install = adapterCanInstall(adapter);
  closeAdapterInfo();
  if (install) runAdapterAction(adapter, null);
  else if (adapterCanStage(adapter)) stageAdapter(adapter, null);
}

async function loadInbox() {
  const region = $("inbox-results");
  clear(region);
  const list = element("div", "list");
  list.append(
    element("div", "row loading-row"),
    element("div", "row loading-row"),
  );
  region.appendChild(list);
  try {
    const data = await api("/api/inbox");
    state.inbox = data.candidates || [];
    renderInbox();
  } catch (error) {
    clear(region);
    const list = element("div", "list");
    list.appendChild(emptyState("收件箱加载失败", formatError(error), "重试", loadInbox));
    region.appendChild(list);
  }
}

function renderInbox() {
  const region = $("inbox-results");
  clear(region);
  $("inbox-total").textContent = String(state.inbox.length);
  if (!state.inbox.length) {
    const list = element("div", "list");
    list.appendChild(emptyState("收件箱是空的", "会话结束后，候选记忆会出现在这里。"));
    region.appendChild(list);
    return;
  }
  const list = element("div", "list");
  state.inbox.forEach((candidate) => {
    const row = element("div", "row inbox-row");
    row.append(icon("inbox"));
    const who = element("div", "who");
    who.dataset.noI18n = "true";
    who.append(
      element("div", "name", candidate.name || "候选记忆"),
      element("div", "sub", candidate.description || ""),
    );
    const meta = element("div", "memory-meta");
    meta.append(
      element("span", "pill", candidate.agent || "agent"),
      element("span", "memory-time", relativeDate(candidate.created)),
    );
    const actions = element("div", "inbox-actions");
    const keep = element("button", "button primary compact", "保留");
    keep.type = "button";
    keep.addEventListener("click", () => promoteCandidate(candidate));
    const drop = element("button", "button quiet compact", "丢弃");
    drop.type = "button";
    drop.addEventListener("click", () => dismissCandidate(candidate));
    actions.append(keep, drop);
    row.append(who, meta, actions);
    list.appendChild(row);
  });
  region.appendChild(list);
}

async function promoteCandidate(candidate) {
  try {
    const result = await api("/api/inbox/promote", {
      method: "POST",
      body: JSON.stringify({ path: candidate.path }),
    });
    toastDataResult(result, "已保留");
    await loadStatus();
    await loadInbox();
  } catch (error) {
    toast(formatError(error), "error");
  }
}

async function dismissCandidate(candidate) {
  if (!window.confirm(tr("丢弃这条候选记忆？"))) return;
  try {
    const result = await api("/api/inbox/dismiss", {
      method: "POST",
      body: JSON.stringify({ path: candidate.path }),
    });
    toastDataResult(result, "已丢弃");
    await loadStatus();
    await loadInbox();
  } catch (error) {
    toast(formatError(error), "error");
  }
}

async function loadAdapters() {
  clear(els.adapterRegion);
  const list = element("div", "list");
  list.append(
    element("div", "row loading-row"),
    element("div", "row loading-row"),
    element("div", "row loading-row"),
  );
  els.adapterRegion.appendChild(list);
  try {
    const data = await api("/api/adapters");
    state.adapters = Array.isArray(data.adapters) ? data.adapters : [];
    renderAdapters();
    return true;
  } catch (error) {
    clear(els.adapterRegion);
    els.adapterCount.textContent = "不可用";
    const list = element("div", "list");
    list.appendChild(emptyState("适配器加载失败", formatError(error), "重试", loadAdapters));
    els.adapterRegion.appendChild(list);
    return false;
  }
}

function renderAdapterRow(adapter, list) {
  const mode = adapterMode(adapter);
  const entry = element("article", "adapter-entry");
  entry.dataset.adapter = adapter.id || "";
  const row = element("div", "row adapter-row");
  row.append(icon("plug", "ic adapter-icon"));

  const who = element("div", "who");
  who.dataset.noI18n = "true";
  const nameLine = element("div", "adapter-name-line");
  nameLine.append(
    element("span", "name", adapter.title || adapter.id || "Adapter"),
    element("code", "", adapter.id || ""),
  );
  // Config targets read the same in every language and are what people look
  // for when deciding whether to install; long notes stay in the details dialog.
  const targets = [...new Set([adapter.hooks?.target, adapter.mcp?.target].filter(Boolean))];
  who.append(nameLine, element("div", "sub", targets.join("  ·  ") || adapter.detected_path || ""));
  row.appendChild(who);

  const st = adapterState(adapter);
  const stateNode = element("span", `adapter-state${st.className}`, st.label);
  if (adapter.detected_path) stateNode.title = adapter.detected_path;
  row.appendChild(stateNode);

  const modeBadge = element(
    "span",
    `pill adapter-mode is-${mode.replace("_", "-")}`,
    ADAPTER_MODE_LABELS[mode],
  );
  modeBadge.title = ADAPTER_MODE_HINTS[mode];
  row.appendChild(modeBadge);

  const tierLabel = ADAPTER_TIER_LABELS[adapter.tier] || adapter.tier || "";
  if (tierLabel) {
    const tier = element("span", "pill adapter-tier", tierLabel);
    tier.title = adapter.tier || "";
    row.appendChild(tier);
  }

  const actions = element("div", "adapter-actions");
  const primary = adapterActionButton(adapter);
  if (primary) actions.appendChild(primary);
  const info = element("button", "button quiet adapter-action", "查看说明");
  info.type = "button";
  info.addEventListener("click", () => openAdapterInfo(adapter));
  actions.appendChild(info);
  row.appendChild(actions);

  entry.appendChild(row);
  list.appendChild(entry);
}

function renderAdapters() {
  clear(els.adapterRegion);
  els.adapterCount.textContent = String(state.adapters.length);
  if (!state.adapters.length) {
    const list = element("div", "list");
    list.appendChild(emptyState("没有适配器", "本地 hub 没有报告可用的 Agent。"));
    els.adapterRegion.appendChild(list);
    return;
  }

  const groups = new Map(ADAPTER_MODES.map((mode) => [mode, []]));
  state.adapters
    .slice()
    .sort((a, b) => (b.detected ? 1 : 0) - (a.detected ? 1 : 0) || String(a.title || a.id).localeCompare(String(b.title || b.id), "zh-CN"))
    .forEach((adapter) => groups.get(adapterMode(adapter)).push(adapter));

  ADAPTER_MODES.forEach((mode) => {
    const adapters = groups.get(mode);
    if (!adapters.length) return;
    const group = element("section", "adapter-group");
    group.dataset.mode = mode;
    const head = element("div", "adapter-group-head");
    head.append(
      element("span", "adapter-group-name", ADAPTER_MODE_GROUP_LABELS[mode]),
      element("span", "adapter-group-count", String(adapters.length)),
    );
    group.appendChild(head);
    group.appendChild(element("p", "adapter-group-hint", ADAPTER_MODE_HINTS[mode]));
    const list = element("div", "list");
    adapters.forEach((adapter) => renderAdapterRow(adapter, list));
    group.appendChild(list);
    els.adapterRegion.appendChild(group);
  });
}

async function loadHistory() {
  clear(els.historyRegion);
  const loading = element("div", "list");
  loading.append(element("div", "row loading-row"), element("div", "row loading-row"));
  els.historyRegion.appendChild(loading);

  if (!state.status) await loadStatus();
  try {
    const data = await api("/api/git/log?n=20");
    state.history = Array.isArray(data.commits) ? data.commits : [];
    renderHistory();
    return true;
  } catch (error) {
    clear(els.historyRegion);
    const list = element("div", "list");
    list.appendChild(emptyState("Git 历史不可用", formatError(error), "重试", loadHistory));
    els.historyRegion.appendChild(list);
    return false;
  }
}

function renderHistory() {
  clear(els.historyRegion);
  const git = state.status?.git;
  if (git?.available === false) {
    const list = element("div", "list");
    list.appendChild(emptyState("Git 不可用", "当前仓库没有可读取的 Git 历史。"));
    els.historyRegion.appendChild(list);
    return;
  }
  if (!state.history.length) {
    const list = element("div", "list");
    list.appendChild(emptyState("还没有提交", "保存记忆后会在这里出现。"));
    els.historyRegion.appendChild(list);
    return;
  }
  const list = element("div", "list");
  state.history.forEach((commit) => {
    const entry = element("article", "history-entry");
    const row = element("div", "row history-row");
    row.append(
      icon("branch"),
      element("code", "history-hash", String(commit.hash || "").slice(0, 8)),
      element("span", "history-subject", commitLabel(commit.subject || "无提交说明")),
      element("span", "history-date", commit.date || ""),
    );
    entry.appendChild(row);
    list.appendChild(entry);
  });
  els.historyRegion.appendChild(list);
}

const VIEW_LABELS = { memories: "全部记忆", inbox: "收件箱", adapters: "适配器", history: "历史", settings: "设置" };

// Settings copy Magpie's preference pages: tabs at the top, one card of
// .row.pref rows under them. The tab strip and the search stay put while the
// page scrolls, and typing in the search shows each page's matching rows.
function positionSetThumb(box) {
  if (!box) return;
  const thumb = box.querySelector(":scope > .thumb");
  const active = box.querySelector(":scope > .opt.on");
  if (!thumb) return;
  if (!active || !box.offsetParent) {
    thumb.style.opacity = "0";
    return;
  }
  thumb.style.opacity = "1";
  thumb.style.width = `${active.offsetWidth}px`;
  thumb.style.height = `${active.offsetHeight}px`;
  thumb.style.transform = `translate(${active.offsetLeft - 2}px, 0)`;
}

function positionSetThumbs() {
  document.querySelectorAll("#view-settings .segs").forEach(positionSetThumb);
}

function showSettingsPage(name) {
  const tabs = [...document.querySelectorAll("#set-tabs [data-set]")];
  const known = tabs.some((tab) => tab.dataset.set === name);
  const target = known ? name : "general";
  tabs.forEach((tab) => {
    const on = tab.dataset.set === target;
    tab.classList.toggle("on", on);
    tab.setAttribute("aria-selected", String(on));
    tab.tabIndex = on ? 0 : -1;
  });
  document.querySelectorAll("#view-settings .set-page").forEach((page) => {
    page.hidden = page.id !== `setPage-${target}`;
  });
  const openPage = document.getElementById(`setPage-${target}`);
  if (openPage) {
    [...openPage.querySelectorAll(".row.pref")].forEach((row, index) => {
      row.style.setProperty("--i", String(Math.min(index, 10)));
    });
  }
  requestAnimationFrame(positionSetThumbs);
}

function applySettingsSearch(raw) {
  const view = $("view-settings");
  if (!view) return;
  const query = String(raw || "").trim().toLowerCase();
  view.classList.toggle("set-searching", Boolean(query));
  let total = 0;
  document.querySelectorAll("#view-settings .set-page").forEach((page) => {
    let hits = 0;
    page.querySelectorAll(".row.pref").forEach((row) => {
      const match = !query || row.textContent.toLowerCase().includes(query);
      row.hidden = Boolean(query) && !match;
      if (match) hits += 1;
    });
    if (query) page.hidden = hits === 0;
    total += hits;
  });
  const none = $("set-none");
  if (none) none.hidden = !query || total > 0;
  if (!query) {
    const active = document.querySelector("#set-tabs .on")?.dataset.set || "general";
    showSettingsPage(active);
  }
}

function resetSettingsSearch() {
  const input = $("set-q");
  if (!input || !input.value) return;
  input.value = "";
  applySettingsSearch("");
}

function switchView(view, { autoLoadMemories = true } = {}) {
  cancelPendingSearch();
  state.view = view;
  document.body.classList.toggle("view-settings", view === "settings");
  if (view !== "memories") {
    state.selectedPath = "";
    hideMemoryDetail();
  }
  document.querySelectorAll("[data-view]").forEach((tab) => {
    const active = tab.dataset.view === view;
    tab.classList.toggle("on", active);
    if (active) tab.setAttribute("aria-current", "page");
    else tab.removeAttribute("aria-current");
  });
  document.querySelectorAll(".view-panel").forEach((panel) => {
    panel.hidden = panel.id !== `view-${view}`;
  });
  updateBreadcrumb();
  positionSegThumb();
  const viewport = document.querySelector(".view");
  if (viewport) viewport.scrollTop = 0;
  requestAnimationFrame(() => {
    window.paintScrollEnds?.();
    positionSideBars();
  });
  $("all-memories")?.classList.toggle("is-active", view === "memories" && !state.project);
  document.querySelectorAll(".project-row").forEach((row) => {
    row.classList.toggle("is-active", view === "memories" && Boolean(state.project) && row.dataset.project === state.project);
  });
  requestAnimationFrame(positionSideHighlight);
  window.setTimeout(positionSideHighlight, 60);
  const newButton = $("new-memory-top");
  if (newButton) newButton.hidden = view !== "memories";
  if (view === "inbox") loadInbox();
  if (view === "adapters" && !state.adapters.length) loadAdapters();
  if (view === "history") loadHistory();
  if (view === "settings") {
    loadSettings();
    requestAnimationFrame(positionSetThumbs);
  } else {
    resetSettingsSearch();
  }
  if (view === "memories" && !state.results.length && autoLoadMemories) loadMemories();
}

// animateTopNavFromSidebar mirrors Magpie's navMorph: the sidebar menu lands
// in the title bar as horizontal tabs instead of disappearing with the panel.
function animateTopNavFromSidebar(fromRect) {
  const seg = document.getElementById("main-nav");
  if (!seg || !fromRect) return;
  const to = seg.getBoundingClientRect();
  if (!to.width) return;
  positionSegThumb();
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  const dx = fromRect.left + fromRect.width / 2 - (to.left + to.width / 2);
  const dy = fromRect.top + fromRect.height / 2 - (to.top + to.height / 2);
  const baseTransform = "translate(-50%, -50%)";
  const startTransform = `${baseTransform} translate(${dx}px, ${dy}px) scale(.35)`;
  seg.getAnimations().forEach((animation) => animation.cancel());
  seg.style.transform = startTransform;
  seg.style.opacity = ".15";
  const animation = seg.animate(
    [
      { transform: startTransform, opacity: .15 },
      { transform: baseTransform, opacity: 1 },
    ],
    { duration: 300, easing: "cubic-bezier(.2, .8, .2, 1)", fill: "forwards" },
  );
  let cleaned = false;
  const cleanup = () => {
    if (cleaned) return;
    cleaned = true;
    seg.style.transform = "";
    seg.style.opacity = "";
    animation.cancel();
  };
  animation.finished.then(cleanup).catch(cleanup);
  window.setTimeout(cleanup, 380);
}

// animateTopNavToSidebar is the reverse: the top tabs fold back down into the
// sidebar's first navigation row instead of blinking away.
function animateTopNavToSidebar(fromRect) {
  const seg = document.getElementById("main-nav");
  if (!seg || !fromRect) return;
  seg.classList.add("nav-return");
  const base = seg.getBoundingClientRect();
  const row = document.querySelector(".side-scroll .side-row");
  const sidebar = document.querySelector(".sidebar");
  if (!row || !sidebar || !base.width) {
    seg.classList.remove("nav-return");
    return;
  }
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
    seg.classList.remove("nav-return");
    return;
  }
  const rowRect = row.getBoundingClientRect();
  const sidebarRect = sidebar.getBoundingClientRect();
  const targetX = rowRect.left - sidebarRect.left + rowRect.width / 2;
  const targetY = rowRect.top + rowRect.height / 2;
  const dx = targetX - (base.left + base.width / 2);
  const dy = targetY - (base.top + base.height / 2);
  const baseTransform = "translate(-50%, -50%)";
  const endTransform = `${baseTransform} translate(${dx}px, ${dy}px) scale(.35)`;
  seg.getAnimations().forEach((animation) => animation.cancel());
  const animation = seg.animate(
    [
      { transform: baseTransform, opacity: 1 },
      { transform: endTransform, opacity: 0 },
    ],
    { duration: 260, easing: "cubic-bezier(.2, .8, .2, 1)", fill: "forwards" },
  );
  let cleaned = false;
  const cleanup = () => {
    if (cleaned) return;
    cleaned = true;
    seg.classList.remove("nav-return");
    seg.getAnimations().forEach((item) => item.cancel());
    animation.cancel();
  };
  animation.finished.then(cleanup).catch(cleanup);
  window.setTimeout(cleanup, 340);
}

// positionSegThumb slides the segmented control's thumb under the picked tab.
function positionSegThumb() {
  const seg = document.getElementById("main-nav");
  const thumb = document.getElementById("seg-thumb");
  if (!seg || !thumb) return;
  const active = seg.querySelector("button.on");
  if (!active || seg.offsetWidth === 0) {
    thumb.style.opacity = "0";
    return;
  }
  thumb.style.opacity = "";
  const x = active.offsetLeft;
  const w = active.offsetWidth;
  const first = !thumb.style.width;
  if (first) thumb.classList.add("still");
  thumb.style.transform = `translateX(${x}px)`;
  thumb.style.width = `${w}px`;
  if (first) {
    void thumb.offsetWidth;
    thumb.classList.remove("still");
  }
}

/* Magpie measures the scrollbar into --bar, so the 1200px column does not
   shift by its width when a long page grows a scrollbar and a short one
   does not. WebKit's overlay bar measures 0, which is the right answer there. */
function measureScrollbarWidth() {
  const probe = element("div", "");
  probe.style.cssText = "position:absolute;top:-1000px;left:0;width:50px;height:20px;overflow-y:scroll;visibility:hidden";
  document.body.appendChild(probe);
  const width = probe.offsetWidth - probe.clientWidth;
  probe.remove();
  document.documentElement.style.setProperty("--bar", `${Math.max(0, width)}px`);
}

function clearFilters() {
  cancelPendingSearch();
  state.query = "";
  state.project = "";
  state.type = "";
  state.selectedPath = "";
  state.detailLoading = false;
  els.search.value = "";
  els.searchClear.hidden = true;
  els.projectFilter.value = "";
  els.typeFilter.value = "";
  syncFilterLabels();
  renderProjects(state.status?.projects || []);
  $("all-memories").classList.add("is-active");
  loadMemories({ preserveSelection: true });
}

function openDialog() {
  state.lastFocus = document.activeElement;
  clear(els.formError);
  els.formError.hidden = true;
  $("memory-project").value = state.project || "global";
  $("memory-type").value = state.type || "project";
  $("memory-name").value = "";
  $("memory-description").value = "";
  $("memory-body").value = "";
  els.modal.classList.remove("closing");
  showModal(els.modal);
  window.setTimeout(() => $("memory-project").focus(), 30);
}

function closeDialog() {
  if (els.modal.hidden || els.modal.classList.contains("closing")) return;
  els.modal.classList.add("closing");
  const restoreFocus = state.lastFocus;
  state.lastFocus = null;
  window.setTimeout(() => {
    if (!els.modal.classList.contains("closing")) return;
    hideModal(els.modal);
    els.modal.classList.remove("closing");
    if (!isAnyModalOpen() && restoreFocus && typeof restoreFocus.focus === "function") {
      restoreFocus.focus();
    }
  }, 260);
}

async function submitMemory(event) {
  event.preventDefault();
  if (!els.form.reportValidity()) return;
  els.formError.hidden = true;
  setButtonLoading(els.submit, true, "正在保存…");
  const payload = {
    project: $("memory-project").value.trim(),
    name: $("memory-name").value.trim(),
    description: $("memory-description").value.trim(),
    type: $("memory-type").value,
    body: $("memory-body").value,
  };

  try {
    const result = await api("/api/memory", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    closeDialog();
    toastDataResult(result, "记忆已保存");
    await loadStatus();
    state.project = payload.project;
    state.type = "";
    state.query = "";
    els.typeFilter.value = "";
    els.search.value = "";
    els.searchClear.hidden = true;
    els.projectFilter.value = payload.project;
    switchView("memories");
    await loadMemories({ preserveSelection: false });
    if (result.path) await selectMemory(result.path);
  } catch (error) {
    els.formError.textContent = formatError(error, "保存失败，请检查表单和本地 hub。");
    els.formError.hidden = false;
    els.formError.scrollIntoView({ block: "nearest" });
  } finally {
    setButtonLoading(els.submit, false);
  }
}

async function retryAll() {
  const status = await loadStatus({ announce: true });
  if (!status) return;
  if (state.view === "adapters") await loadAdapters();
  else if (state.view === "history") await loadHistory();
  else await loadMemories({ preserveSelection: false });
}

async function refreshCurrent() {
  const button = $("refresh-view");
  button.disabled = true;
  let refreshed = false;
  cancelPendingSearch();
  const selectedPath = state.selectedPath;
  state.details.clear();
  state.detailErrors.clear();
  state.detailLoading = false;
  try {
    if (state.view === "memories") {
      const status = await loadStatus();
      if (status) {
        await loadMemories({ preserveSelection: true });
        if (selectedPath && state.selectedPath === selectedPath) {
          await selectMemory(selectedPath, true);
        }
        refreshed = true;
      }
    } else if (state.view === "adapters") {
      await loadStatus();
      state.adapters = [];
      refreshed = await loadAdapters();
    } else if (state.view === "history") {
      await loadStatus();
      refreshed = await loadHistory();
    } else if (state.view === "settings") {
      await loadStatus();
      await loadSettings();
      refreshed = true;
    } else {
      refreshed = Boolean(await loadStatus());
    }
  } finally {
    button.disabled = false;
  }
  if (refreshed) toast("已刷新");
  else toast("刷新失败，请确认本地 hub 正在运行", "error");
}

function cancelPendingSearch() {
  if (state.searchTimer) {
    window.clearTimeout(state.searchTimer);
    state.searchTimer = 0;
  }
  state.searchGeneration += 1;
}

function openSearch(focus = true) {
  els.searchShell.classList.add("is-open");
  els.searchToggle.setAttribute("aria-expanded", "true");
  els.search.tabIndex = 0;
  if (focus) {
    // Focus now, not on the next frame: a key press should land in the field
    // even when the window is busy or the frame is throttled.
    els.search.focus();
    requestAnimationFrame(() => els.search.select());
  }
}

function closeSearch() {
  if (els.search.value.trim()) return;
  els.searchShell.classList.remove("is-open");
  els.searchToggle.setAttribute("aria-expanded", "false");
  els.search.tabIndex = -1;
}

function toggleSearch() {
  if (els.searchShell.classList.contains("is-open")) {
    if (els.search.value.trim()) els.search.focus();
    else closeSearch();
    return;
  }
  openSearch();
}

function focusSearch() {
  openSearch();
}

function bindEvents() {
  els.search.addEventListener("input", () => {
    const query = els.search.value.trim();
    state.query = query;
    state.selectedPath = "";
    els.searchClear.hidden = !query;
    const generation = ++state.searchGeneration;
    if (state.searchTimer) window.clearTimeout(state.searchTimer);
    state.searchTimer = window.setTimeout(() => {
      state.searchTimer = 0;
      if (generation !== state.searchGeneration) return;
      if (state.view !== "memories" && document.activeElement !== els.search) return;
      if (state.view !== "memories") switchView("memories", { autoLoadMemories: false });
      loadMemories({ preserveSelection: false });
    }, 240);
  });

  els.searchClear.addEventListener("click", () => {
    cancelPendingSearch();
    els.search.value = "";
    state.query = "";
    state.selectedPath = "";
    els.searchClear.hidden = true;
    els.search.focus();
    loadMemories({ preserveSelection: false });
  });

  els.searchToggle.setAttribute("aria-expanded", "false");
  els.searchToggle.addEventListener("click", toggleSearch);
  els.search.addEventListener("blur", () => {
    window.setTimeout(() => {
      if (!els.search.value.trim()) closeSearch();
    }, 140);
  });
  els.search.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    if (els.search.value.trim()) {
      event.preventDefault();
      cancelPendingSearch();
      els.search.value = "";
      state.query = "";
      state.selectedPath = "";
      els.searchClear.hidden = true;
      loadMemories({ preserveSelection: false });
      return;
    }
    els.search.blur();
    closeSearch();
  });

  const viewport = document.querySelector(".view");
  if (viewport) viewport.addEventListener("scroll", queueLoadMoreCheck, { passive: true });

  els.resultRegion.addEventListener("keydown", (event) => {
    if (!/^(ArrowLeft|ArrowRight|ArrowUp|ArrowDown|Home|End)$/.test(event.key)) return;
    const cards = [...els.resultRegion.querySelectorAll(".memory-card")];
    const index = cards.indexOf(document.activeElement);
    if (index < 0) return;
    event.preventDefault();
    let next = index;
    if (event.key === "Home") next = 0;
    else if (event.key === "End") next = cards.length - 1;
    else {
      const columns = wallColumns(cards);
      const step = event.key === "ArrowUp" ? -columns
        : event.key === "ArrowDown" ? columns
        : event.key === "ArrowLeft" ? -1 : 1;
      next = Math.min(cards.length - 1, Math.max(0, index + step));
    }
    cards[next].focus();
  });

  document.addEventListener("keydown", (event) => {
    if (event.key === "/" && !event.metaKey && !event.ctrlKey && !event.altKey) {
      const target = event.target;
      const typing = target instanceof HTMLElement
        && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable);
      if (!typing && !isAnyModalOpen()) {
        event.preventDefault();
        focusSearch();
      }
      return;
    }
    if ((event.metaKey || event.ctrlKey) && !event.altKey && !event.shiftKey && (event.key === "," || event.code === "Comma")) {
      if (isAnyModalOpen()) return;
      event.preventDefault();
      switchView("settings");
      return;
    }
    if (event.metaKey && event.ctrlKey && event.key.toLowerCase() === "s") {
      event.preventDefault();
      toggleSidebarCollapsed();
      return;
    }
    if ((event.metaKey || event.ctrlKey) && !event.altKey && (event.key === "+" || event.key === "=")) {
      event.preventDefault();
      stepReadSize(1);
      return;
    }
    if ((event.metaKey || event.ctrlKey) && !event.altKey && event.key === "-") {
      event.preventDefault();
      stepReadSize(-1);
      return;
    }
    if ((event.metaKey || event.ctrlKey) && !event.altKey && event.key === "0") {
      event.preventDefault();
      applyReadSize("m");
      return;
    }
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
      if (isAnyModalOpen()) return;
      event.preventDefault();
      focusSearch();
    }
    if (event.key === "Escape") {
      if (!els.modal.hidden) {
        event.preventDefault();
        closeDialog();
        return;
      }
      if (els.adapterModal && !els.adapterModal.hidden) {
        event.preventDefault();
        closeAdapterInfo();
        return;
      }
      if (memoryDetailOpen()) {
        event.preventDefault();
        closeMemoryDetail();
      }
    }
  });

  document.querySelectorAll("[data-view]").forEach((tab) => {
    tab.addEventListener("click", () => switchView(tab.dataset.view));
  });

  $("all-memories").addEventListener("click", () => {
    state.type = "";
    els.typeFilter.value = "";
    selectProject("");
  });
  els.projectFilter.addEventListener("change", () => {
    cancelPendingSearch();
    state.project = els.projectFilter.value;
    renderProjects(state.status?.projects || []);
    $("all-memories").classList.toggle("is-active", !state.project);
    loadMemories({ preserveSelection: true });
  });
  els.typeFilter.addEventListener("change", () => {
    cancelPendingSearch();
    state.type = els.typeFilter.value;
    syncFilterLabels();
    loadMemories({ preserveSelection: true });
  });

  $("project-filter-button").addEventListener("click", openProjectMenu);
  $("type-filter-button").addEventListener("click", openTypeMenu);
  $("sort-button").addEventListener("click", openSortMenu);
  document.querySelectorAll("[data-read-choice]").forEach((button) => {
    button.addEventListener("click", () => applyReadSize(button.dataset.readChoice));
  });

  $("new-memory-top").addEventListener("click", openDialog);
  $("refresh-view").addEventListener("click", refreshCurrent);
  document.querySelectorAll("[data-theme-choice]").forEach((button) => {
    button.addEventListener("click", () => setTheme(button.dataset.themeChoice));
  });
  $("copy-root").addEventListener("click", () => copyText($("settings-root").textContent, "根目录", $("copy-root")));
  $("copy-token").addEventListener("click", () => copyText($("settings-token").textContent, "Token", $("copy-token")));
  $("reindex").addEventListener("click", rebuildIndex);
  $("backup-now").addEventListener("click", backupNow);
  $("save-aliases").addEventListener("click", saveAliases);
  const settingsTabs = [...document.querySelectorAll("#set-tabs [data-set]")];
  settingsTabs.forEach((tab) => {
    tab.addEventListener("click", () => showSettingsPage(tab.dataset.set));
  });
  $("set-tabs").addEventListener("keydown", (event) => {
    const currentIndex = settingsTabs.indexOf(document.activeElement);
    if (currentIndex < 0) return;
    let nextIndex = currentIndex;
    if (event.key === "ArrowRight") nextIndex = (currentIndex + 1) % settingsTabs.length;
    else if (event.key === "ArrowLeft") nextIndex = (currentIndex - 1 + settingsTabs.length) % settingsTabs.length;
    else if (event.key === "Home") nextIndex = 0;
    else if (event.key === "End") nextIndex = settingsTabs.length - 1;
    else return;
    event.preventDefault();
    const next = settingsTabs[nextIndex];
    showSettingsPage(next.dataset.set);
    next.focus();
  });
  $("set-q").addEventListener("input", () => applySettingsSearch($("set-q").value));
  $("set-q").addEventListener("search", () => applySettingsSearch($("set-q").value));
  document.querySelectorAll("[data-lang]").forEach((button) => {
    button.addEventListener("click", () => window.setTimeout(positionSetThumbs, 30));
  });
  window.addEventListener("resize", positionSetThumbs);
  const settingsViewport = document.querySelector(".view");
  if (settingsViewport) {
    settingsViewport.addEventListener("scroll", () => {
      const head = document.querySelector("#view-settings .set-head");
      if (head) head.classList.toggle("stuck", settingsViewport.scrollTop > 4);
    });
  }
  document.querySelectorAll("[data-import]").forEach((button) => {
    button.addEventListener("click", () => importSource(button.dataset.import, button));
  });
  $("retry-hub").addEventListener("click", retryAll);
  $("close-dialog").addEventListener("click", closeDialog);
  $("cancel-dialog").addEventListener("click", closeDialog);
  $("close-detail")?.addEventListener("click", closeMemoryDetail);
  els.detailModal?.addEventListener("click", (event) => {
    if (event.target === els.detailModal) closeMemoryDetail();
  });
  els.detailModal?.addEventListener("keydown", (event) => {
    if (event.key !== "Tab") return;
    const focusables = [...els.detailModal.querySelectorAll(
      'button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), a[href]',
    )].filter((node) => node.offsetParent !== null);
    if (!focusables.length) return;
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });
  $("close-adapter-dialog")?.addEventListener("click", closeAdapterInfo);
  $("adapter-dialog-close")?.addEventListener("click", closeAdapterInfo);
  els.adapterDialogAction?.addEventListener("click", runAdapterDialogAction);
  els.adapterModal?.addEventListener("click", (event) => {
    if (event.target === els.adapterModal) closeAdapterInfo();
  });
  els.adapterModal?.addEventListener("keydown", (event) => {
    if (event.key !== "Tab") return;
    const focusables = [...els.adapterModal.querySelectorAll(
      'button:not([disabled]), a[href]',
    )].filter((node) => node.offsetParent !== null);
    if (!focusables.length) return;
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });
  els.form.addEventListener("submit", submitMemory);
  els.modal.addEventListener("click", (event) => {
    if (event.target === els.modal) closeDialog();
  });
  els.modal.addEventListener("keydown", (event) => {
    if (event.key !== "Tab") return;
    const focusables = [...els.modal.querySelectorAll(
      'button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), a[href]',
    )].filter((node) => node.offsetParent !== null);
    if (!focusables.length) return;
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });
  document.querySelectorAll("#side-toggle, #side-toggle-top").forEach((button) => {
    button.addEventListener("click", () => {
      const wasCollapsed = document.body.classList.contains("side-collapsed");
      const sideRect = wasCollapsed
        ? null
        : document.querySelector(".side-scroll")?.getBoundingClientRect();
      const segEl = document.getElementById("main-nav");
      const topRect = wasCollapsed ? segEl?.getBoundingClientRect() : null;
      if (!wasCollapsed && segEl) segEl.classList.remove("nav-return");
      if (wasCollapsed && segEl) segEl.classList.add("nav-return");
      const collapsed = document.body.classList.toggle("side-collapsed");
      try {
        localStorage.setItem("marktoast-sidebar", collapsed ? "collapsed" : "open");
      } catch (error) {}
      syncSidebarControls();
      if (collapsed) animateTopNavFromSidebar(sideRect);
      if (!collapsed && wasCollapsed) animateTopNavToSidebar(topRect);
      window.setTimeout(positionSideHighlight, 0);
    });
  });
  document.querySelectorAll(".brand").forEach((brand) => {
    brand.addEventListener("click", (event) => {
      event.preventDefault();
      switchView("memories");
      focusSearch();
    });
  });
  window.addEventListener("resize", positionSegThumb);
  positionSegThumb();
  requestAnimationFrame(positionSetThumbs);
  window.addEventListener("online", () => loadStatus({ announce: true }));
  window.addEventListener("offline", () => {
    setHubState("offline", "浏览器已离线");
    showHubAlert("浏览器已离线。重新联网后可继续刷新本地记忆。");
  });
}


function flashCopied(button) {
  if (!button) return;
  const use = button.querySelector("use");
  if (!use) return;
  const original = use.getAttribute("href");
  window.clearTimeout(button.copiedTimer);
  use.setAttribute("href", "#icon-check");
  button.classList.add("done");
  button.copiedTimer = window.setTimeout(() => {
    use.setAttribute("href", original);
    button.classList.remove("done");
  }, 1200);
}

function copyText(value, label, button) {
  if (!value || value === "—") return;
  navigator.clipboard.writeText(value).then(
    () => {
      toast(`${label}已复制`);
      flashCopied(button);
    },
    () => toast("复制失败", "error"),
  );
}

async function loadSettings() {
  try {
    const data = await api("/api/settings");
    state.settings = data;
    state.aliases = data.aliases || state.aliases;
    renderSettings();
  } catch (error) {
    toast(formatError(error), "error");
  }
}

function renderSettings() {
  const settings = state.settings;
  if (!settings) return;
  $("settings-root").textContent = settings.root || "—";
  $("settings-root").title = settings.root || "";
  $("settings-facts").textContent = `${settings.facts || 0} 条`;
  const idx = settings.index || {};
  $("settings-index").textContent = idx.builtAt
    ? `${idx.facts || 0} 条 · ${formatDate(idx.builtAt, true)}`
    : "未建立";
  $("settings-token").textContent = settings.token || "—";
  renderAliasList(settings);
  applyTheme();
  requestAnimationFrame(positionSetThumbs);
}

function renderAliasList(settings) {
  const list = $("alias-list");
  clear(list);
  const projects = (state.status?.projects || []).slice().sort(
    (a, b) => (Number(b.facts) || 0) - (Number(a.facts) || 0) || projectLabel(a.key).localeCompare(projectLabel(b.key), "zh-CN"),
  );
  if (!projects.length) {
    list.hidden = true;
    return;
  }
  list.hidden = false;
  const aliases = settings?.aliases || state.aliases || {};
  projects.forEach((project) => {
    const row = element("div", "row pref alias-row");
    row.dataset.key = project.key;
    const who = element("div", "who");
    who.appendChild(element("div", "name alias-key", project.key));
    who.appendChild(element("div", "sub", `${Number(project.facts) || 0} 条记忆`));
    row.appendChild(who);
    const val = element("div", "val");
    const input = element("input", "set-field");
    input.type = "text";
    input.maxLength = 80;
    input.value = aliases[project.key] || "";
    input.placeholder = "显示名";
    input.autocomplete = "off";
    input.spellcheck = false;
    input.setAttribute("aria-label", `项目 ${project.key} 的显示名`);
    val.appendChild(input);
    row.appendChild(val);
    list.appendChild(row);
  });
}

async function saveAliases() {
  const aliases = {};
  document.querySelectorAll(".alias-row").forEach((row) => {
    const value = row.querySelector("input")?.value.trim();
    if (value) aliases[row.dataset.key] = value;
  });
  try {
    const data = await api("/api/aliases", {
      method: "PUT",
      body: JSON.stringify({ aliases }),
    });
    state.aliases = data.aliases || {};
    if (state.settings) state.settings.aliases = state.aliases;
    renderAliasList(state.settings);
    renderProjects(state.status?.projects || []);
    renderProjectOptions(state.status?.projects || []);
    flash($("save-aliases")?.closest(".row.pref"));
    updateHeading();
    if (state.view === "memories") renderMemories();
    toast("已保存");
  } catch (error) {
    toast(formatError(error), "error");
  }
}

async function importSource(from, button) {
  const label = button.textContent;
  button.disabled = true;
  button.textContent = "导入中…";
  try {
    const data = await api("/api/import", {
      method: "POST",
      body: JSON.stringify({ from }),
      timeoutMs: 120_000,
    });
    const fresh = data.facts || 0;
    const skipped = data.skipped || 0;
    toast(fresh ? `已导入 ${fresh} 条` : skipped ? `没有新记忆（跳过 ${skipped} 条）` : "没有可导入的记忆");
    await loadStatus();
    if (state.view === "memories") await loadMemories({ preserveSelection: true });
    await loadSettings();
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    if (button.isConnected) {
      button.disabled = false;
      button.textContent = label;
    }
  }
}

async function backupNow() {
  const button = $("backup-now");
  button.disabled = true;
  try {
    const data = await api("/api/backup", { method: "POST", timeoutMs: 120_000 });
    $("settings-backup").textContent = data.path || "—";
    $("settings-backup").title = data.path || "";
    toast("备份已创建");
    flash($("backup-now")?.closest(".row.pref"));
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    button.disabled = false;
  }
}

async function rebuildIndex() {
  const button = $("reindex");
  button.disabled = true;
  try {
    const data = await api("/api/reindex", { method: "POST", timeoutMs: 120_000 });
    toast(`索引已重建 · ${data.facts || 0} 条`);
    flash($("reindex")?.closest(".row.pref"));
    await loadSettings();
    await loadStatus();
    if (state.view === "memories") loadMemories();
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    button.disabled = false;
  }
}

function renderMemoryHistory(path) {
  const history = state.memoryHistory;
  const box = element("div", "memory-history");
  box.appendChild(element("h3", "", "历史"));
  if (history.loading) {
    const loading = element("div", "detail-loading");
    loading.append(element("span", "skeleton"), element("span", "skeleton"));
    box.appendChild(loading);
    return box;
  }
  if (history.error) {
    const error = element("div", "expanded-error");
    error.append(
      icon("warning"),
      element("span", "", formatError(history.error)),
      (() => {
        const retry = element("button", "text-button", "重试");
        retry.type = "button";
        retry.addEventListener("click", () => toggleMemoryHistory(path, true));
        return retry;
      })(),
    );
    box.appendChild(error);
    return box;
  }
  if (!history.commits.length) {
    box.appendChild(element("p", "muted", "还没有提交记录"));
    return box;
  }
  const list = element("div", "memory-commits");
  history.commits.forEach((commit) => {
    const row = element("button", `memory-commit${commit.hash === history.selectedHash ? " on" : ""}`);
    row.type = "button";
    row.setAttribute("aria-pressed", String(commit.hash === history.selectedHash));
    row.append(
      element("code", "", String(commit.hash || "").slice(0, 8)),
      element("span", "", commitLabel(commit.subject || "")),
      element("time", "", commit.date || ""),
    );
    row.addEventListener("click", () => loadMemoryDiff(path, commit.hash));
    list.appendChild(row);
  });
  box.appendChild(list);
  if (history.patchLoading) {
    box.appendChild(element("div", "detail-loading"));
  } else if (history.patchError) {
    box.appendChild(element("p", "muted", formatError(history.patchError)));
  } else if (history.patch) {
    const actions = element("div", "diff-actions");
    const restore = element("button", "button quiet", history.restoring ? "恢复中…" : "恢复此版本");
    restore.type = "button";
    restore.disabled = history.restoring;
    restore.addEventListener("click", () => restoreMemoryVersion(path, history.selectedHash));
    actions.appendChild(restore);
    box.appendChild(actions);
    const diff = element("div", "diff-box");
    diff.appendChild(element("pre", "", history.patch));
    box.appendChild(diff);
  }
  return box;
}

async function toggleMemoryHistory(path, force = false) {
  const history = state.memoryHistory;
  if (!force && history.open && history.path === path && !history.loading) {
    history.open = false;
    renderMemories();
    return;
  }
  Object.assign(history, {
    path,
    open: true,
    loading: true,
    commits: [],
    error: null,
    selectedHash: "",
    patch: "",
    patchError: null,
    patchLoading: false,
    restoring: false,
  });
  renderMemories();
  try {
    const data = await api(`/api/memory/history?path=${encodeURIComponent(path)}`);
    if (history.path !== path) return;
    history.commits = data.commits || [];
    history.loading = false;
  } catch (error) {
    if (history.path !== path) return;
    history.loading = false;
    history.error = error;
  }
  renderMemories();
}

async function loadMemoryDiff(path, hash) {
  const history = state.memoryHistory;
  Object.assign(history, { selectedHash: hash, patch: "", patchError: null, patchLoading: true });
  renderMemories();
  try {
    const data = await api(`/api/memory/diff?path=${encodeURIComponent(path)}&hash=${encodeURIComponent(hash)}`);
    if (history.selectedHash !== hash) return;
    history.patch = data.patch || "";
  } catch (error) {
    if (history.selectedHash !== hash) return;
    history.patchError = error;
  }
  history.patchLoading = false;
  renderMemories();
}

async function restoreMemoryVersion(path, hash) {
  if (!hash) return;
  if (!window.confirm(tr("恢复这个版本？会生成一次新的提交。"))) return;
  const history = state.memoryHistory;
  history.restoring = true;
  renderMemories();
  try {
    const result = await api("/api/memory/restore", {
      method: "POST",
      body: JSON.stringify({ path, hash }),
    });
    state.details.delete(path);
    toastDataResult(result, "已恢复");
    flash(document.querySelector(`.memory-card[data-path="${CSS.escape(path)}"]`));
    await selectMemory(path, true);
    const data = await api(`/api/memory/history?path=${encodeURIComponent(path)}`);
    history.commits = data.commits || [];
    history.selectedHash = "";
    history.patch = "";
    history.patchError = null;
    await loadStatus();
    await loadMemories({ preserveSelection: true });
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    history.restoring = false;
    renderMemories();
  }
}

async function init() {
  syncSidebarControls();
  syncPageInert();
  showSettingsPage(document.querySelector("#set-tabs .on")?.dataset.set || "general");
  applyReadSize(readSizePreference(), { persist: false });
  measureScrollbarWidth();
  applySideWidth();
  initSidebarEdge();
  attachSideBar(document.querySelector(".side-scroll"));
  attachSideBar($("project-list"));
  initNavLamp();
  initTooltip();
  window.paintScrollEnds = initScrollEnds();
  window.addEventListener("resize", () => {
    if (state.barFrame) cancelAnimationFrame(state.barFrame);
    state.barFrame = requestAnimationFrame(measureScrollbarWidth);
  });
  bindEvents();
  renderMemoryLoading();
  applyTheme();
  const status = await loadStatus();
  if (status) await loadMemories();
  else renderMemoryError(new Error("无法连接本地 hub。请运行 `marktoast serve` 后重试。"));
  window.appReady = true;
}

init();
