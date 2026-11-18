// MarkToast Web UI — dependency-free, local-first, served by the loopback hub.

"use strict";

const $ = (id) => document.getElementById(id);
const tr = (text) => (window.MarkToastI18n ? window.MarkToastI18n.t(text) : text);
const localized = (text) => (window.MarkToastI18n?.language === "en" ? window.MarkToastI18n.t(text) : text);

const API_TIMEOUT_MS = 15_000;
const MAX_RENDERED_BODY_CHARS = 200_000;
const CARD_PREVIEW_CHARS = 360;
const MEMORY_PAGE_SIZE = 50;
const SEARCH_PAGE_SIZE = 20;
const MAX_DETAIL_CACHE_ENTRIES = 80;

// Exact limits matching internal/store/store.go
const MAX_FACT_NAME_RUNES = 200;
const MAX_FACT_DESC_RUNES = 2_000;
const MAX_FACT_BODY_BYTES = 2 * 1024 * 1024;

const isMac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
if (isMac) document.body.classList.add("mac");

const desktopShell = Boolean(
  window._wails ||
  /^wails:/.test(location.protocol) ||
  location.hostname.includes("wails") ||
  /Wails/i.test(navigator.userAgent),
);
if (desktopShell) document.body.classList.add("desktop");

try {
  if (localStorage.getItem("marktoast-sidebar") === "collapsed") {
    document.body.classList.add("side-collapsed");
  }
} catch {}

/* ---- Application State -------------------------------------------------- */
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
  loadMoreError: null,
  newFrom: 0,

  selectedPath: "",
  detailScrollPath: "",
  detailViewMode: "rendered", // "rendered" | "raw"
  details: new Map(),
  detailErrors: new Map(),
  detailLoading: false,

  editingMemory: null, // { path, project, name, description, type, body } | null
  formInitial: null,
  formBodyMode: "write", // "write" | "preview"

  adapters: [],
  adapterQuery: "",
  adapterFilter: "all",
  adapterInfo: null,
  adapterLastReport: new Map(),

  inbox: [],
  inboxTargetProject: "global",
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

  cmdk: {
    query: "",
    memories: [],
    loading: false,
    activeIndex: 0,
    items: [],
  },

  barFrame: 0,
};

const els = {
  hubAlert: $("hub-alert"),
  hubAlertText: $("hub-alert-text"),
  hubStateText: $("hub-state-text"),
  statusDot: $("side-status-dot"),
  search: $("global-search"),
  searchShell: $("search-shell"),
  searchToggle: $("search-toggle"),
  searchKbd: $("search-kbd"),
  searchClear: $("search-clear"),
  projectList: $("project-list"),
  projectFilter: $("project-filter"),
  typeFilter: $("type-filter"),
  filterReset: $("active-filter-reset"),
  filterSummary: $("filter-summary"),
  resultRegion: $("memory-results"),
  resultTotal: $("result-total"),
  inboxToolbar: $("inbox-toolbar"),
  inboxSummary: $("inbox-summary-text"),
  inboxProjectSelect: $("inbox-project-select"),
  adapterRegion: $("adapter-results"),
  adapterCount: $("adapter-count"),
  adapterSearch: $("adapter-search"),
  adapterSearchClear: $("adapter-search-clear"),
  adapterStats: $("adapter-stats"),
  historyRegion: $("history-results"),
  modal: $("memory-modal"),
  detailModal: $("memory-detail-modal"),
  adapterModal: $("adapter-modal"),
  cmdkModal: $("cmdk-modal"),
  shortcutsModal: $("shortcuts-modal"),
  adapterDialogTitle: $("adapter-dialog-title"),
  adapterDialogBody: $("adapter-dialog-body"),
  adapterDialogAction: $("adapter-dialog-action"),
  adapterDialogUninstall: $("adapter-dialog-uninstall"),
  detailContent: $("detail-content"),
  form: $("memory-form"),
  formError: $("form-error"),
  submit: $("submit-memory"),
};

/* ---- UTF-8 & Surrogate-Safe String Primitives --------------------------- */
const utf8Encoder = new TextEncoder();

function runeCount(value) {
  let count = 0;
  for (const _ of String(value || "")) count += 1;
  return count;
}

function utf8ByteLength(value) {
  return utf8Encoder.encode(String(value || "")).length;
}

// Slice without splitting a UTF-16 high/low surrogate pair (emoji, CJK Ext B+).
function safeSlice(value, maxCodeUnits) {
  const str = String(value || "");
  if (str.length <= maxCodeUnits) return str;
  let end = Math.max(0, maxCodeUnits);
  const code = str.charCodeAt(end - 1);
  if (code >= 0xd800 && code <= 0xdbff) end -= 1;
  return str.slice(0, end);
}

function isIMEComposing(event) {
  return Boolean(event && (event.isComposing || event.keyCode === 229));
}

function isEditableTarget(target) {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || target.isContentEditable;
}

/* ---- DOM & Formatting Helpers ------------------------------------------- */
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
  if (!node) return;
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

function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function queryTokens(query) {
  const raw = String(query || "").trim();
  if (!raw) return [];
  const parts = raw
    .split(/\s+/)
    .map((part) => part.trim())
    .filter(Boolean);
  return [...new Set(parts)].slice(0, 6);
}

function appendHighlighted(parent, text, query) {
  const str = String(text ?? "");
  const tokens = queryTokens(query);
  if (!str || !tokens.length) {
    parent.appendChild(document.createTextNode(str));
    return;
  }
  const pattern = new RegExp(`(${tokens.map(escapeRegExp).join("|")})`, "gi");
  let last = 0;
  let match;
  while ((match = pattern.exec(str)) !== null) {
    if (match.index > last) {
      parent.appendChild(document.createTextNode(str.slice(last, match.index)));
    }
    const mark = element("mark", "hl", match[0]);
    parent.appendChild(mark);
    last = pattern.lastIndex;
    if (match[0].length === 0) pattern.lastIndex += 1;
  }
  if (last < str.length) parent.appendChild(document.createTextNode(str.slice(last)));
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

/* ---- IME-Safe Search Input Binder --------------------------------------- */
function bindSearchInput(input, { onCommit, onEscape, debounceMs = 0 }) {
  if (!input) return () => {};
  let composing = false;
  let timer = 0;
  let generation = 0;

  const cancel = () => {
    if (timer) {
      window.clearTimeout(timer);
      timer = 0;
    }
    generation += 1;
  };

  const schedule = (immediate = false) => {
    if (timer) {
      window.clearTimeout(timer);
      timer = 0;
    }
    const gen = ++generation;
    if (immediate || debounceMs <= 0) {
      onCommit(input.value);
      return;
    }
    timer = window.setTimeout(() => {
      timer = 0;
      if (gen === generation) onCommit(input.value);
    }, debounceMs);
  };

  input.addEventListener("compositionstart", () => {
    composing = true;
  });
  input.addEventListener("compositionend", () => {
    composing = false;
    schedule(false);
  });
  input.addEventListener("input", (event) => {
    if (composing || event.isComposing) return;
    schedule(false);
  });
  input.addEventListener("search", () => {
    if (composing) return;
    schedule(true);
  });
  if (onEscape) {
    input.addEventListener("keydown", (event) => {
      if (isIMEComposing(event) || composing) return;
      if (event.key === "Escape") onEscape(event, cancel);
    });
  }
  return cancel;
}

/* ---- Request Slots & HTTP Client ---------------------------------------- */
const requestSlots = new Map();

function beginRequestSlot(name) {
  const prev = requestSlots.get(name);
  if (prev) prev.controller.abort();
  const next = { id: (prev?.id || 0) + 1, controller: new AbortController() };
  requestSlots.set(name, next);
  return next;
}

function isCurrentSlot(name, token) {
  return requestSlots.get(name)?.id === token.id;
}

function abortRequestSlot(name) {
  const prev = requestSlots.get(name);
  if (prev) {
    prev.controller.abort();
    requestSlots.delete(name);
  }
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
        const text = await response.text();
        if (text.trim()) {
          try {
            const payload = JSON.parse(text);
            detail = String(payload?.error || payload?.message || "");
          } catch {
            detail = text.trim();
          }
        }
      } catch {}
      let message = detail || `HTTP ${response.status} ${response.statusText}`.trim();
      if (response.status === 401) message = "未授权，请重新连接本地 hub。";
      else if (response.status === 403) message = "没有权限执行此操作。";
      else if (response.status >= 500 && response.status <= 599 && !detail) {
        message = "本地 hub 暂时不可用，请稍后重试。";
      }
      const error = makeError(message, "ApiError", response.status);
      error.detail = detail;
      throw error;
    }

    if (response.status === 204 || response.status === 205) return null;
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
    if (contentType.includes("json") || /^[\s]*[\[{]/.test(text)) {
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

/* ---- Theme & Reading Size ----------------------------------------------- */
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
  if (meta) meta.setAttribute("content", dark ? "#141518" : "#f5f5f7");
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
  } catch {}
  applyTheme(preference);
  positionSetThumbs();
}

const READ_SIZE_KEY = "marktoast-read-size";
const READ_SIZES = ["s", "m", "l"];

function readSizePreference() {
  try {
    const saved = localStorage.getItem(READ_SIZE_KEY);
    return READ_SIZES.includes(saved) ? saved : "m";
  } catch {
    return "m";
  }
}

function applyReadSize(size, { persist = true } = {}) {
  const next = READ_SIZES.includes(size) ? size : "m";
  document.body.dataset.read = next;
  if (persist) {
    try {
      localStorage.setItem(READ_SIZE_KEY, next);
    } catch {}
  }
  document.querySelectorAll("[data-read-choice]").forEach((button) => {
    const on = button.dataset.readChoice === next;
    button.classList.toggle("on", on);
    button.setAttribute("aria-pressed", String(on));
  });
  positionSetThumbs();
}

function stepReadSize(delta) {
  const index = Math.max(0, READ_SIZES.indexOf(readSizePreference()));
  const next = READ_SIZES[Math.min(READ_SIZES.length - 1, Math.max(0, index + delta))];
  applyReadSize(next);
}

/* ---- Toasts, Status Pill & Feedback Animations -------------------------- */
function setHubState(kind) {
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

function toast(message, kind = "success") {
  if (kind !== "error") {
    showStatusPill(message, kind);
    return;
  }
  const region = $("toast-region");
  if (!region) return;
  const text = String(message || "");
  const existing = [...region.querySelectorAll(".toast span")].find((node) => node.textContent === text);
  if (existing) return;
  while (region.children.length >= 3) {
    region.firstElementChild.remove();
  }
  const node = element("div", "toast is-error");
  node.appendChild(icon("warning"));
  node.appendChild(element("span", "", text));
  region.appendChild(node);
  window.setTimeout(() => {
    node.style.opacity = "0";
    node.style.transform = "translateY(5px)";
    window.setTimeout(() => node.remove(), 180);
  }, 3600);
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
    window.clearTimeout(button.copiedTimer);
    button.classList.remove("is-copied", "done");
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
    delete button.dataset.original;
  }
}

function flash(node) {
  if (!node) return;
  node.classList.remove("flash");
  void node.offsetWidth;
  node.classList.add("flash");
  window.setTimeout(() => node.classList.remove("flash"), 1300);
}

function flashCopied(button) {
  if (!button) return;
  const use = button.querySelector("use");
  if (use) {
    const original = button.dataset.origIcon || use.getAttribute("href");
    button.dataset.origIcon = original;
    window.clearTimeout(button.copiedTimer);
    use.setAttribute("href", "#icon-check");
    button.classList.add("done", "is-copied");
    button.copiedTimer = window.setTimeout(() => {
      use.setAttribute("href", original);
      button.classList.remove("done", "is-copied");
    }, 1200);
    return;
  }
  const origText = button.dataset.origLabel || button.textContent;
  button.dataset.origLabel = origText;
  window.clearTimeout(button.copiedTimer);
  button.textContent = "已复制";
  button.classList.add("is-copied");
  button.copiedTimer = window.setTimeout(() => {
    button.textContent = origText;
    button.classList.remove("is-copied");
  }, 1200);
}

async function writeClipboard(value, successMessage, button) {
  if (value === undefined || value === null || value === "" || value === "—") return false;
  try {
    await navigator.clipboard.writeText(String(value));
    if (successMessage) toast(successMessage);
    if (button) flashCopied(button);
    return true;
  } catch {
    toast("复制失败，请稍后再试", "error");
    return false;
  }
}

function emptyState(title, message, actions = [], iconName = "stack") {
  const empty = element("div", "empty-state");
  if (iconName) {
    const badge = element("div", "empty-icon");
    badge.appendChild(icon(iconName));
    empty.appendChild(badge);
  }
  empty.append(element("b", "", title), element("span", "", message));
  const validActions = (Array.isArray(actions) ? actions : []).filter((a) => a && a.label && a.onClick);
  if (validActions.length) {
    const wrap = element("div", "empty-actions");
    validActions.forEach((act) => {
      const btn = element("button", `button ${act.primary ? "primary" : "quiet"}`, act.label);
      btn.type = "button";
      btn.addEventListener("click", act.onClick);
      wrap.appendChild(btn);
    });
    empty.appendChild(wrap);
  }
  return empty;
}

/* ---- Modal Stack, Focus Management & Escape Layering -------------------- */
// Ordered from topmost layer to bottommost layer so Escape & Tab trapping
// always act strictly on the highest visible modal.
const MODAL_ORDER = [
  () => els.cmdkModal,
  () => els.shortcutsModal,
  () => els.modal,
  () => els.adapterModal,
  () => els.detailModal,
];
const modalReturnFocus = new WeakMap();

function openModalsTopDown() {
  return MODAL_ORDER
    .map((get) => get())
    .filter((modal) => Boolean(modal && !modal.hidden && !modal.classList.contains("closing")));
}

function topOpenModal() {
  return openModalsTopDown()[0] || null;
}

function isAnyModalOpen() {
  return openModalsTopDown().length > 0;
}

function syncModalLayers() {
  const activeModals = openModalsTopDown();
  const topModal = activeModals[0] || null;
  const anyOpen = Boolean(topModal);

  [document.querySelector("header.top"), document.querySelector("aside.sidebar"), document.querySelector("main.view")]
    .filter(Boolean)
    .forEach((node) => {
      node.inert = anyOpen;
      if (anyOpen) node.setAttribute("inert", "");
      else node.removeAttribute("inert");
    });

  MODAL_ORDER.map((get) => get()).filter(Boolean).forEach((modal) => {
    const isTop = modal === topModal;
    const inert = !modal.hidden && !isTop;
    modal.inert = inert;
    if (inert) modal.setAttribute("inert", "");
    else modal.removeAttribute("inert");
  });

  document.body.classList.toggle("modal-open", anyOpen);
}

function showModal(modal, { rememberFocus = true } = {}) {
  if (!modal) return;
  closePopMenu();
  if (rememberFocus && modal.hidden && document.activeElement instanceof HTMLElement) {
    modalReturnFocus.set(modal, document.activeElement);
  }
  modal.classList.remove("closing");
  modal.hidden = false;
  syncModalLayers();
}

function hideModal(modal, { animate = false, onClosed } = {}) {
  if (!modal || modal.hidden) return;
  const restore = modalReturnFocus.get(modal);
  modalReturnFocus.delete(modal);

  const finalize = () => {
    modal.hidden = true;
    modal.classList.remove("closing");
    syncModalLayers();
    if (typeof onClosed === "function") onClosed();
    if (restore && restore.isConnected && typeof restore.focus === "function") {
      const top = topOpenModal();
      if (!top || top.contains(restore)) {
        requestAnimationFrame(() => restore.focus({ preventScroll: true }));
      }
    }
  };

  if (!animate || matchMedia("(prefers-reduced-motion: reduce)").matches) {
    finalize();
    return;
  }
  if (modal.classList.contains("closing")) return;
  modal.classList.add("closing");
  syncModalLayers();
  window.setTimeout(() => {
    if (modal.classList.contains("closing")) finalize();
  }, 200);
}

function trapModalTab(modal, event) {
  if (event.key !== "Tab" || topOpenModal() !== modal) return;
  const focusables = [...modal.querySelectorAll(
    'button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])',
  )].filter((node) => !node.hidden && node.offsetParent !== null);
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
}

/* ---- Popover Menus (.pop) ----------------------------------------------- */
let popMenu = null;

function closePopMenu({ restoreFocus = false } = {}) {
  const current = popMenu;
  if (!current) return;
  popMenu = null;
  current.anchor.setAttribute("aria-expanded", "false");
  current.pop.classList.add("leaving");
  const leaving = current.pop;
  window.setTimeout(() => leaving.remove(), 180);
  document.removeEventListener("pointerdown", current.onOutside, true);
  document.removeEventListener("scroll", current.onDismiss, true);
  window.removeEventListener("resize", current.onDismiss);
  if (restoreFocus) current.anchor.focus({ preventScroll: true });
}

function openPopMenu(anchor, config) {
  if (!anchor) return;
  if (popMenu && popMenu.anchor === anchor) {
    closePopMenu({ restoreFocus: true });
    return;
  }
  closePopMenu();

  const pop = element("div", "pop");
  pop.setAttribute("role", "listbox");
  pop.tabIndex = -1;
  if (config.label) pop.setAttribute("aria-label", config.label);
  if (config.singularProject) pop.dataset.singularProject = "true";

  let filter = null;
  if (config.searchable) {
    filter = element("input", "pop-search");
    filter.type = "search";
    filter.placeholder = config.placeholder || "搜索";
    filter.setAttribute("aria-label", config.placeholder || "搜索");
    filter.autocomplete = "off";
    filter.spellcheck = false;
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
    row.dataset.label = `${item.label} ${item.key || ""} ${item.meta ?? ""}`.trim();
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
  const visibleRows = () => rows.filter((row) => !row.hidden);

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

  pop.addEventListener("keydown", (event) => {
    if (isIMEComposing(event)) return;
    const shown = visibleRows();
    const index = shown.indexOf(active);
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (!shown.length) return;
      setActive(
        event.key === "ArrowDown"
          ? shown[Math.min(shown.length - 1, index + 1)]
          : shown[Math.max(0, index - 1)],
      );
    } else if (event.key === "Home" || event.key === "End") {
      event.preventDefault();
      if (!shown.length) return;
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
    bindSearchInput(filter, {
      debounceMs: 0,
      onCommit: (raw) => {
        const needle = String(raw || "").trim().toLowerCase();
        let hits = 0;
        rows.forEach((row) => {
          const hit = !needle || row.dataset.label.toLowerCase().includes(needle);
          row.hidden = !hit;
          if (hit) hits += 1;
        });
        empty.hidden = hits > 0;
        list.scrollTop = 0;
        setActive(visibleRows()[0] || null);
      },
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
  pop.style.minWidth = `${Math.round(Math.max(rect.width, 184))}px`;
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
  window.setTimeout(() => rows.forEach((row) => row.classList.add("settled")), 280);

  anchor.setAttribute("aria-expanded", "true");
  popMenu = { anchor, pop, onOutside, onDismiss };
  setActive(active);
  if (filter) filter.focus();
  else pop.focus();
}

/* ---- Sidebar Width, Scrollbars, Highlight & Torch ----------------------- */
const SIDE_WIDTH_KEY = "marktoast-side-w";
const SIDE_MIN = 168;
const SIDE_MAX = 320;
let sideWidthValue = 0;
try {
  sideWidthValue = Math.round(Number(localStorage.getItem(SIDE_WIDTH_KEY) || 0)) || 0;
} catch {}

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
  } catch {}
  applySideWidth();
}

function toggleSidebarCollapsed() {
  $("side-toggle")?.click();
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
    const thumbHeight = Math.max(24, Math.round((height * height) / total));
    const y = Math.round(((height - thumbHeight) * box.scrollTop) / (total - height));
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
    const row = event.target?.closest?.(".side-row") || null;
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

function positionSideHighlight() {
  const highlight = $("side-hl");
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
  const topToggle = $("side-toggle-top");
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

/* ---- Segmented Control Thumbs (ResizeObserver-Synced) ------------------- */
function positionSetThumb(box, { immediate = false } = {}) {
  if (!box) return;
  const thumb = box.querySelector(":scope > .thumb");
  const active = box.querySelector(":scope > .opt.on");
  if (!thumb) return;
  if (!active || !box.offsetParent || !active.offsetWidth) {
    thumb.style.opacity = "0";
    delete thumb.dataset.placed;
    return;
  }
  const snap = immediate || !thumb.dataset.placed;
  if (snap) thumb.classList.add("still");
  thumb.style.opacity = "1";
  thumb.style.width = `${active.offsetWidth}px`;
  thumb.style.height = `${active.offsetHeight}px`;
  thumb.style.transform = `translate(${active.offsetLeft - 2}px, 0)`;
  if (snap) {
    void thumb.offsetWidth;
    thumb.dataset.placed = "1";
    thumb.classList.remove("still");
  }
}

function positionSetThumbs(opts) {
  document.querySelectorAll("#view-settings .segs").forEach((box) => positionSetThumb(box, opts));
}

function positionSegThumb() {
  const seg = $("main-nav");
  const thumb = $("seg-thumb");
  if (!seg || !thumb) return;
  const active = seg.querySelector("button.on");
  if (!active || seg.offsetWidth === 0 || active.offsetWidth === 0) {
    thumb.style.opacity = "0";
    delete thumb.dataset.placed;
    return;
  }
  const first = !thumb.dataset.placed;
  if (first) thumb.classList.add("still");
  thumb.style.opacity = "1";
  thumb.style.transform = `translateX(${active.offsetLeft}px)`;
  thumb.style.width = `${active.offsetWidth}px`;
  if (first) {
    void thumb.offsetWidth;
    thumb.dataset.placed = "1";
    thumb.classList.remove("still");
  }
}

function initSegmentedObservers() {
  const segObserver = new ResizeObserver(() => {
    positionSetThumbs({ immediate: true });
    positionSegThumb();
  });
  document.querySelectorAll("#view-settings .segs, #view-settings .segs .opt, #main-nav, #main-nav button").forEach((el) => {
    segObserver.observe(el);
  });
}

/* ---- Custom Tooltip & Floating Scroll Capsule --------------------------- */
const TIP_KEYCAPS = {
  "open-cmdk": isMac ? "⌘K" : "Ctrl K",
  "search-toggle": "/",
  "open-settings": isMac ? "⌘," : "Ctrl ,",
  "open-shortcuts": "?",
  "side-open-shortcuts": "?",
  "side-toggle": isMac ? "⌃⌘S" : "Ctrl+Alt+S",
  "side-toggle-top": isMac ? "⌃⌘S" : "Ctrl+Alt+S",
};
let tipBox = null;
let tipTimer = 0;
let tipOwner = null;
let tipShownAt = 0;

function tooltipButton(node) {
  const button = node?.closest?.("button") || null;
  if (!button || button.disabled) return null;
  if (!button.querySelector(".ic") && button.id !== "side-open-shortcuts") return null;
  if (button.id !== "side-open-shortcuts" && button.textContent.trim()) return null;
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
  if (!button.dataset.tipTitle && button.getAttribute("title")) {
    button.dataset.tipTitle = button.getAttribute("title");
    button.removeAttribute("title");
  }
  tipOwner = button;
  clear(tipBox);
  tipBox.appendChild(element("span", "", label));
  const cap = TIP_KEYCAPS[button.id] || button.dataset.keycap;
  if (cap) {
    const kbd = element("kbd", "", cap);
    kbd.dataset.noI18n = "true";
    tipBox.appendChild(kbd);
  }
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
    else tipTimer = window.setTimeout(() => showTip(button), 420);
  });
  document.addEventListener("pointerout", (event) => {
    const button = tooltipButton(event.target);
    if (!button || (event.relatedTarget && button.contains(event.relatedTarget))) return;
    hideTip();
  });
  document.addEventListener("focusin", (event) => {
    const button = tooltipButton(event.target);
    if (button && button.matches(":focus-visible")) showTip(button);
  });
  document.addEventListener("focusout", hideTip);
  document.addEventListener("pointerdown", hideTip, true);
  document.addEventListener("keydown", hideTip, true);
  document.addEventListener("scroll", hideTip, true);
  window.addEventListener("blur", hideTip);
}

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
  document.addEventListener("scroll", paint, true);
  window.addEventListener("resize", paint);
  return paint;
}

/* ---- Status, Aliases & Projects ----------------------------------------- */
const TYPE_LABELS = { user: "用户", feedback: "反馈", project: "项目", reference: "参考", decision: "决策" };
const VIEW_LABELS = { memories: "全部记忆", inbox: "收件箱", adapters: "适配器", history: "历史", settings: "设置" };

function typeLabel(type) {
  return TYPE_LABELS[type] || type || "记忆";
}

function projectLabel(key) {
  const value = String(key || "");
  if (state.aliases && state.aliases[value]) return state.aliases[value];
  const match = value.match(/^(.*)-[0-9a-f]{16}$/);
  return match ? match[1] : value;
}

function commitLabel(subject) {
  const value = String(subject || "");
  const mem = value.match(/^mem\(([^)]+)\):\s*(.*)$/);
  if (mem) {
    const project = projectLabel(mem[1]);
    const action = mem[2]
      .replace(/^add\s+/, "新增 ")
      .replace(/^save\s+/, "保存 ")
      .replace(/^delete\s+/, "删除 ")
      .replace(/^remove\s+/, "删除 ")
      .replace(/^reindex$/, "重建索引");
    return `${project} · ${action}`;
  }
  const restore = value.match(/^restore\(([^)]+)\):\s*(.*)$/);
  if (restore) return `${projectLabel(restore[1])} · 恢复 ${restore[2]}`;
  return value;
}

async function loadAliases() {
  try {
    const data = await api("/api/aliases");
    state.aliases = data?.aliases || {};
  } catch {
    state.aliases = state.aliases || {};
  }
  return state.aliases;
}

async function loadStatus({ announce = false } = {}) {
  if (!state.status) setHubState("");
  try {
    const [data] = await Promise.all([api("/api/status"), loadAliases()]);
    state.status = data;
    state.root = data.root || "";
    hideHubAlert();
    setHubState("online");
    $("all-count").textContent = String(data.facts ?? 0);
    $("inbox-count").textContent = Number(data.inbox) ? String(data.inbox) : "";
    const sideAdapterCount = $("side-adapter-count");
    if (sideAdapterCount && Array.isArray(data.adapters) && data.adapters.length) {
      sideAdapterCount.textContent = String(data.adapters.length);
    }
    $("repo-count").textContent = `${Number(data.facts) || 0} 条`;
    $("repo-count").title = `${Number(data.facts) || 0} 条记忆`;
    $("repo-status").title = data.root || "";
    renderProjects(data.projects || []);
    renderProjectOptions(data.projects || []);
    requestAnimationFrame(() => {
      positionSideHighlight();
      positionSideBars();
    });
    if (announce) toast("已连接到本地 hub");
    return data;
  } catch (error) {
    if (error.name === "AbortError") return null;
    state.status = null;
    setHubState("offline");
    showHubAlert(formatError(error));
    $("repo-count").textContent = "";
    renderProjects([]);
    renderProjectOptions([]);
    if (state.view === "memories" && !state.results.length) renderMemoryError(error);
    if (announce) toast(formatError(error), "error");
    return null;
  }
}

function sortedProjects(projects) {
  return (projects || [])
    .slice()
    .sort((a, b) => (Number(b.facts) || 0) - (Number(a.facts) || 0) || projectLabel(a.key).localeCompare(projectLabel(b.key), "zh-CN"));
}

function renderProjects(projects) {
  clear(els.projectList);
  const total = $("project-total");
  if (total) total.textContent = projects.length ? String(projects.length) : "";
  if (!projects.length) {
    els.projectList.appendChild(element("div", "side-label", state.status ? "暂无项目" : "等待本地 hub"));
    return;
  }
  sortedProjects(projects).forEach((project) => {
    const active = state.view === "memories" && state.project === project.key;
    const button = element("button", `side-row project-row${active ? " is-active" : ""}`);
    button.type = "button";
    button.dataset.project = project.key;
    button.dataset.noI18n = "true";
    button.title = project.key;
    button.setAttribute("aria-pressed", String(active));
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

  const datalist = $("memory-project-options");
  if (datalist) {
    clear(datalist);
    const globalOption = document.createElement("option");
    globalOption.value = "global";
    globalOption.textContent = "global";
    datalist.appendChild(globalOption);
  }

  const inboxSelect = els.inboxProjectSelect;
  const currentInboxProj = state.inboxTargetProject || "global";
  if (inboxSelect) {
    clear(inboxSelect);
    const globalOpt = element("option", "", "global");
    globalOpt.value = "global";
    globalOpt.dataset.noI18n = "true";
    inboxSelect.appendChild(globalOpt);
  }

  sortedProjects(projects).forEach((project) => {
    const option = element("option", "", projectLabel(project.key));
    option.value = project.key;
    option.dataset.noI18n = "true";
    els.projectFilter.appendChild(option);

    if (datalist && project.key !== "global") {
      const suggestion = document.createElement("option");
      suggestion.value = project.key;
      suggestion.textContent = projectLabel(project.key);
      datalist.appendChild(suggestion);
    }

    if (inboxSelect && project.key !== "global") {
      const opt = element("option", "", projectLabel(project.key));
      opt.value = project.key;
      opt.dataset.noI18n = "true";
      inboxSelect.appendChild(opt);
    }
  });

  els.projectFilter.value = projects.some((p) => p.key === current) ? current : "";
  if (inboxSelect) {
    inboxSelect.value = [...inboxSelect.options].some((o) => o.value === currentInboxProj) ? currentInboxProj : "global";
  }
  syncFilterLabels();
}

/* ---- Filters, Search State & Breadcrumbs -------------------------------- */
function syncSearchInputUI() {
  const hasVal = Boolean(els.search.value.trim());
  els.searchClear.hidden = !hasVal;
  els.searchShell.classList.toggle("has-value", hasVal);
}

function setFilterLabel(node, text, raw = false) {
  if (!node) return;
  if (raw) node.dataset.noI18n = "true";
  else delete node.dataset.noI18n;
  node.textContent = text;
}

function hasActiveMemoryFilters() {
  return Boolean(state.query || state.project || state.type);
}

function syncFilterLabels() {
  const projectButton = $("project-filter-button");
  const typeButton = $("type-filter-button");
  if (projectButton) {
    setFilterLabel($("project-filter-label"), state.project ? projectLabel(state.project) : "全部项目", Boolean(state.project));
    projectButton.classList.toggle("on", Boolean(state.project));
    projectButton.title = state.project || "";
  }
  if (typeButton) {
    setFilterLabel($("type-filter-label"), state.type ? typeLabel(state.type) : "全部类型");
    typeButton.classList.toggle("on", Boolean(state.type));
    typeButton.title = state.type ? typeLabel(state.type) : "";
  }
  if (els.filterReset) {
    els.filterReset.hidden = !hasActiveMemoryFilters();
  }
}

function updateBreadcrumb() {
  const crumb = $("breadcrumb-current");
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

function openProjectMenu() {
  const items = [{ value: "", label: "全部项目", selected: !state.project }];
  sortedProjects(state.status?.projects || []).forEach((project) => {
    items.push({
      value: project.key,
      key: project.key,
      label: projectLabel(project.key),
      meta: project.facts ?? 0,
      raw: true,
      selected: state.project === project.key,
    });
  });
  openPopMenu($("project-filter-button"), {
    label: "项目筛选",
    searchable: true,
    placeholder: "搜索项目",
    emptyText: "没有匹配的项目",
    items,
    onPick: (value) => {
      if (value === (state.project || "")) return;
      cancelToolbarSearch();
      state.project = value;
      els.projectFilter.value = value;
      renderProjects(state.status?.projects || []);
      $("all-memories")?.classList.toggle("is-active", !value);
      requestAnimationFrame(positionSideHighlight);
      syncFilterLabels();
      loadMemories({ preserveSelection: true });
    },
  });
}

function openTypeMenu() {
  const items = [{ value: "", label: "全部类型", selected: !state.type }];
  Object.keys(TYPE_LABELS).forEach((type) => {
    items.push({ value: type, label: typeLabel(type), selected: state.type === type });
  });
  openPopMenu($("type-filter-button"), {
    label: "类型筛选",
    singularProject: true,
    items,
    onPick: (value) => {
      if (value === (state.type || "")) return;
      cancelToolbarSearch();
      state.type = value;
      els.typeFilter.value = value;
      syncFilterLabels();
      loadMemories({ preserveSelection: true });
    },
  });
}

function openSortMenu() {
  const items = [];
  if (state.query) items.push({ value: "relevance", label: "相关度", selected: state.sort === "relevance" });
  items.push({
    value: "updated",
    label: "最近更新",
    selected: state.sort === "updated" || (!state.query && state.sort === "relevance"),
  });
  items.push({ value: "title", label: "标题", selected: state.sort === "title" });

  openPopMenu($("sort-button"), {
    label: "排序",
    items,
    onPick: (value) => {
      if (value === state.sort) return;
      cancelToolbarSearch();
      state.sort = value;
      updateHeading();
      loadMemories({ preserveSelection: true });
    },
  });
}

function selectProject(project) {
  cancelToolbarSearch();
  state.project = project;
  state.query = "";
  els.search.value = "";
  syncSearchInputUI();
  els.projectFilter.value = project;
  syncFilterLabels();
  $("all-memories")?.classList.toggle("is-active", !project);
  switchView("memories", { autoLoadMemories: false });
  renderProjects(state.status?.projects || []);
  requestAnimationFrame(positionSideHighlight);
  loadMemories({ preserveSelection: true });
}

function clearFilters() {
  cancelToolbarSearch();
  state.query = "";
  state.project = "";
  state.type = "";
  state.selectedPath = "";
  state.detailLoading = false;
  els.search.value = "";
  syncSearchInputUI();
  els.projectFilter.value = "";
  els.typeFilter.value = "";
  syncFilterLabels();
  renderProjects(state.status?.projects || []);
  $("all-memories")?.classList.add("is-active");
  requestAnimationFrame(positionSideHighlight);
  loadMemories({ preserveSelection: false });
}

/* ---- Memories Wall & Pagination ----------------------------------------- */
function renderMemoryLoading(count = 5) {
  clear(els.resultRegion);
  const list = element("div", "list");
  for (let index = 0; index < count; index += 1) {
    const row = element("div", "row loading-row");
    const who = element("div", "who");
    who.append(element("span", "skeleton title"), element("span", "skeleton line"));
    row.append(icon("file"), who);
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

async function loadMemories({ preserveSelection = true, silent = false, append = false } = {}) {
  const slot = beginRequestSlot("memories");
  updateHeading();
  const limit = currentPageLimit();
  const offset = append ? state.offset : 0;

  state.loadMoreError = null;
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
  if (state.project) params.set("project", state.project);
  if (state.type) params.set("type", state.type);

  let endpoint;
  if (state.query) {
    params.set("q", state.query);
    endpoint = `/api/search?${params.toString()}`;
  } else {
    endpoint = `/api/memories?${params.toString()}`;
  }

  try {
    const data = await api(endpoint, { signal: slot.controller.signal });
    if (!isCurrentSlot("memories", slot)) return;

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
    if (!preserveSelection || (!selectedCanStay && !memoryDetailOpen())) {
      state.selectedPath = "";
      state.detailLoading = false;
      if (!preserveSelection) hideMemoryDetail();
    }
    state.loadingMore = false;
    renderMemories();
  } catch (error) {
    if (error.name === "AbortError" || !isCurrentSlot("memories", slot)) return;
    state.loadingMore = false;
    if (append) {
      // Prevent infinite rAF scroll retry storms when pagination fails.
      state.loadMoreError = error;
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
    if (isCurrentSlot("memories", slot)) {
      state.loadingMore = false;
      els.resultRegion.setAttribute("aria-busy", "false");
    }
  }
}

let autoLoadFrame = 0;

function maybeLoadMore() {
  autoLoadFrame = 0;
  if (state.view !== "memories" || !state.hasMore || state.loadingMore || state.loadMoreError) return;
  if (!state.results.length) return;
  const viewport = document.querySelector(".view");
  if (!viewport) return;
  const rest = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight;
  if (rest < viewport.clientHeight * 1.2) {
    loadMemories({ preserveSelection: true, silent: true, append: true });
  }
}

function queueLoadMoreCheck() {
  if (autoLoadFrame || state.loadMoreError) return;
  autoLoadFrame = requestAnimationFrame(maybeLoadMore);
}

function appendLoadMoreHint() {
  if (!state.hasMore && !state.loadingMore && !state.loadMoreError) return;
  const hint = element("div", "load-more-hint");
  if (state.loadMoreError) {
    hint.appendChild(element("span", "", "加载更多失败"));
    const retry = element("button", "button quiet compact", "点击重试");
    retry.type = "button";
    retry.addEventListener("click", () => {
      state.loadMoreError = null;
      loadMemories({ preserveSelection: true, silent: true, append: true });
    });
    hint.appendChild(retry);
    els.resultRegion.appendChild(hint);
    return;
  }
  if (state.loadingMore) {
    hint.textContent = "正在加载…";
  }
  hint.setAttribute("aria-hidden", "true");
  els.resultRegion.appendChild(hint);
  queueLoadMoreCheck();
}

function updateFilterSummary() {
  if (!els.filterSummary) return;
  const count = state.totalKnown ? state.total : state.results.length;
  if (!state.results.length && !hasActiveMemoryFilters()) {
    els.filterSummary.textContent = "";
    return;
  }
  if (state.query) {
    els.filterSummary.textContent = `匹配 ${count} 条`;
  } else if (state.hasMore && state.totalKnown) {
    els.filterSummary.textContent = `显示 ${state.results.length} / ${state.total} 条`;
  } else {
    els.filterSummary.textContent = `共 ${count} 条`;
  }
}

function renderMemories() {
  const newFrom = state.newFrom || 0;
  state.newFrom = 0;
  clear(els.resultRegion);
  const totalCount = state.totalKnown ? state.total : state.results.length;
  els.resultTotal.textContent = String(totalCount);
  updateFilterSummary();

  if (!state.selectedPath && !memoryDetailOpen()) hideMemoryDetail();

  if (!state.results.length) {
    const list = element("div", "list");
    if (hasActiveMemoryFilters()) {
      list.appendChild(emptyState(
        "没有匹配的记忆",
        "换个关键词或调整筛选条件试试。",
        [
          { label: "清除筛选条件", primary: true, onClick: clearFilters },
          { label: "新建记忆", primary: false, onClick: () => openDialog() },
        ],
        "search",
      ));
    } else {
      list.appendChild(emptyState(
        "还没有记忆",
        "在这里沉淀偏好、架构决策与项目上下文，接入的 Agent 可随时按需检索。",
        [
          { label: "新建第一条记忆", primary: true, onClick: () => openDialog() },
          { label: "配置适配器", primary: false, onClick: () => switchView("adapters") },
        ],
        "stack",
      ));
    }
    els.resultRegion.appendChild(list);
    return;
  }

  const grid = element("div", "memory-grid");
  state.results.forEach((result, index) => {
    const open = result.path === state.selectedPath;
    const type = result.type || "reference";
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

    const title = element("div", "card-title");
    title.dataset.noI18n = "true";
    appendHighlighted(title, result.name || fileName(result.path), state.query);
    card.appendChild(title);

    const snippet = element("div", "card-snippet");
    snippet.dataset.noI18n = "true";
    renderCardPreview(snippet, result, state.query);
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

function cleanSnippet(snippet) {
  return String(snippet || "").replace(/\s+/g, " ").trim();
}

function cardPreviewLine(line) {
  let text = String(line || "").replace(/^\s{0,3}#{1,6}\s+/, "").trim();
  if (!text) return "";
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
  return text
    .replace(/^\s{0,3}>\s?/, "")
    .replace(/^\s*(?:[-*+])\s+/, "· ")
    .replace(/^\s*(\d+)[.)]\s+/, "$1. ");
}

function renderCardPreview(container, result, query = "") {
  const source = String(
    (query && cleanSnippet(result.snippet)) ||
    result.preview ||
    result.description ||
    cleanSnippet(result.snippet) ||
    shortPath(result.path) ||
    "",
  );
  const text = source
    .split("\n")
    .map(cardPreviewLine)
    .filter((line) => line.trim())
    .join(" ")
    .replace(/\s{2,}/g, " ")
    .trim();
  const excerpt = safeSlice(text, CARD_PREVIEW_CHARS);
  renderInline(container, excerpt, query);
}

function renderMemoryError(error) {
  clear(els.resultRegion);
  els.resultTotal.textContent = "不可用";
  if (els.filterSummary) els.filterSummary.textContent = "";
  const list = element("div", "list");
  list.appendChild(emptyState(
    state.status ? "记忆加载失败" : "本地 hub 未连接",
    formatError(error),
    [{ label: "重新连接", primary: true, onClick: retryAll }],
    "warning",
  ));
  els.resultRegion.appendChild(list);
}

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

function findCardElement(path) {
  if (!path) return null;
  return [...document.querySelectorAll(".memory-card")].find((node) => node.dataset.path === path) || null;
}

/* ---- Memory Detail Reading Sheet, Edit, Delete & History ---------------- */
function memoryDetailOpen() {
  return Boolean(els.detailModal && !els.detailModal.hidden && !els.detailModal.classList.contains("closing"));
}

function hideMemoryDetail({ animate = false } = {}) {
  abortRequestSlot("detail");
  state.detailLoading = false;
  if (els.detailModal && !els.detailModal.hidden) {
    hideModal(els.detailModal, {
      animate,
      onClosed: () => requestAnimationFrame(() => window.paintScrollEnds?.()),
    });
  } else {
    requestAnimationFrame(() => window.paintScrollEnds?.());
  }
}

function openMemoryDetail() {
  if (!els.detailModal) return;
  showModal(els.detailModal);
  renderMemoryDetail();
  requestAnimationFrame(() => {
    ($("detail-dialog-box") || $("close-detail"))?.focus({ preventScroll: true });
  });
}

function closeMemoryDetail() {
  const path = state.selectedPath;
  state.selectedPath = "";
  hideMemoryDetail({ animate: true });
  if (state.view === "memories") renderMemories();
  if (path && state.view === "memories") {
    requestAnimationFrame(() => {
      findCardElement(path)?.focus({ preventScroll: true });
    });
  }
}

function parseRawFrontmatter(raw) {
  const text = String(raw || "").replace(/^\ufeff/, "");
  if (!text.startsWith("---\n") && !text.startsWith("---\r\n")) return {};
  const rest = text.slice(4);
  const end = rest.indexOf("\n---");
  if (end < 0) return {};
  const out = {};
  rest.slice(0, end).split(/\r?\n/).forEach((line) => {
    const m = line.match(/^\s*([A-Za-z0-9_]+)\s*:\s*(.+?)\s*$/);
    if (!m) return;
    const key = m[1];
    if (key === "metadata") return;
    out[key] = m[2].replace(/^['"]|['"]$/g, "");
  });
  return out;
}

function adjacentMemoryPath(delta) {
  const index = state.results.findIndex((item) => item.path === state.selectedPath);
  if (index < 0) return "";
  const next = state.results[index + delta];
  return next?.path || "";
}

function renderMemoryDetail() {
  if (!els.detailModal || els.detailModal.hidden) return;
  const path = state.selectedPath;
  const result = state.results.find((item) => item.path === path) || state.details.get(path);
  if (!result && !state.detailLoading && !state.detailErrors.has(path)) {
    hideMemoryDetail();
    return;
  }
  clear(els.detailContent);
  els.detailContent.appendChild(renderExpandedMemory(result || { path }));
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

function renderExpandedMemory(result) {
  const box = element("div", "memory-document");
  const path = result.path;

  if (state.detailLoading && !state.details.has(path)) {
    box.classList.add("loading");
    const loading = element("div", "detail-loading");
    loading.append(element("span", "skeleton"), element("span", "skeleton"), element("span", "skeleton"));
    box.appendChild(loading);
    return box;
  }

  if (state.detailErrors.has(path)) {
    const error = element("div", "expanded-error");
    const retry = element("button", "text-button", "重试");
    retry.type = "button";
    retry.addEventListener("click", () => selectMemory(path, true));
    error.append(icon("warning"), element("span", "", formatError(state.detailErrors.get(path))), retry);
    box.appendChild(error);
    return box;
  }

  const detail = state.details.get(path);
  if (!detail) {
    box.classList.add("loading");
    box.appendChild(element("div", "detail-loading"));
    return box;
  }

  const fullPath = detail.path || path;
  const type = detail.type || result.type || "reference";
  const projKey = detail.project || result.project || "global";
  const factTitle = detail.name || result.name || fileName(fullPath);
  const fmExtra = parseRawFrontmatter(detail.raw);

  const header = element("header", "doc-head");
  const meta = element("div", "doc-meta");

  const typePill = element("span", `pill type-pill type-${type}`, typeLabel(type));
  typePill.title = type;
  meta.appendChild(typePill);

  if (projKey) {
    const projBtn = element("button", "doc-project-btn");
    projBtn.type = "button";
    projBtn.dataset.noI18n = "true";
    projBtn.title = projKey;
    projBtn.setAttribute("aria-label", "按此项目筛选");
    projBtn.append(icon("folder"), element("span", "", projectLabel(projKey)));
    projBtn.addEventListener("click", () => {
      closeMemoryDetail();
      selectProject(projKey);
    });
    meta.appendChild(projBtn);
  }

  const timeNode = element("span", "doc-time", relativeDate(detail.updated || result.updated));
  timeNode.title = formatDate(detail.updated || result.updated, true);
  meta.appendChild(timeNode);

  const actions = element("div", "doc-actions");

  // Prev / Next navigation within current list
  if (state.results.length > 1 && state.results.some((item) => item.path === fullPath)) {
    const prevPath = adjacentMemoryPath(-1);
    const nextPath = adjacentMemoryPath(1);
    const prevBtn = element("button", "icon");
    prevBtn.type = "button";
    prevBtn.disabled = !prevPath;
    prevBtn.setAttribute("aria-label", "上一条记忆");
    prevBtn.dataset.keycap = "K";
    prevBtn.appendChild(icon("chevron-left"));
    prevBtn.addEventListener("click", () => prevPath && selectMemory(prevPath));

    const nextBtn = element("button", "icon");
    nextBtn.type = "button";
    nextBtn.disabled = !nextPath;
    nextBtn.setAttribute("aria-label", "下一条记忆");
    nextBtn.dataset.keycap = "J";
    nextBtn.appendChild(icon("chevron-right"));
    nextBtn.addEventListener("click", () => nextPath && selectMemory(nextPath));

    actions.append(prevBtn, nextBtn, element("span", "doc-nav-sep"));
  }

  // Rendered vs Raw Markdown switch
  const viewSegs = element("div", "mini-segs");
  viewSegs.setAttribute("role", "group");
  viewSegs.setAttribute("aria-label", "切换阅读与源码视图");
  const btnRead = element("button", `mini-opt${state.detailViewMode === "rendered" ? " on" : ""}`, "阅读");
  btnRead.type = "button";
  btnRead.setAttribute("aria-pressed", String(state.detailViewMode === "rendered"));
  btnRead.addEventListener("click", () => {
    if (state.detailViewMode === "rendered") return;
    state.detailViewMode = "rendered";
    renderMemoryDetail();
  });
  const btnRaw = element("button", `mini-opt${state.detailViewMode === "raw" ? " on" : ""}`, "源码");
  btnRaw.type = "button";
  btnRaw.setAttribute("aria-pressed", String(state.detailViewMode === "raw"));
  btnRaw.addEventListener("click", () => {
    if (state.detailViewMode === "raw") return;
    state.detailViewMode = "raw";
    renderMemoryDetail();
  });
  viewSegs.append(btnRead, btnRaw);
  actions.append(viewSegs, element("span", "doc-nav-sep"));

  // Edit button
  const editBtn = element("button", "icon");
  editBtn.id = "detail-edit";
  editBtn.type = "button";
  editBtn.setAttribute("aria-label", "编辑这条记忆");
  editBtn.dataset.keycap = "E";
  editBtn.appendChild(icon("edit"));
  editBtn.addEventListener("click", () => openEditDialog(detail));

  // History button
  const historyBtn = element("button", `icon${state.memoryHistory.open && state.memoryHistory.path === fullPath ? " on" : ""}`);
  historyBtn.id = "detail-history";
  historyBtn.type = "button";
  historyBtn.setAttribute("aria-label", "查看这条记忆的版本历史");
  historyBtn.appendChild(icon("branch"));
  historyBtn.addEventListener("click", () => toggleMemoryHistory(fullPath));

  // Copy Markdown button
  const copyMdBtn = element("button", "icon");
  copyMdBtn.id = "detail-copy-md";
  copyMdBtn.type = "button";
  copyMdBtn.setAttribute("aria-label", "复制这条记忆的 Markdown");
  copyMdBtn.appendChild(icon("file"));
  copyMdBtn.addEventListener("click", () => copyMarkdown(fullPath, copyMdBtn));

  // Copy Path button
  const copyPathBtn = element("button", "icon");
  copyPathBtn.id = "detail-copy-path";
  copyPathBtn.type = "button";
  copyPathBtn.setAttribute("aria-label", "复制记忆路径");
  copyPathBtn.appendChild(icon("copy"));
  copyPathBtn.addEventListener("click", () => writeClipboard(fullPath, "路径已复制到剪贴板", copyPathBtn));

  // Delete button
  const deleteBtn = element("button", "icon danger-hover");
  deleteBtn.id = "detail-delete";
  deleteBtn.type = "button";
  deleteBtn.setAttribute("aria-label", "删除这条记忆");
  deleteBtn.appendChild(icon("trash"));
  deleteBtn.addEventListener("click", () => deleteMemory(detail, deleteBtn));

  actions.append(editBtn, historyBtn, copyMdBtn, copyPathBtn, deleteBtn);
  meta.appendChild(actions);
  header.appendChild(meta);

  // Title + Copy Name button
  const titleRow = element("div", "doc-title-row");
  const title = element("h1", "doc-title");
  title.id = "detail-title";
  title.dataset.noI18n = "true";
  title.title = fullPath;
  appendHighlighted(title, factTitle, state.query);
  const copyNameBtn = element("button", "icon doc-copy-name");
  copyNameBtn.type = "button";
  copyNameBtn.setAttribute("aria-label", "复制名称");
  copyNameBtn.appendChild(icon("copy"));
  copyNameBtn.addEventListener("click", () => writeClipboard(factTitle, "名称已复制", copyNameBtn));
  titleRow.append(title, copyNameBtn);
  header.appendChild(titleRow);

  const description = String(detail.description || result.description || "").trim();
  if (description) {
    const lead = element("p", "doc-description");
    lead.dataset.noI18n = "true";
    appendHighlighted(lead, description, state.query);
    header.appendChild(lead);
  }

  // Frontmatter Metadata Bar
  const fmBar = element("div", "doc-frontmatter-bar");
  const pathItem = element("span", "fm-item");
  pathItem.appendChild(element("b", "", "路径"));
  const pathCopy = element("button", "fm-path-btn");
  pathCopy.type = "button";
  pathCopy.title = fullPath;
  pathCopy.setAttribute("aria-label", "复制记忆路径");
  const pathCode = element("code", "", shortPath(fullPath));
  pathCode.dataset.noI18n = "true";
  pathCopy.append(pathCode, icon("copy"));
  pathCopy.addEventListener("click", () => writeClipboard(fullPath, "路径已复制到剪贴板", pathCopy));
  pathItem.appendChild(pathCopy);
  fmBar.appendChild(pathItem);

  const updatedExact = formatDate(detail.updated || result.updated, true);
  if (updatedExact && updatedExact !== "—") {
    const updItem = element("span", "fm-item");
    const updCode = element("code", "", updatedExact);
    updCode.dataset.noI18n = "true";
    updItem.append(element("b", "", "更新"), updCode);
    fmBar.appendChild(updItem);
  }

  const chars = runeCount(detail.body || "");
  const lenItem = element("span", "fm-item");
  const lenSpan = element("span", "", `${chars} 字符`);
  lenItem.append(element("b", "", "字数"), lenSpan);
  fmBar.appendChild(lenItem);

  if (fmExtra.originSessionId) {
    const sessItem = element("span", "fm-item");
    const sessCode = element("code", "", safeSlice(fmExtra.originSessionId, 16));
    sessCode.dataset.noI18n = "true";
    sessCode.title = fmExtra.originSessionId;
    sessItem.append(element("b", "", "会话"), sessCode);
    fmBar.appendChild(sessItem);
  }

  header.appendChild(fmBar);
  header.appendChild(element("div", "doc-rule"));
  box.appendChild(header);

  const fullBody = String(detail.body || "");
  if (state.detailViewMode === "raw") {
    const rawWrap = element("div", "raw-source-box");
    const rawHead = element("div", "raw-source-head");
    const rawLabel = element("span", "", shortPath(fullPath));
    rawLabel.dataset.noI18n = "true";
    const copyRawBtn = element("button", "button quiet compact", "复制 Markdown");
    copyRawBtn.type = "button";
    copyRawBtn.addEventListener("click", () => writeClipboard(detail.raw || fullBody, "Markdown 已复制到剪贴板", copyRawBtn));
    rawHead.append(rawLabel, copyRawBtn);
    const rawPre = element("pre", "raw-source-pre", safeSlice(String(detail.raw || fullBody), MAX_RENDERED_BODY_CHARS));
    rawPre.dataset.noI18n = "true";
    rawWrap.append(rawHead, rawPre);
    box.appendChild(rawWrap);
  } else {
    const body = element("div", "markdown-body doc-body");
    renderMarkdown(body, safeSlice(fullBody, MAX_RENDERED_BODY_CHARS), state.query);
    if (!fullBody.trim()) {
      body.appendChild(element("p", "doc-empty", "没有正文。"));
    } else if (fullBody.length > MAX_RENDERED_BODY_CHARS) {
      body.appendChild(element("p", "body-truncation-note", `正文较长，仅显示前 ${MAX_RENDERED_BODY_CHARS.toLocaleString("en-US")} 个字符。`));
    }
    box.appendChild(body);
  }

  if (state.memoryHistory.open && state.memoryHistory.path === fullPath) {
    box.appendChild(renderMemoryHistory(fullPath));
  }
  return box;
}

async function copyMarkdown(path, button) {
  const detail = state.details.get(path);
  if (!detail || !(detail.raw || detail.body)) {
    toast("这条记忆还没加载完", "error");
    return;
  }
  await writeClipboard(String(detail.raw || detail.body), "Markdown 已复制到剪贴板", button);
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
  if (state.selectedPath !== path) {
    state.detailViewMode = "rendered";
    if (state.memoryHistory.path !== path) {
      state.memoryHistory.open = false;
    }
  }
  state.selectedPath = path;
  state.detailErrors.delete(path);
  state.detailLoading = force || !state.details.has(path);
  if (state.view === "memories") renderMemories();
  openMemoryDetail();
  if (state.details.has(path) && !force) {
    state.detailLoading = false;
    renderMemoryDetail();
    return;
  }

  const slot = beginRequestSlot("detail");
  try {
    const detail = await api(`/api/memory?path=${encodeURIComponent(path)}`, {
      signal: slot.controller.signal,
    });
    if (!isCurrentSlot("detail", slot)) return;
    cacheMemoryDetail(path, detail);
    state.detailErrors.delete(path);
    if (state.selectedPath === path) {
      state.detailLoading = false;
      if (state.view === "memories") renderMemories();
      else renderMemoryDetail();
    }
  } catch (error) {
    if (error.name === "AbortError" || !isCurrentSlot("detail", slot)) return;
    if (state.selectedPath === path) {
      state.detailLoading = false;
      state.detailErrors.set(path, error);
      if (state.view === "memories") renderMemories();
      else renderMemoryDetail();
    }
  }
}

async function deleteMemory(detail, button) {
  const path = detail?.path || state.selectedPath;
  if (!path) return;
  const title = detail?.name || fileName(path);
  if (!window.confirm(localized(`删除记忆「${title}」？删除后可通过 Git 历史找回。`))) return;

  if (button) button.disabled = true;
  try {
    const result = await api(`/api/memory?path=${encodeURIComponent(path)}`, {
      method: "DELETE",
      body: JSON.stringify({ path }),
    });
    state.details.delete(path);
    state.detailErrors.delete(path);
    state.selectedPath = "";
    hideMemoryDetail({ animate: true });
    toastDataResult(result, "记忆已删除");
    await loadStatus();
    if (state.view === "memories") await loadMemories({ preserveSelection: false, silent: true });
    else if (state.view === "history") await loadHistory();
  } catch (error) {
    if (error.status === 405) {
      toast("当前本地 hub 版本暂未启用删除接口。", "error");
    } else {
      toast(formatError(error), "error");
    }
  } finally {
    if (button && button.isConnected) button.disabled = false;
  }
}

/* ---- Safe Dependency-Free Markdown Renderer ----------------------------- */
const MD_FENCE_RE = /^\s{0,3}(`{3,}|~{3,})\s*([^`\s]*)\s*$/;
const MD_HEADING_RE = /^\s{0,3}(#{1,6})\s+(.+?)\s*#*\s*$/;
const MD_RULE_RE = /^\s{0,3}(?:(\*\s*){3,}|(-\s*){3,}|(_\s*){3,})$/;
const MD_QUOTE_RE = /^\s{0,3}>/;
const MD_INLINE_SOURCE = "(`[^`\\n]+`|\\*\\*[^*\\n]+\\*\\*|~~[^~\\n]+~~|!\\[[^\\]\\n]*\\]\\([^)\\n]+\\)|\\[[^\\]\\n]+\\]\\([^)\\n]+\\)|\\*[^*\\s][^*\\n]*\\*|<(?:https?:\\/\\/|mailto:)[^<>\\s]+>)";

function renderMarkdown(container, markdown, highlightQuery = "") {
  renderMarkdownBlocks(container, normalizeMarkdown(markdown), highlightQuery);
}

function normalizeMarkdown(value) {
  const lines = String(value || "").replace(/\r\n?/g, "\n").replace(/\t/g, "    ").split("\n");
  while (lines.length && !lines[lines.length - 1].trim()) lines.pop();
  return lines;
}

function renderMarkdownBlocks(parent, lines, highlightQuery = "") {
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
      const lang = fence[2].replace(/[^\w+-]/g, "") || "";
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
      renderInline(node, heading[2], highlightQuery);
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
      i = renderMarkdownTable(parent, lines, i, highlightQuery);
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
      const quote = element("blockquote");
      renderMarkdownBlocks(quote, quoted, highlightQuery);
      parent.appendChild(quote);
      continue;
    }

    if (listMarker(line)) {
      i = renderMarkdownList(parent, lines, i, highlightQuery);
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
    appendParagraphContent(node, paragraph, highlightQuery);
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

function renderMarkdownList(parent, lines, start, highlightQuery = "") {
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
      renderInline(item, task[2], highlightQuery);
    } else {
      renderInline(item, marker.content, highlightQuery);
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
      renderMarkdownBlocks(item, dedentLines(continuation), highlightQuery);
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

function isCJK(ch) {
  return /[\u3000-\u303f\u3400-\u4dbf\u4e00-\u9fff\uff00-\uffef]/.test(ch);
}

function softBreakJoiner(previous, next) {
  const last = String(previous || "").trim().slice(-1);
  const first = String(next || "").trim().charAt(0);
  if (isCJK(last) && isCJK(first)) return "";
  return " ";
}

function appendParagraphContent(parent, lines, highlightQuery = "") {
  lines.forEach((line, index) => {
    const hardBreak = /\s{2,}$/.test(line) || line.endsWith("\\");
    renderInline(parent, line.replace(/\\$/, "").replace(/\s+$/, ""), highlightQuery);
    if (hardBreak) parent.appendChild(document.createElement("br"));
    else if (index < lines.length - 1) {
      const join = softBreakJoiner(lines[index], lines[index + 1]);
      if (join) parent.appendChild(document.createTextNode(join));
    }
  });
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

function renderMarkdownTable(parent, lines, start, highlightQuery = "") {
  const header = splitTableRow(lines[start]);
  const alignments = splitTableRow(lines[start + 1]).map(tableAlignment);
  const table = element("table", "md-table");
  const head = document.createElement("thead");
  const headRow = document.createElement("tr");
  header.forEach((cell, index) => {
    const th = document.createElement("th");
    if (alignments[index]) th.style.textAlign = alignments[index];
    renderInline(th, cell, highlightQuery);
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
      renderInline(td, cells[column] || "", highlightQuery);
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
  const head = element("div", "md-code-head");
  const langSpan = element("span", "md-code-lang", lang || "code");
  langSpan.dataset.noI18n = "true";
  const copyBtn = element("button", "icon md-code-copy");
  copyBtn.type = "button";
  copyBtn.setAttribute("aria-label", "复制代码");
  copyBtn.appendChild(icon("copy"));
  copyBtn.addEventListener("click", () => writeClipboard(code, "代码已复制", copyBtn));
  head.append(langSpan, copyBtn);
  wrap.appendChild(head);

  const pre = document.createElement("pre");
  const node = document.createElement("code");
  if (lang) node.className = `language-${lang}`;
  node.textContent = code;
  pre.appendChild(node);
  wrap.appendChild(pre);
  return wrap;
}

function renderInline(parent, source, highlightQuery = "") {
  const text = String(source ?? "");
  const pattern = new RegExp(MD_INLINE_SOURCE, "g");
  let cursor = 0;
  let match;
  while ((match = pattern.exec(text)) !== null) {
    if (match.index > cursor) {
      appendHighlighted(parent, text.slice(cursor, match.index), highlightQuery);
    }
    renderInlineToken(parent, match[0], highlightQuery);
    cursor = pattern.lastIndex;
  }
  if (cursor < text.length) {
    appendHighlighted(parent, text.slice(cursor), highlightQuery);
  }
}

function safeUrl(raw) {
  const trimmed = String(raw || "").trim();
  if (!trimmed || /^[\u0000-\u001f]/.test(trimmed)) return "";
  if (/^(javascript|data|vbscript|file):/i.test(trimmed)) return "";
  try {
    const url = new URL(trimmed);
    if (url.protocol === "http:" || url.protocol === "https:" || url.protocol === "mailto:") {
      return url.href;
    }
  } catch {}
  return "";
}

function renderInlineToken(parent, token, highlightQuery = "") {
  if (token.startsWith("`")) {
    const code = element("code");
    appendHighlighted(code, token.slice(1, -1), highlightQuery);
    parent.appendChild(code);
    return;
  }
  if (token.startsWith("![")) {
    const image = token.match(/^!\[([^\]]*)\]\(([^)]+)\)$/);
    parent.appendChild(document.createTextNode(image && image[1] ? image[1] : localized("图片")));
    return;
  }
  if (token.startsWith("[")) {
    const link = token.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
    if (!link) {
      appendHighlighted(parent, token, highlightQuery);
      return;
    }
    const href = safeUrl(link[2]);
    if (!href) {
      renderInline(parent, link[1], highlightQuery);
      return;
    }
    const anchor = document.createElement("a");
    anchor.href = href;
    anchor.target = "_blank";
    anchor.rel = "noreferrer noopener";
    renderInline(anchor, link[1], highlightQuery);
    parent.appendChild(anchor);
    return;
  }
  if (token.startsWith("**")) {
    const strong = element("strong");
    renderInline(strong, token.slice(2, -2), highlightQuery);
    parent.appendChild(strong);
    return;
  }
  if (token.startsWith("~~")) {
    const strike = element("del");
    renderInline(strike, token.slice(2, -2), highlightQuery);
    parent.appendChild(strike);
    return;
  }
  if (token[0] === "*") {
    const emphasis = element("em");
    renderInline(emphasis, token.slice(1, -1), highlightQuery);
    parent.appendChild(emphasis);
    return;
  }
  if (token.startsWith("<")) {
    const url = token.slice(1, -1);
    const href = safeUrl(url);
    if (!href) {
      appendHighlighted(parent, token, highlightQuery);
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
  appendHighlighted(parent, token, highlightQuery);
}

/* ---- Create / Edit Memory Modal & Dirty-State Protection ---------------- */
function currentFormValues() {
  return {
    project: $("memory-project").value.trim(),
    type: $("memory-type").value,
    name: $("memory-name").value.trim(),
    description: $("memory-description").value.trim(),
    body: $("memory-body").value,
  };
}

function isFormDirty() {
  if (!state.formInitial || els.modal.hidden) return false;
  const cur = currentFormValues();
  return (
    cur.project !== state.formInitial.project ||
    cur.type !== state.formInitial.type ||
    cur.name !== state.formInitial.name ||
    cur.description !== state.formInitial.description ||
    cur.body !== state.formInitial.body
  );
}

function syncFormDirtyUI() {
  const pill = $("dialog-dirty-pill");
  if (pill) pill.hidden = !isFormDirty();
  const countNode = $("body-char-count");
  if (countNode) {
    const runes = runeCount($("memory-body").value);
    countNode.textContent = `${runes.toLocaleString("en-US")} / 2 MiB`;
  }
}

function setFormBodyMode(mode) {
  state.formBodyMode = mode === "preview" ? "preview" : "write";
  const writeBtn = $("body-mode-write");
  const previewBtn = $("body-mode-preview");
  const textarea = $("memory-body");
  const preview = $("memory-preview");
  const isPreview = state.formBodyMode === "preview";

  writeBtn?.classList.toggle("on", !isPreview);
  writeBtn?.setAttribute("aria-pressed", String(!isPreview));
  previewBtn?.classList.toggle("on", isPreview);
  previewBtn?.setAttribute("aria-pressed", String(isPreview));

  if (textarea) textarea.hidden = isPreview;
  if (preview) {
    preview.hidden = !isPreview;
    if (isPreview) {
      clear(preview);
      const raw = textarea?.value || "";
      if (!raw.trim()) preview.appendChild(element("p", "doc-empty", "暂无可预览的正文"));
      else renderMarkdown(preview, safeSlice(raw, MAX_RENDERED_BODY_CHARS));
    }
  }
}

function openDialog() {
  state.editingMemory = null;
  clear(els.formError);
  els.formError.hidden = true;
  $("dialog-title").textContent = "新建记忆";
  $("memory-project").value = state.project || "global";
  $("memory-type").value = state.type || "project";
  $("memory-name").value = "";
  $("memory-description").value = "";
  $("memory-body").value = "";
  state.formInitial = currentFormValues();
  setFormBodyMode("write");
  syncFormDirtyUI();
  showModal(els.modal);
  window.setTimeout(() => $("memory-name").focus(), 25);
}

function openEditDialog(detail) {
  if (!detail) return;
  state.editingMemory = {
    path: detail.path || state.selectedPath,
    project: detail.project || "global",
    name: detail.name || fileName(detail.path),
    description: detail.description || "",
    type: detail.type || "project",
    body: detail.body || "",
  };
  clear(els.formError);
  els.formError.hidden = true;
  $("dialog-title").textContent = "编辑记忆";
  $("memory-project").value = state.editingMemory.project;
  $("memory-type").value = TYPE_LABELS[state.editingMemory.type] ? state.editingMemory.type : "project";
  $("memory-name").value = state.editingMemory.name;
  $("memory-description").value = state.editingMemory.description;
  $("memory-body").value = state.editingMemory.body;
  state.formInitial = currentFormValues();
  setFormBodyMode("write");
  syncFormDirtyUI();
  showModal(els.modal);
  window.setTimeout(() => $("memory-body").focus(), 25);
}

function closeDialog({ force = false } = {}) {
  if (els.modal.hidden || els.modal.classList.contains("closing")) return false;
  if (!force && isFormDirty()) {
    if (!window.confirm(localized("放弃未保存的修改？"))) return false;
  }
  state.formInitial = null;
  state.editingMemory = null;
  hideModal(els.modal, { animate: true });
  return true;
}

function validateMemoryForm(payload) {
  if (!payload.name) return "名称不能为空。";
  if (runeCount(payload.name) > MAX_FACT_NAME_RUNES) return "名称不能超过 200 个字符。";
  if (runeCount(payload.description) > MAX_FACT_DESC_RUNES) return "描述不能超过 2,000 个字符。";
  if (!payload.body.trim()) return "正文不能为空。";
  if (utf8ByteLength(payload.body) > MAX_FACT_BODY_BYTES) return "正文大小不能超过 2 MiB。";
  return "";
}

async function submitMemory(event) {
  event.preventDefault();
  if (state.formBodyMode === "preview") setFormBodyMode("write");
  if (!els.form.reportValidity()) return;

  const values = currentFormValues();
  const payload = {
    project: values.project || "global",
    name: values.name,
    description: values.description,
    type: values.type,
    body: values.body,
  };
  const validationError = validateMemoryForm(payload);
  if (validationError) {
    els.formError.textContent = validationError;
    els.formError.hidden = false;
    els.formError.scrollIntoView({ block: "nearest" });
    return;
  }

  const editing = state.editingMemory;
  const isEdit = Boolean(editing?.path);
  const renamedOrMoved = isEdit && (editing.project !== payload.project || editing.name !== payload.name);
  const requestBody = renamedOrMoved
    ? { ...payload, previousPath: editing.path }
    : payload;

  els.formError.hidden = true;
  setButtonLoading(els.submit, true, "正在保存…");

  try {
    let result;
    try {
      result = await api("/api/memory", {
        method: "POST",
        body: JSON.stringify(requestBody),
      });
    } catch (err) {
      // Fallback if a strict older hub rejects unknown previousPath field.
      if (renamedOrMoved && err.status === 400 && /unknown field/i.test(err.detail || err.message)) {
        result = await api("/api/memory", {
          method: "POST",
          body: JSON.stringify(payload),
        });
      } else {
        throw err;
      }
    }

    if (editing?.path) state.details.delete(editing.path);
    if (result?.path) state.details.delete(result.path);

    closeDialog({ force: true });
    toastDataResult(result, isEdit ? "记忆已更新" : "记忆已保存");
    await loadStatus();

    state.project = payload.project;
    state.type = "";
    state.query = "";
    els.typeFilter.value = "";
    els.search.value = "";
    syncSearchInputUI();
    els.projectFilter.value = payload.project;
    switchView("memories", { autoLoadMemories: false });
    renderProjects(state.status?.projects || []);
    requestAnimationFrame(positionSideHighlight);
    await loadMemories({ preserveSelection: false });
    if (result?.path) {
      await selectMemory(result.path, true);
      flash(findCardElement(result.path));
    }
  } catch (error) {
    els.formError.textContent = formatError(error, "保存失败，请检查表单和本地 hub。");
    els.formError.hidden = false;
    els.formError.scrollIntoView({ block: "nearest" });
  } finally {
    setButtonLoading(els.submit, false);
  }
}

/* ---- Per-Memory Version History & Syntax Diff --------------------------- */
function renderDiffLines(container, patch) {
  const lines = String(patch || "").replace(/\r\n?/g, "\n").split("\n");
  if (!lines.length || (lines.length === 1 && !lines[0])) {
    container.appendChild(element("div", "diff-line is-meta", localized("没有差异内容")));
    return;
  }
  lines.forEach((line) => {
    let cls = "diff-line";
    if (line.startsWith("@@")) cls += " is-hunk";
    else if (line.startsWith("+++") || line.startsWith("---") || line.startsWith("diff ") || line.startsWith("index ")) {
      cls += " is-meta";
    } else if (line.startsWith("+")) {
      cls += " is-add";
    } else if (line.startsWith("-")) {
      cls += " is-del";
    }
    const row = element("div", cls, line || " ");
    row.dataset.noI18n = "true";
    container.appendChild(row);
  });
}

function renderMemoryHistory(path) {
  const history = state.memoryHistory;
  const box = element("div", "memory-history");
  const head = element("div", "memory-history-head");
  head.appendChild(element("h3", "", "版本历史"));
  box.appendChild(head);

  if (history.loading) {
    const loading = element("div", "detail-loading");
    loading.append(element("span", "skeleton"), element("span", "skeleton"));
    box.appendChild(loading);
    return box;
  }
  if (history.error) {
    const error = element("div", "expanded-error");
    const retry = element("button", "text-button", "重试");
    retry.type = "button";
    retry.addEventListener("click", () => toggleMemoryHistory(path, true));
    error.append(icon("warning"), element("span", "", formatError(history.error)), retry);
    box.appendChild(error);
    return box;
  }
  if (!history.commits.length) {
    box.appendChild(element("p", "doc-empty", "还没有提交记录"));
    return box;
  }

  const list = element("div", "memory-commits");
  history.commits.forEach((commit) => {
    const selected = commit.hash === history.selectedHash;
    const row = element("button", `memory-commit${selected ? " on" : ""}`);
    row.type = "button";
    row.setAttribute("aria-pressed", String(selected));
    const hashCode = element("code", "", String(commit.hash || "").slice(0, 8));
    hashCode.dataset.noI18n = "true";
    const subj = element("span", "", commitLabel(commit.subject || "无提交说明"));
    const time = element("time", "", commit.date || "");
    time.dataset.noI18n = "true";
    row.append(hashCode, subj, time);
    row.addEventListener("click", () => loadMemoryDiff(path, commit.hash));
    list.appendChild(row);
  });
  box.appendChild(list);

  if (history.patchLoading) {
    const loading = element("div", "detail-loading");
    loading.append(element("span", "skeleton"), element("span", "skeleton"));
    box.appendChild(loading);
  } else if (history.patchError) {
    const err = element("div", "expanded-error");
    err.append(icon("warning"), element("span", "", formatError(history.patchError)));
    box.appendChild(err);
  } else if (history.selectedHash) {
    const panel = element("div", "diff-panel");
    const diffHead = element("div", "diff-head");
    const hashLabel = element("span", "diff-head-title", String(history.selectedHash).slice(0, 10));
    hashLabel.dataset.noI18n = "true";
    const diffActions = element("div", "diff-actions");
    if (history.patch) {
      const copyBtn = element("button", "button quiet compact", "复制补丁");
      copyBtn.type = "button";
      copyBtn.addEventListener("click", () => writeClipboard(history.patch, "补丁已复制", copyBtn));
      diffActions.appendChild(copyBtn);
    }
    const restore = element("button", "button primary compact", history.restoring ? "恢复中…" : "恢复此版本");
    restore.type = "button";
    restore.disabled = history.restoring;
    restore.addEventListener("click", () => restoreMemoryVersion(path, history.selectedHash));
    diffActions.appendChild(restore);
    diffHead.append(hashLabel, diffActions);
    panel.appendChild(diffHead);

    const diffBox = element("div", "diff-box");
    renderDiffLines(diffBox, history.patch);
    panel.appendChild(diffBox);
    box.appendChild(panel);
  }
  return box;
}

async function toggleMemoryHistory(path, force = false) {
  const history = state.memoryHistory;
  if (!force && history.open && history.path === path && !history.loading) {
    history.open = false;
    abortRequestSlot("memoryHistory");
    abortRequestSlot("memoryDiff");
    renderMemoryDetail();
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
  renderMemoryDetail();

  const slot = beginRequestSlot("memoryHistory");
  try {
    const data = await api(`/api/memory/history?path=${encodeURIComponent(path)}`, {
      signal: slot.controller.signal,
    });
    if (!isCurrentSlot("memoryHistory", slot) || history.path !== path) return;
    history.commits = Array.isArray(data?.commits) ? data.commits : [];
    history.loading = false;
    renderMemoryDetail();
    if (history.commits.length) {
      await loadMemoryDiff(path, history.commits[0].hash);
    }
  } catch (error) {
    if (error.name === "AbortError" || !isCurrentSlot("memoryHistory", slot) || history.path !== path) return;
    history.loading = false;
    history.error = error;
    renderMemoryDetail();
  }
}

async function loadMemoryDiff(path, hash) {
  const history = state.memoryHistory;
  Object.assign(history, { selectedHash: hash, patch: "", patchError: null, patchLoading: true });
  renderMemoryDetail();

  const slot = beginRequestSlot("memoryDiff");
  try {
    const data = await api(`/api/memory/diff?path=${encodeURIComponent(path)}&hash=${encodeURIComponent(hash)}`, {
      signal: slot.controller.signal,
    });
    if (!isCurrentSlot("memoryDiff", slot) || history.selectedHash !== hash) return;
    history.patch = data?.patch || "";
  } catch (error) {
    if (error.name === "AbortError" || !isCurrentSlot("memoryDiff", slot) || history.selectedHash !== hash) return;
    history.patchError = error;
  }
  history.patchLoading = false;
  renderMemoryDetail();
}

async function restoreMemoryVersion(path, hash) {
  if (!hash) return;
  if (!window.confirm(tr("恢复这个版本？会生成一次新的提交。"))) return;
  const history = state.memoryHistory;
  history.restoring = true;
  renderMemoryDetail();
  try {
    const result = await api("/api/memory/restore", {
      method: "POST",
      body: JSON.stringify({ path, hash }),
    });
    state.details.delete(path);
    toastDataResult(result, "已恢复");
    flash(findCardElement(path));
    await selectMemory(path, true);
    const data = await api(`/api/memory/history?path=${encodeURIComponent(path)}`);
    history.commits = data?.commits || [];
    history.selectedHash = "";
    history.patch = "";
    history.patchError = null;
    await loadStatus();
    if (state.view === "memories") await loadMemories({ preserveSelection: true, silent: true });
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    history.restoring = false;
    renderMemoryDetail();
  }
}

/* ---- Inbox Candidate Review --------------------------------------------- */
async function loadInbox() {
  const region = $("inbox-results");
  clear(region);
  const loading = element("div", "list");
  loading.append(element("div", "row loading-row"), element("div", "row loading-row"));
  region.appendChild(loading);

  const slot = beginRequestSlot("inbox");
  try {
    const data = await api("/api/inbox", { signal: slot.controller.signal });
    if (!isCurrentSlot("inbox", slot)) return;
    state.inbox = Array.isArray(data?.candidates) ? data.candidates : [];
    renderInbox();
    return true;
  } catch (error) {
    if (error.name === "AbortError" || !isCurrentSlot("inbox", slot)) return false;
    clear(region);
    if (els.inboxToolbar) els.inboxToolbar.hidden = true;
    const list = element("div", "list");
    list.appendChild(emptyState("收件箱加载失败", formatError(error), [{ label: "重试", primary: true, onClick: loadInbox }], "warning"));
    region.appendChild(list);
    return false;
  }
}

function renderInbox() {
  const region = $("inbox-results");
  clear(region);
  $("inbox-total").textContent = String(state.inbox.length);

  if (!state.inbox.length) {
    if (els.inboxToolbar) els.inboxToolbar.hidden = true;
    const list = element("div", "list");
    list.appendChild(emptyState(
      "收件箱是空的",
      "Agent 会话结束或触发摄取后，待审阅的候选记忆会出现在这里。",
      [
        { label: "查看适配器", primary: false, onClick: () => switchView("adapters") },
        { label: "新建记忆", primary: false, onClick: () => openDialog() },
      ],
      "inbox",
    ));
    region.appendChild(list);
    return;
  }

  if (els.inboxToolbar) {
    els.inboxToolbar.hidden = false;
    if (els.inboxSummary) els.inboxSummary.textContent = `${state.inbox.length} 条待审阅`;
  }

  const list = element("div", "list");
  state.inbox.forEach((candidate) => {
    const entry = element("article", "inbox-entry");
    const row = element("div", "row inbox-row");
    row.appendChild(icon("inbox"));

    const who = element("div", "who");
    who.dataset.noI18n = "true";
    who.append(
      element("div", "name", candidate.name || "候选记忆"),
      element("div", "sub", candidate.description || ""),
    );

    const meta = element("div", "memory-meta");
    const agentPill = element("span", "pill", candidate.agent || "agent");
    agentPill.dataset.noI18n = "true";
    meta.append(agentPill, element("span", "memory-time", relativeDate(candidate.created)));

    const actions = element("div", "inbox-actions");
    const keep = element("button", "button primary compact", "");
    keep.type = "button";
    keep.appendChild(element("span", "", "保留"));
    keep.addEventListener("click", () => promoteCandidate(candidate, keep));

    const drop = element("button", "button quiet compact", "");
    drop.type = "button";
    drop.appendChild(element("span", "", "丢弃"));
    drop.addEventListener("click", () => dismissCandidate(candidate, drop));

    actions.append(keep, drop);
    row.append(who, meta, actions);
    entry.appendChild(row);
    list.appendChild(entry);
  });
  region.appendChild(list);
}

async function promoteCandidate(candidate, button) {
  setButtonLoading(button, true);
  try {
    const project = els.inboxProjectSelect?.value || state.inboxTargetProject || "global";
    const result = await api("/api/inbox/promote", {
      method: "POST",
      body: JSON.stringify({ path: candidate.path, project }),
    });
    toastDataResult(result, "已保留");
    await loadStatus();
    await loadInbox();
  } catch (error) {
    toast(formatError(error), "error");
    setButtonLoading(button, false);
  }
}

async function dismissCandidate(candidate, button) {
  if (!window.confirm(tr("丢弃这条候选记忆？"))) return;
  setButtonLoading(button, true);
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
    setButtonLoading(button, false);
  }
}

/* ---- Adapter Center (40 Harnesses), Filtering & Snippet Copy ------------ */
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

function adapterStagedReady(adapter) {
  return adapter?.staged_ready === true || Boolean(state.adapterLastReport.get(adapter?.id)?.staged_dir);
}

function adapterStagedDir(adapter) {
  const report = state.adapterLastReport.get(adapter?.id);
  return String(report?.staged_dir || report?.config || adapter?.staged_dir || "").trim();
}

function adapterIsConnected(adapter) {
  const mode = adapterMode(adapter);
  return (mode === "auto" && Boolean(adapter?.installed)) || (mode !== "auto" && adapterStagedReady(adapter));
}

function adapterState(adapter) {
  const mode = adapterMode(adapter);
  if (mode === "auto" && adapter?.installed) {
    return { key: "installed", label: "已接入", className: " is-on" };
  }
  if (adapterStagedReady(adapter)) {
    return { key: "staged", label: "已生成片段", className: " is-on" };
  }
  if (adapter?.detected) return { key: "detected", label: "已检测", className: " is-detected" };
  return { key: "missing", label: "未检测", className: "" };
}

function adapterCanInstall(adapter) {
  if (adapterMode(adapter) !== "auto" || !adapter?.detected) return false;
  if (typeof adapter?.installable === "boolean") return adapter.installable;
  return true;
}

function adapterCanStage(adapter) {
  const mode = adapterMode(adapter);
  if (mode === "auto") {
    return Boolean(adapter?.detected) && adapter?.installable === false;
  }
  const hasAnyConfig = Boolean(
    (Array.isArray(adapter?.hooks?.events) && adapter.hooks.events.length && adapter.hooks.target) ||
    (adapter?.mcp?.kind && String(adapter.mcp.kind).toLowerCase() !== "none"),
  );
  return hasAnyConfig;
}

function adapterMatchesFilter(adapter) {
  const mode = adapterMode(adapter);
  const f = state.adapterFilter;
  if (f === "detected" && !adapter.detected) return false;
  if (f === "connected" && !adapterIsConnected(adapter)) return false;
  if (ADAPTER_MODES.includes(f) && mode !== f) return false;

  const q = state.adapterQuery.trim().toLowerCase();
  if (!q) return true;
  const haystack = [
    adapter.title,
    adapter.id,
    mode,
    adapter.tier,
    adapter.hooks?.target,
    adapter.mcp?.target,
    adapter.detected_path,
    adapter.notes,
  ].filter(Boolean).join(" ").toLowerCase();
  return haystack.includes(q);
}

async function loadAdapters() {
  clear(els.adapterRegion);
  const loading = element("div", "list");
  loading.append(element("div", "row loading-row"), element("div", "row loading-row"), element("div", "row loading-row"));
  els.adapterRegion.appendChild(loading);

  const slot = beginRequestSlot("adapters");
  try {
    const data = await api("/api/adapters", { signal: slot.controller.signal });
    if (!isCurrentSlot("adapters", slot)) return false;
    state.adapters = Array.isArray(data?.adapters) ? data.adapters : [];
    renderAdapters();
    if (state.adapterInfo && els.adapterModal && !els.adapterModal.hidden) {
      const fresh = state.adapters.find((a) => a.id === state.adapterInfo.id);
      if (fresh) {
        state.adapterInfo = fresh;
        renderAdapterInfo(fresh);
        syncAdapterDialogAction(fresh);
      }
    }
    return true;
  } catch (error) {
    if (error.name === "AbortError" || !isCurrentSlot("adapters", slot)) return false;
    clear(els.adapterRegion);
    els.adapterCount.textContent = "不可用";
    const list = element("div", "list");
    list.appendChild(emptyState("适配器加载失败", formatError(error), [{ label: "重试", primary: true, onClick: loadAdapters }], "warning"));
    els.adapterRegion.appendChild(list);
    return false;
  }
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
    state.adapterLastReport.set(adapter.id, data || {});
    const stagedDir = String(data?.staged_dir || data?.config || data?.dir || "").trim();

    if (data?.downgraded || (!data?.installed && data?.staged)) {
      const reason = String(data?.reason || "").trim();
      if (stagedDir) {
        try {
          await navigator.clipboard.writeText(stagedDir);
        } catch {}
      }
      toast(
        `${title} · ${localized("已降级为片段安装")}${reason ? `（${reason}）` : ""}`,
        "warning",
      );
      await loadAdapters();
      const updated = state.adapters.find((a) => a.id === adapter.id) || adapter;
      openAdapterInfo(updated);
      return;
    }

    toast(`${title} · ${localized(data?.installed ? "已接入" : "配置已更新")}`);
    await loadAdapters();
    await loadStatus();
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    setButtonLoading(button, false);
  }
}

async function uninstallAdapter(adapter, button) {
  const title = adapter.title || adapter.id || "Adapter";
  if (!window.confirm(localized("移除这家 harness 中的 MarkToast 配置？会先备份原文件。"))) return;
  setButtonLoading(button, true);
  try {
    await api("/api/adapters/action", {
      method: "POST",
      body: JSON.stringify({ id: adapter.id, action: "uninstall" }),
      timeoutMs: 60_000,
    });
    state.adapterLastReport.delete(adapter.id);
    toast(`${title} · ${localized("已移除 MarkToast 配置")}`);
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
  let copied = false;
  try {
    const data = await api("/api/adapters/action", {
      method: "POST",
      body: JSON.stringify({ id: adapter.id, action: "stage" }),
      timeoutMs: 60_000,
    });
    state.adapterLastReport.set(adapter.id, data || {});
    const dir = String(data?.staged_dir || data?.config || data?.dir || "").trim();
    if (dir) {
      try {
        await navigator.clipboard.writeText(dir);
        copied = true;
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
    if (state.adapterInfo && els.adapterModal && !els.adapterModal.hidden) {
      syncAdapterDialogAction(state.adapterInfo);
    }
    if (copied && button && button.isConnected) flashCopied(button);
  }
}

function adapterActionButton(adapter, extraClass = "") {
  if (adapterCanInstall(adapter)) {
    const install = element("button", `button quiet adapter-action${extraClass}`);
    install.type = "button";
    install.appendChild(element("span", "", adapter.installed ? "修复" : "接入"));
    install.addEventListener("click", (event) => runAdapterAction(adapter, event.currentTarget));
    return install;
  }
  if (adapterCanStage(adapter)) {
    const stage = element("button", `button quiet adapter-action${extraClass}`);
    stage.type = "button";
    stage.title = "生成合并片段";
    stage.appendChild(element("span", "", "生成片段"));
    stage.addEventListener("click", (event) => stageAdapter(adapter, event.currentTarget));
    return stage;
  }
  return null;
}

function buildMcpSnippetPreview(adapter) {
  const mcp = adapter?.mcp || {};
  const kind = String(mcp.kind || "").toLowerCase();
  if (!kind || kind === "none") return "";
  const endpoint = `${window.location.origin}/mcp`;
  const target = String(mcp.target || "").toLowerCase();

  if (target.endsWith(".toml")) {
    if (kind.includes("stdio")) {
      return `[mcp_servers.marktoast]\ncommand = "marktoast"\nargs = ["mcp"]`;
    }
    return `[mcp_servers.marktoast]\nurl = "${endpoint}"\nbearer_token_env_var = "MARKTOAST_TOKEN"`;
  }
  if (target.endsWith(".yaml") || target.endsWith(".yml")) {
    return `extensions:\n  marktoast:\n    name: MarkToast\n    type: streamable_http\n    enabled: true\n    uri: "${endpoint}"`;
  }
  if (kind.includes("stdio")) {
    return JSON.stringify({
      mcpServers: {
        marktoast: { command: "marktoast", args: ["mcp"] },
      },
    }, null, 2);
  }
  const containerKey = mcp.shape === "context_servers" ? "context_servers" : mcp.shape === "mcp" ? "mcp" : "mcpServers";
  return JSON.stringify({
    [containerKey]: {
      marktoast: {
        type: "http",
        url: endpoint,
        headers: { Authorization: "Bearer <MARKTOAST_TOKEN>" },
      },
    },
  }, null, 2);
}

function renderSnippetCard(titleText, subText, codeText, copySuccessMsg) {
  const card = element("div", "snippet-card");
  const head = element("div", "snippet-head");
  const left = element("div");
  left.appendChild(element("span", "snippet-title", titleText));
  if (subText) {
    const sub = element("span", "snippet-sub", subText);
    sub.dataset.noI18n = "true";
    left.appendChild(sub);
  }
  const copyBtn = element("button", "button quiet compact", "复制");
  copyBtn.type = "button";
  copyBtn.addEventListener("click", () => writeClipboard(codeText, copySuccessMsg, copyBtn));
  head.append(left, copyBtn);

  const pre = element("pre", "snippet-pre", codeText);
  pre.dataset.noI18n = "true";
  card.append(head, pre);
  return card;
}

function adapterInfoRows(adapter) {
  const mode = adapterMode(adapter);
  const hooks = adapter.hooks || {};
  const mcp = adapter.mcp || {};
  const st = adapterState(adapter);
  const modeCombined = mode === "mcp_only" ? "仅 MCP" : `${ADAPTER_MODE_LABELS[mode]} · ${ADAPTER_MODE_GROUP_LABELS[mode]}`;

  const rows = [
    ["接入方式", modeCombined, false],
    ["层级", ADAPTER_TIER_LABELS[adapter.tier] || adapter.tier || "—", false],
    ["状态", st.label, false],
  ];
  const detectedPaths = adapter.detected_path || (Array.isArray(adapter.detect) ? adapter.detect.join(", ") : "");
  if (detectedPaths) rows.push(["检测路径", detectedPaths, true]);
  if (Array.isArray(hooks.events) && hooks.events.length) rows.push(["Hook 事件", hooks.events.join(" · "), true]);
  if (hooks.target) rows.push(["Hook 目标文件", hooks.target, true]);
  if (hooks.injection) rows.push(["注入方式", ADAPTER_INJECTION_LABELS[hooks.injection] || hooks.injection, true]);
  if (adapter.install_kind || hooks.install_kind) {
    rows.push(["安装类型", adapter.install_kind || hooks.install_kind, true]);
  }
  if (mcp.kind) rows.push(["MCP 方式", mcp.kind, true]);
  if (mcp.target) rows.push(["MCP 目标文件", mcp.target, true]);
  return rows;
}

function renderAdapterInfo(adapter) {
  if (!els.adapterDialogBody) return;
  const body = els.adapterDialogBody;
  clear(body);
  const mode = adapterMode(adapter);
  const st = adapterState(adapter);
  els.adapterDialogTitle.textContent = adapter.title || adapter.id || "Adapter";

  const intro = element("div", "adapter-info-intro");
  intro.append(
    element("span", `pill adapter-mode is-${mode.replace("_", "-")}`, ADAPTER_MODE_LABELS[mode]),
    element("span", `adapter-state${st.className}`, st.label),
    element("p", "adapter-info-hint", ADAPTER_MODE_HINTS[mode]),
  );
  body.appendChild(intro);

  const report = state.adapterLastReport.get(adapter.id);
  if (report?.reason) {
    const warn = element("p", "body-truncation-note", report.reason);
    warn.dataset.noI18n = "true";
    body.appendChild(warn);
  }

  const table = element("table", "frontmatter-table adapter-info-table");
  adapterInfoRows(adapter).forEach(([key, value, raw]) => {
    const trNode = element("tr");
    const td = element("td", "", value);
    if (raw) td.dataset.noI18n = "true";
    trNode.append(element("th", "", key), td);
    table.appendChild(trNode);
  });
  body.appendChild(table);

  const stagedDir = adapterStagedDir(adapter);
  if (stagedDir) {
    const files = Array.isArray(report?.files) && report.files.length ? `\n${report.files.join("\n")}` : "";
    body.appendChild(renderSnippetCard("片段目录", "", `${stagedDir}${files}`, "路径已复制到剪贴板"));
  }

  const cliCmd = `marktoast adapters install ${adapter.id} --apply`;
  body.appendChild(renderSnippetCard("CLI 命令", adapter.id, cliCmd, "命令已复制"));

  const mcpSnippet = buildMcpSnippetPreview(adapter);
  if (mcpSnippet) {
    body.appendChild(renderSnippetCard("MCP 配置示例", adapter.mcp?.target || "", mcpSnippet, "片段已复制"));
  }

  if (adapter.notes) {
    const notes = element("section", "adapter-info-notes");
    notes.appendChild(element("h3", "", "说明"));
    notes.appendChild(element("p", "", adapter.notes));
    body.appendChild(notes);
  }
}

function syncAdapterDialogAction(adapter) {
  const action = els.adapterDialogAction;
  const uninstall = els.adapterDialogUninstall;
  if (uninstall) {
    uninstall.hidden = !(adapterMode(adapter) === "auto" && adapter.installed);
  }
  if (!action) return;
  if (adapterCanInstall(adapter)) {
    action.hidden = false;
    action.textContent = adapter.installed ? "修复" : "接入";
    action.dataset.adapterAction = "install";
    return;
  }
  if (adapterCanStage(adapter)) {
    action.hidden = false;
    action.textContent = "生成片段";
    action.dataset.adapterAction = "stage";
    return;
  }
  action.hidden = true;
  action.removeAttribute("data-adapter-action");
}

function openAdapterInfo(adapter) {
  if (!els.adapterModal) return;
  state.adapterInfo = adapter;
  renderAdapterInfo(adapter);
  syncAdapterDialogAction(adapter);
  showModal(els.adapterModal);
  requestAnimationFrame(() => $("close-adapter-dialog")?.focus({ preventScroll: true }));
}

function closeAdapterInfo() {
  if (!els.adapterModal || els.adapterModal.hidden || els.adapterModal.classList.contains("closing")) return;
  state.adapterInfo = null;
  hideModal(els.adapterModal, { animate: true });
}

function renderAdapterRow(adapter, list) {
  const mode = adapterMode(adapter);
  const entry = element("article", "adapter-entry");
  entry.dataset.adapter = adapter.id || "";
  const row = element("div", "row adapter-row");
  row.appendChild(icon("plug", "ic adapter-icon"));

  const who = element("div", "who");
  who.dataset.noI18n = "true";
  const nameLine = element("div", "adapter-name-line");
  const nameEl = element("span", "name");
  appendHighlighted(nameEl, adapter.title || adapter.id || "Adapter", state.adapterQuery);
  const codeEl = element("code");
  appendHighlighted(codeEl, adapter.id || "", state.adapterQuery);
  nameLine.append(nameEl, codeEl);

  const targets = [...new Set([adapter.hooks?.target, adapter.mcp?.target].filter(Boolean))];
  const subEl = element("div", "sub");
  appendHighlighted(subEl, targets.join("  ·  ") || adapter.detected_path || "", state.adapterQuery);
  who.append(nameLine, subEl);
  row.appendChild(who);

  const st = adapterState(adapter);
  const stateNode = element("span", `adapter-state${st.className}`, st.label);
  if (adapter.detected_path) stateNode.title = adapter.detected_path;
  row.appendChild(stateNode);

  const modeBadge = element("span", `pill adapter-mode is-${mode.replace("_", "-")}`, ADAPTER_MODE_LABELS[mode]);
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
  const total = state.adapters.length;
  els.adapterCount.textContent = String(total);
  const sideAdapterCount = $("side-adapter-count");
  if (sideAdapterCount && total) sideAdapterCount.textContent = String(total);

  if (!total) {
    if (els.adapterStats) els.adapterStats.textContent = "";
    const list = element("div", "list");
    list.appendChild(emptyState("没有适配器", "本地 hub 没有报告可用的 Agent。", [], "plug"));
    els.adapterRegion.appendChild(list);
    return;
  }

  const connectedCount = state.adapters.filter(adapterIsConnected).length;
  const detectedCount = state.adapters.filter((a) => a.detected).length;
  const filtered = state.adapters.filter(adapterMatchesFilter);

  if (els.adapterStats) {
    if (state.adapterFilter !== "all" || state.adapterQuery.trim()) {
      els.adapterStats.textContent = `显示 ${filtered.length} / ${total} 个`;
    } else {
      els.adapterStats.textContent = `已接入 ${connectedCount} · 已检测 ${detectedCount} · 共 ${total} 个`;
    }
  }

  if (!filtered.length) {
    const list = element("div", "list");
    list.appendChild(emptyState(
      "没有匹配的适配器",
      "换个关键词或切换筛选条件试试。",
      [{ label: "清除筛选", primary: true, onClick: resetAdapterFilter }],
      "plug",
    ));
    els.adapterRegion.appendChild(list);
    return;
  }

  const groups = new Map(ADAPTER_MODES.map((mode) => [mode, []]));
  filtered
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

function resetAdapterFilter() {
  state.adapterQuery = "";
  state.adapterFilter = "all";
  if (els.adapterSearch) els.adapterSearch.value = "";
  if (els.adapterSearchClear) els.adapterSearchClear.hidden = true;
  document.querySelectorAll("[data-adapter-filter]").forEach((btn) => {
    const on = btn.dataset.adapterFilter === "all";
    btn.classList.toggle("on", on);
    btn.setAttribute("aria-pressed", String(on));
  });
  renderAdapters();
}

/* ---- Git History View --------------------------------------------------- */
async function loadHistory() {
  clear(els.historyRegion);
  const loading = element("div", "list");
  loading.append(element("div", "row loading-row"), element("div", "row loading-row"));
  els.historyRegion.appendChild(loading);

  if (!state.status) await loadStatus();
  const slot = beginRequestSlot("history");
  try {
    const data = await api("/api/git/log?n=40", { signal: slot.controller.signal });
    if (!isCurrentSlot("history", slot)) return false;
    state.history = Array.isArray(data?.commits) ? data.commits : [];
    renderHistory();
    return true;
  } catch (error) {
    if (error.name === "AbortError" || !isCurrentSlot("history", slot)) return false;
    clear(els.historyRegion);
    const list = element("div", "list");
    list.appendChild(emptyState("Git 历史不可用", formatError(error), [{ label: "重试", primary: true, onClick: loadHistory }], "warning"));
    els.historyRegion.appendChild(list);
    return false;
  }
}

function renderHistory() {
  clear(els.historyRegion);
  if (state.status?.git?.available === false) {
    const list = element("div", "list");
    list.appendChild(emptyState("Git 不可用", "当前仓库没有可读取的 Git 历史。", [], "branch"));
    els.historyRegion.appendChild(list);
    return;
  }
  if (!state.history.length) {
    const list = element("div", "list");
    list.appendChild(emptyState(
      "还没有提交",
      "每次保存或恢复记忆都会自动记录在本地 Git 历史中。",
      [{ label: "新建记忆", primary: true, onClick: () => openDialog() }],
      "branch",
    ));
    els.historyRegion.appendChild(list);
    return;
  }
  const list = element("div", "list");
  state.history.forEach((commit) => {
    const entry = element("article", "history-entry");
    const row = element("div", "row history-row");
    const fullHash = String(commit.hash || "");
    const hashBtn = element("button", "history-hash-btn", fullHash.slice(0, 8));
    hashBtn.type = "button";
    hashBtn.dataset.noI18n = "true";
    hashBtn.title = fullHash;
    hashBtn.setAttribute("aria-label", "复制提交哈希");
    hashBtn.addEventListener("click", () => writeClipboard(fullHash, "提交哈希已复制", hashBtn));

    const dateSpan = element("span", "history-date", commit.date || "");
    dateSpan.dataset.noI18n = "true";
    row.append(
      icon("branch"),
      hashBtn,
      element("span", "history-subject", commitLabel(commit.subject || "无提交说明")),
      dateSpan,
    );
    entry.appendChild(row);
    list.appendChild(entry);
  });
  els.historyRegion.appendChild(list);
}

/* ---- Settings View ------------------------------------------------------ */
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
  const openPage = $(`setPage-${target}`);
  if (openPage) {
    [...openPage.querySelectorAll(".row.pref")].forEach((row, index) => {
      row.style.setProperty("--i", String(Math.min(index, 10)));
    });
  }
  positionSetThumbs({ immediate: true });
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
  } else {
    positionSetThumbs({ immediate: true });
  }
}

function resetSettingsSearch() {
  const input = $("set-q");
  if (!input || !input.value) return;
  input.value = "";
  applySettingsSearch("");
}

async function loadSettings() {
  const slot = beginRequestSlot("settings");
  try {
    const data = await api("/api/settings", { signal: slot.controller.signal });
    if (!isCurrentSlot("settings", slot)) return false;
    state.settings = data;
    state.aliases = data?.aliases || state.aliases;
    renderSettings();
    return true;
  } catch (error) {
    if (error.name === "AbortError" || !isCurrentSlot("settings", slot)) return false;
    toast(formatError(error), "error");
    return false;
  }
}

function renderSettings() {
  const mcpUrlEl = $("settings-mcp-url");
  if (mcpUrlEl) mcpUrlEl.textContent = `${window.location.origin}/mcp`;

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
  positionSetThumbs({ immediate: true });
}

function renderAliasList(settings) {
  const list = $("alias-list");
  clear(list);
  const projects = sortedProjects(state.status?.projects || []);
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
    const keyNode = element("div", "name alias-key", project.key);
    keyNode.dataset.noI18n = "true";
    who.appendChild(keyNode);
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
    state.aliases = data?.aliases || {};
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
    const fresh = data?.facts || 0;
    const skipped = data?.skipped || 0;
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
    $("settings-backup").textContent = data?.path || "—";
    $("settings-backup").title = data?.path || "";
    toast("备份已创建");
    flash(button.closest(".row.pref"));
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
    toast(`索引已重建 · ${data?.facts || 0} 条`);
    flash(button.closest(".row.pref"));
    await loadSettings();
    await loadStatus();
    if (state.view === "memories") loadMemories({ preserveSelection: true });
  } catch (error) {
    toast(formatError(error), "error");
  } finally {
    button.disabled = false;
  }
}

/* ---- Cmd/Ctrl+K Command & Global Search Palette ------------------------- */
function cmdkOpen() {
  return Boolean(els.cmdkModal && !els.cmdkModal.hidden && !els.cmdkModal.classList.contains("closing"));
}

function openCmdk(initialQuery = "") {
  if (!els.cmdkModal) return;
  if (cmdkOpen()) {
    closeCmdk();
    return;
  }
  const input = $("cmdk-input");
  const clearBtn = $("cmdk-clear");
  state.cmdk.query = String(initialQuery || "");
  state.cmdk.memories = [];
  state.cmdk.loading = false;
  state.cmdk.activeIndex = 0;
  if (input) input.value = state.cmdk.query;
  if (clearBtn) clearBtn.hidden = !state.cmdk.query;

  showModal(els.cmdkModal);
  renderCmdkResults();
  if (state.cmdk.query) runCmdkSearch(state.cmdk.query);
  requestAnimationFrame(() => {
    input?.focus();
    input?.select();
  });
}

function closeCmdk() {
  if (!els.cmdkModal || els.cmdkModal.hidden) return;
  abortRequestSlot("cmdk");
  hideModal(els.cmdkModal, { animate: true });
}

function builtinCommands() {
  const nextLang = window.MarkToastI18n?.language === "en" ? "zh" : "en";
  return [
    {
      id: "new-memory",
      icon: "plus",
      label: "新建记忆",
      keywords: "new create add memory write 新建 记忆 添加",
      kbd: "N",
      run: () => {
        closeCmdk();
        openDialog();
      },
    },
    {
      id: "go-memories",
      icon: "stack",
      label: "前往全部记忆",
      keywords: "memories library all 记忆 全部",
      run: () => {
        closeCmdk();
        selectProject("");
      },
    },
    {
      id: "go-inbox",
      icon: "inbox",
      label: "前往收件箱",
      keywords: "inbox candidate review 收件箱 候选",
      run: () => {
        closeCmdk();
        switchView("inbox");
      },
    },
    {
      id: "go-adapters",
      icon: "plug",
      label: "前往适配器中心",
      keywords: "adapters harness mcp hooks claude codex cursor grok 适配器 接入",
      run: () => {
        closeCmdk();
        switchView("adapters");
      },
    },
    {
      id: "go-history",
      icon: "branch",
      label: "前往 Git 历史",
      keywords: "history git log commits 历史 提交",
      run: () => {
        closeCmdk();
        switchView("history");
      },
    },
    {
      id: "go-settings",
      icon: "settings",
      label: "打开偏好设置",
      keywords: "settings preferences config token Settings 设置 偏好",
      kbd: isMac ? "⌘," : "Ctrl ,",
      run: () => {
        closeCmdk();
        switchView("settings");
      },
    },
    {
      id: "theme-light",
      icon: "sun",
      label: "切换到浅色外观",
      keywords: "theme light appearance 浅色 亮色 主题 外观",
      run: () => {
        closeCmdk();
        setTheme("light");
      },
    },
    {
      id: "theme-dark",
      icon: "moon",
      label: "切换到深色外观",
      keywords: "theme dark appearance 深色 暗色 主题 外观",
      run: () => {
        closeCmdk();
        setTheme("dark");
      },
    },
    {
      id: "toggle-lang",
      icon: "code",
      label: nextLang === "en" ? "切换界面语言为 English" : "切换界面语言为 中文",
      keywords: "language english chinese zh en 语言 中文 英文",
      run: () => {
        closeCmdk();
        window.MarkToastI18n?.setLanguage(nextLang);
      },
    },
    {
      id: "reindex",
      icon: "refresh",
      label: "重建全文搜索索引",
      keywords: "reindex fts sqlite index 重建 索引",
      run: () => {
        closeCmdk();
        rebuildIndex();
      },
    },
    {
      id: "shortcuts",
      icon: "help",
      label: "显示快捷键速查",
      keywords: "shortcuts help keys keyboard 快捷键 帮助",
      kbd: "?",
      run: () => {
        closeCmdk();
        openShortcutsModal();
      },
    },
  ];
}

async function runCmdkSearch(rawQuery) {
  const query = String(rawQuery || "").trim();
  state.cmdk.query = query;
  $("cmdk-clear").hidden = !query;
  if (!query) {
    abortRequestSlot("cmdk");
    state.cmdk.memories = [];
    state.cmdk.loading = false;
    state.cmdk.activeIndex = 0;
    renderCmdkResults();
    return;
  }

  state.cmdk.loading = true;
  renderCmdkResults();
  const slot = beginRequestSlot("cmdk");
  try {
    const data = await api(`/api/search?q=${encodeURIComponent(query)}&limit=12`, {
      signal: slot.controller.signal,
    });
    if (!isCurrentSlot("cmdk", slot)) return;
    state.cmdk.memories = Array.isArray(data?.results) ? data.results : [];
    state.cmdk.loading = false;
    state.cmdk.activeIndex = 0;
    renderCmdkResults();
  } catch (error) {
    if (error.name === "AbortError" || !isCurrentSlot("cmdk", slot)) return;
    state.cmdk.loading = false;
    renderCmdkResults();
  }
}

function applyQueryToWall(query) {
  const trimmed = String(query || "").trim();
  closeCmdk();
  if (memoryDetailOpen()) hideMemoryDetail();
  cancelToolbarSearch();
  state.query = trimmed;
  els.search.value = trimmed;
  syncSearchInputUI();
  switchView("memories", { autoLoadMemories: false });
  loadMemories({ preserveSelection: false });
}

function renderCmdkResults() {
  const container = $("cmdk-list");
  if (!container) return;
  clear(container);
  const query = state.cmdk.query.trim();
  const qLower = query.toLowerCase();
  const flatItems = [];

  const addSection = (title, entries) => {
    if (!entries.length) return;
    container.appendChild(element("div", "cmdk-section-label", title));
    entries.forEach((entry) => {
      const idx = flatItems.length;
      const btn = element("button", "cmdk-item");
      btn.type = "button";
      btn.id = `cmdk-opt-${idx}`;
      btn.setAttribute("role", "option");
      const selected = idx === state.cmdk.activeIndex;
      btn.setAttribute("aria-selected", String(selected));
      btn.appendChild(icon(entry.icon || "file"));

      const main = element("div", "cmdk-item-main");
      const titleEl = element("div", "cmdk-item-title");
      if (entry.rawTitle) {
        titleEl.dataset.noI18n = "true";
        appendHighlighted(titleEl, entry.title, query);
      } else {
        titleEl.textContent = entry.title;
      }
      main.appendChild(titleEl);

      if (entry.sub) {
        const subEl = element("div", "cmdk-item-sub");
        if (entry.rawSub) {
          subEl.dataset.noI18n = "true";
          appendHighlighted(subEl, entry.sub, query);
        } else {
          subEl.textContent = entry.sub;
        }
        main.appendChild(subEl);
      }
      btn.appendChild(main);

      const right = element("div", "cmdk-item-right");
      if (entry.pill) {
        right.appendChild(element("span", `pill type-pill type-${entry.pillType || "reference"}`, entry.pill));
      }
      if (entry.meta) {
        const meta = element("span", "", entry.meta);
        meta.dataset.noI18n = "true";
        right.appendChild(meta);
      }
      if (entry.kbd) {
        const kbd = element("kbd", "", entry.kbd);
        kbd.dataset.noI18n = "true";
        right.appendChild(kbd);
      }
      if (right.childNodes.length) btn.appendChild(right);

      btn.addEventListener("pointerenter", () => setCmdkActive(idx));
      btn.addEventListener("click", () => entry.run());
      container.appendChild(btn);
      flatItems.push({ node: btn, run: entry.run });
    });
  };

  if (query) {
    const memoryEntries = state.cmdk.memories.map((mem) => ({
      icon: "file",
      title: mem.name || fileName(mem.path),
      rawTitle: true,
      sub: cleanSnippet(mem.snippet || mem.preview || mem.description || ""),
      rawSub: true,
      pill: typeLabel(mem.type || "reference"),
      pillType: mem.type || "reference",
      meta: mem.project ? projectLabel(mem.project) : "",
      run: () => {
        closeCmdk();
        if (state.view !== "memories") switchView("memories");
        selectMemory(mem.path);
      },
    }));
    memoryEntries.push({
      icon: "search",
      title: `在记忆列表中查看「${query}」的全部结果`,
      rawTitle: false,
      kbd: isMac ? "⌘↵" : "Ctrl ↵",
      run: () => applyQueryToWall(query),
    });
    addSection("匹配记忆", memoryEntries);

    const projHits = sortedProjects(state.status?.projects || [])
      .filter((p) => `${p.key} ${projectLabel(p.key)}`.toLowerCase().includes(qLower))
      .slice(0, 4)
      .map((p) => ({
        icon: "folder",
        title: projectLabel(p.key),
        rawTitle: true,
        meta: `${p.facts ?? 0}`,
        run: () => {
          closeCmdk();
          selectProject(p.key);
        },
      }));
    addSection("匹配项目", projHits);

    const cmdHits = builtinCommands()
      .filter((c) => `${c.label} ${localized(c.label)} ${c.keywords}`.toLowerCase().includes(qLower))
      .map((c) => ({ icon: c.icon, title: c.label, kbd: c.kbd, run: c.run }));
    addSection("快捷命令", cmdHits);
  } else {
    const cmds = builtinCommands().map((c) => ({ icon: c.icon, title: c.label, kbd: c.kbd, run: c.run }));
    addSection("快捷命令", cmds);

    const recent = state.results.slice(0, 6).map((mem) => ({
      icon: "file",
      title: mem.name || fileName(mem.path),
      rawTitle: true,
      sub: cleanSnippet(mem.description || mem.preview || ""),
      rawSub: true,
      pill: typeLabel(mem.type || "reference"),
      pillType: mem.type || "reference",
      meta: mem.project ? projectLabel(mem.project) : "",
      run: () => {
        closeCmdk();
        if (state.view !== "memories") switchView("memories");
        selectMemory(mem.path);
      },
    }));
    addSection("最近记忆", recent);
  }

  state.cmdk.items = flatItems;
  if (!flatItems.length) {
    container.appendChild(element("div", "cmdk-empty", state.cmdk.loading ? "正在加载…" : "没有找到匹配的记忆或命令"));
    return;
  }
  setCmdkActive(Math.min(state.cmdk.activeIndex, flatItems.length - 1));
}

function setCmdkActive(index) {
  const items = state.cmdk.items;
  if (!items.length) return;
  const clamped = Math.max(0, Math.min(items.length - 1, index));
  state.cmdk.activeIndex = clamped;
  items.forEach((item, idx) => {
    item.node.setAttribute("aria-selected", String(idx === clamped));
  });
  items[clamped].node.scrollIntoView({ block: "nearest" });
}

/* ---- Keyboard Shortcuts Cheat Sheet Modal ------------------------------- */
function openShortcutsModal() {
  if (!els.shortcutsModal) return;
  if (!els.shortcutsModal.hidden) {
    closeShortcutsModal();
    return;
  }
  showModal(els.shortcutsModal);
  requestAnimationFrame(() => $("close-shortcuts")?.focus({ preventScroll: true }));
}

function closeShortcutsModal() {
  if (!els.shortcutsModal || els.shortcutsModal.hidden) return;
  hideModal(els.shortcutsModal, { animate: true });
}

/* ---- View Switching, Nav Morph & Refresh -------------------------------- */
function switchView(view, { autoLoadMemories = true } = {}) {
  cancelToolbarSearch();
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
    const active = view === "memories" && Boolean(state.project) && row.dataset.project === state.project;
    row.classList.toggle("is-active", active);
    row.setAttribute("aria-pressed", String(active));
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
    requestAnimationFrame(() => positionSetThumbs({ immediate: true }));
  } else {
    resetSettingsSearch();
  }
  if (view === "memories" && !state.results.length && autoLoadMemories) loadMemories();
}

function animateTopNavFromSidebar(fromRect) {
  const seg = $("main-nav");
  if (!seg || !fromRect) return;
  const to = seg.getBoundingClientRect();
  if (!to.width) return;
  positionSegThumb();
  if (matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  const dx = fromRect.left + fromRect.width / 2 - (to.left + to.width / 2);
  const dy = fromRect.top + fromRect.height / 2 - (to.top + to.height / 2);
  const baseTransform = "translate(-50%, -50%)";
  const startTransform = `${baseTransform} translate(${dx}px, ${dy}px) scale(.35)`;
  seg.getAnimations().forEach((a) => a.cancel());
  const animation = seg.animate(
    [
      { transform: startTransform, opacity: 0.15 },
      { transform: baseTransform, opacity: 1 },
    ],
    { duration: 280, easing: "cubic-bezier(.2, .8, .2, 1)" },
  );
  window.setTimeout(() => animation.cancel(), 340);
}

function animateTopNavToSidebar(fromRect) {
  const seg = $("main-nav");
  if (!seg || !fromRect) return;
  seg.classList.add("nav-return");
  const base = seg.getBoundingClientRect();
  const row = document.querySelector(".side-scroll .side-row");
  const sidebar = document.querySelector(".sidebar");
  if (!row || !sidebar || !base.width || matchMedia("(prefers-reduced-motion: reduce)").matches) {
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
  seg.getAnimations().forEach((a) => a.cancel());
  const animation = seg.animate(
    [
      { transform: baseTransform, opacity: 1 },
      { transform: endTransform, opacity: 0 },
    ],
    { duration: 250, easing: "cubic-bezier(.2, .8, .2, 1)", fill: "forwards" },
  );
  const cleanup = () => {
    seg.classList.remove("nav-return");
    seg.getAnimations().forEach((a) => a.cancel());
  };
  animation.finished.then(cleanup).catch(cleanup);
  window.setTimeout(cleanup, 320);
}

function measureScrollbarWidth() {
  const probe = element("div");
  probe.style.cssText = "position:absolute;top:-1000px;left:0;width:50px;height:20px;overflow-y:scroll;visibility:hidden";
  document.body.appendChild(probe);
  const width = probe.offsetWidth - probe.clientWidth;
  probe.remove();
  document.documentElement.style.setProperty("--bar", `${Math.max(0, width)}px`);
}

async function retryAll() {
  const status = await loadStatus({ announce: true });
  if (!status) return;
  if (state.view === "adapters") await loadAdapters();
  else if (state.view === "inbox") await loadInbox();
  else if (state.view === "history") await loadHistory();
  else if (state.view === "settings") await loadSettings();
  else await loadMemories({ preserveSelection: false });
}

async function refreshCurrent() {
  const button = $("refresh-view");
  button.disabled = true;
  let refreshed = false;
  cancelToolbarSearch();
  const selectedPath = state.selectedPath;
  state.details.clear();
  state.detailErrors.clear();
  state.detailLoading = false;
  try {
    const status = await loadStatus();
    if (status) {
      if (state.view === "memories") {
        await loadMemories({ preserveSelection: true });
        if (selectedPath && state.selectedPath === selectedPath && memoryDetailOpen()) {
          await selectMemory(selectedPath, true);
        }
        refreshed = true;
      } else if (state.view === "inbox") {
        refreshed = await loadInbox();
      } else if (state.view === "adapters") {
        refreshed = await loadAdapters();
      } else if (state.view === "history") {
        refreshed = await loadHistory();
      } else if (state.view === "settings") {
        refreshed = await loadSettings();
      } else {
        refreshed = true;
      }
    }
  } finally {
    button.disabled = false;
  }
  if (refreshed) toast("已刷新");
  else toast("刷新失败，请确认本地 hub 正在运行", "error");
}

/* ---- Event Wiring & Keyboard Shortcuts ---------------------------------- */
let cancelToolbarSearch = () => {};

function focusToolbarSearch() {
  if (state.view === "settings") switchView("memories");
  els.search.focus();
  requestAnimationFrame(() => els.search.select());
}

function handleEscapeTopLayer(event) {
  if (popMenu) {
    event.preventDefault();
    closePopMenu({ restoreFocus: true });
    return true;
  }
  const top = topOpenModal();
  if (!top) return false;
  event.preventDefault();
  if (top === els.cmdkModal) closeCmdk();
  else if (top === els.shortcutsModal) closeShortcutsModal();
  else if (top === els.modal) closeDialog();
  else if (top === els.adapterModal) closeAdapterInfo();
  else if (top === els.detailModal) closeMemoryDetail();
  return true;
}

function navigateMemoryCardsByKey(deltaOrKey, event) {
  const cards = [...els.resultRegion.querySelectorAll(".memory-card")];
  if (!cards.length) return;
  event.preventDefault();
  const activeIdx = cards.indexOf(document.activeElement);
  const selectedIdx = state.selectedPath ? cards.findIndex((c) => c.dataset.path === state.selectedPath) : -1;
  const baseIdx = activeIdx >= 0 ? activeIdx : selectedIdx;

  let next = 0;
  if (baseIdx < 0) {
    next = deltaOrKey === "ArrowUp" || deltaOrKey === "k" ? cards.length - 1 : 0;
  } else if (deltaOrKey === "Home") {
    next = 0;
  } else if (deltaOrKey === "End") {
    next = cards.length - 1;
  } else if (deltaOrKey === "j" || deltaOrKey === "ArrowRight") {
    next = Math.min(cards.length - 1, baseIdx + 1);
  } else if (deltaOrKey === "k" || deltaOrKey === "ArrowLeft") {
    next = Math.max(0, baseIdx - 1);
  } else {
    const cols = wallColumns(cards);
    const step = deltaOrKey === "ArrowUp" ? -cols : cols;
    next = Math.min(cards.length - 1, Math.max(0, baseIdx + step));
  }
  cards[next]?.focus();
  cards[next]?.scrollIntoView({ block: "nearest" });
}

function bindEvents() {
  // Toolbar instant search (IME-safe)
  cancelToolbarSearch = bindSearchInput(els.search, {
    debounceMs: 220,
    onCommit: (raw) => {
      const query = String(raw || "").trim();
      syncSearchInputUI();
      if (query === state.query && state.view === "memories") return;
      state.query = query;
      state.selectedPath = "";
      if (state.view !== "memories") switchView("memories", { autoLoadMemories: false });
      loadMemories({ preserveSelection: false });
    },
    onEscape: (event, cancel) => {
      if (els.search.value.trim()) {
        event.preventDefault();
        cancel();
        els.search.value = "";
        state.query = "";
        state.selectedPath = "";
        syncSearchInputUI();
        loadMemories({ preserveSelection: false });
        return;
      }
      els.search.blur();
    },
  });

  els.searchClear.addEventListener("click", () => {
    cancelToolbarSearch();
    els.search.value = "";
    state.query = "";
    state.selectedPath = "";
    syncSearchInputUI();
    els.search.focus();
    loadMemories({ preserveSelection: false });
  });
  els.searchToggle.addEventListener("click", focusToolbarSearch);
  els.searchKbd?.addEventListener("click", () => openCmdk(state.query));
  $("open-cmdk")?.addEventListener("click", () => openCmdk());
  $("open-shortcuts")?.addEventListener("click", openShortcutsModal);
  $("side-open-shortcuts")?.addEventListener("click", openShortcutsModal);

  // Cmd+K Palette input & navigation (IME-safe)
  const cmdkInput = $("cmdk-input");
  bindSearchInput(cmdkInput, {
    debounceMs: 160,
    onCommit: (raw) => runCmdkSearch(raw),
  });
  $("cmdk-clear")?.addEventListener("click", () => {
    if (!cmdkInput) return;
    cmdkInput.value = "";
    runCmdkSearch("");
    cmdkInput.focus();
  });
  els.cmdkModal?.addEventListener("keydown", (event) => {
    if (isIMEComposing(event)) return;
    trapModalTab(els.cmdkModal, event);
    const items = state.cmdk.items;
    if (event.key === "ArrowDown" || (event.ctrlKey && event.key.toLowerCase() === "n")) {
      event.preventDefault();
      if (items.length) setCmdkActive((state.cmdk.activeIndex + 1) % items.length);
    } else if (event.key === "ArrowUp" || (event.ctrlKey && event.key.toLowerCase() === "p")) {
      event.preventDefault();
      if (items.length) setCmdkActive((state.cmdk.activeIndex - 1 + items.length) % items.length);
    } else if (event.key === "Enter") {
      event.preventDefault();
      if ((event.metaKey || event.ctrlKey) && state.cmdk.query.trim()) {
        applyQueryToWall(state.cmdk.query);
        return;
      }
      items[state.cmdk.activeIndex]?.run();
    }
  });
  els.cmdkModal?.addEventListener("click", (event) => {
    if (event.target === els.cmdkModal) closeCmdk();
  });

  // Shortcuts Modal
  $("close-shortcuts")?.addEventListener("click", closeShortcutsModal);
  els.shortcutsModal?.addEventListener("click", (event) => {
    if (event.target === els.shortcutsModal) closeShortcutsModal();
  });
  els.shortcutsModal?.addEventListener("keydown", (event) => trapModalTab(els.shortcutsModal, event));

  // Adapter Center instant search & filter pills (IME-safe)
  bindSearchInput(els.adapterSearch, {
    debounceMs: 0,
    onCommit: (raw) => {
      state.adapterQuery = String(raw || "");
      if (els.adapterSearchClear) els.adapterSearchClear.hidden = !state.adapterQuery.trim();
      renderAdapters();
    },
    onEscape: (event) => {
      if (els.adapterSearch?.value) {
        event.preventDefault();
        els.adapterSearch.value = "";
        state.adapterQuery = "";
        if (els.adapterSearchClear) els.adapterSearchClear.hidden = true;
        renderAdapters();
      }
    },
  });
  els.adapterSearchClear?.addEventListener("click", () => {
    if (!els.adapterSearch) return;
    els.adapterSearch.value = "";
    state.adapterQuery = "";
    els.adapterSearchClear.hidden = true;
    renderAdapters();
    els.adapterSearch.focus();
  });
  document.querySelectorAll("[data-adapter-filter]").forEach((chip) => {
    chip.addEventListener("click", () => {
      state.adapterFilter = chip.dataset.adapterFilter || "all";
      document.querySelectorAll("[data-adapter-filter]").forEach((btn) => {
        const on = btn === chip;
        btn.classList.toggle("on", on);
        btn.setAttribute("aria-pressed", String(on));
      });
      renderAdapters();
    });
  });

  // Inbox target project selector
  els.inboxProjectSelect?.addEventListener("change", () => {
    state.inboxTargetProject = els.inboxProjectSelect.value || "global";
  });

  // Infinite scroll trigger
  const viewport = document.querySelector(".view");
  if (viewport) {
    viewport.addEventListener("scroll", () => {
      queueLoadMoreCheck();
      const head = document.querySelector("#view-settings .set-head");
      if (head) head.classList.toggle("stuck", viewport.scrollTop > 4);
    }, { passive: true });
  }

  // Global Keyboard Shortcuts (IME-safe & Modal-Stack-Aware)
  document.addEventListener("keydown", (event) => {
    if (isIMEComposing(event)) return;

    // Cmd/Ctrl+K works from ANY view and even while reading a memory detail!
    if ((event.metaKey || event.ctrlKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === "k") {
      if (topOpenModal() === els.modal && isFormDirty()) return;
      event.preventDefault();
      openCmdk();
      return;
    }

    // Cmd/Ctrl+Enter inside memory editor submits immediately
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter" && topOpenModal() === els.modal) {
      event.preventDefault();
      els.form.requestSubmit();
      return;
    }

    if (event.key === "Escape") {
      if (handleEscapeTopLayer(event)) return;
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
    if (event.metaKey && event.ctrlKey && event.key.toLowerCase() === "s") {
      event.preventDefault();
      toggleSidebarCollapsed();
      return;
    }
    if ((event.metaKey || event.ctrlKey) && !event.altKey && !event.shiftKey && (event.key === "," || event.code === "Comma")) {
      if (topOpenModal() === els.modal && isFormDirty()) return;
      event.preventDefault();
      if (isAnyModalOpen()) {
        closeCmdk();
        closeShortcutsModal();
        closeAdapterInfo();
        hideMemoryDetail();
      }
      switchView("settings");
      return;
    }

    // Single-key shortcuts only when not typing in an input
    if (isEditableTarget(event.target) || event.metaKey || event.ctrlKey || event.altKey) return;

    // Shortcuts inside open Memory Reading Sheet
    if (topOpenModal() === els.detailModal) {
      if (event.key.toLowerCase() === "e") {
        const detail = state.details.get(state.selectedPath);
        if (detail) {
          event.preventDefault();
          openEditDialog(detail);
        }
        return;
      }
      if (event.key === "j" || event.key === "k") {
        const adj = adjacentMemoryPath(event.key === "j" ? 1 : -1);
        if (adj) {
          event.preventDefault();
          selectMemory(adj);
        }
        return;
      }
      if (event.key === "?") {
        event.preventDefault();
        openShortcutsModal();
      }
      return;
    }

    if (isAnyModalOpen() || popMenu) return;

    if (event.key === "/") {
      event.preventDefault();
      if (state.view === "adapters" && els.adapterSearch) els.adapterSearch.focus();
      else if (state.view === "settings" && $("set-q")) $("set-q").focus();
      else focusToolbarSearch();
      return;
    }
    if (event.key === "?") {
      event.preventDefault();
      openShortcutsModal();
      return;
    }
    if (event.key.toLowerCase() === "n") {
      event.preventDefault();
      openDialog();
      return;
    }
    if (
      state.view === "memories" &&
      /^(j|k|ArrowLeft|ArrowRight|ArrowUp|ArrowDown|Home|End)$/.test(event.key)
    ) {
      navigateMemoryCardsByKey(event.key, event);
    }
  });

  // View navigation & filters
  document.querySelectorAll("[data-view]").forEach((tab) => {
    tab.addEventListener("click", () => switchView(tab.dataset.view));
  });
  $("all-memories")?.addEventListener("click", () => {
    state.type = "";
    els.typeFilter.value = "";
    selectProject("");
  });
  els.projectFilter.addEventListener("change", () => {
    cancelToolbarSearch();
    state.project = els.projectFilter.value;
    renderProjects(state.status?.projects || []);
    $("all-memories")?.classList.toggle("is-active", !state.project);
    requestAnimationFrame(positionSideHighlight);
    syncFilterLabels();
    loadMemories({ preserveSelection: true });
  });
  els.typeFilter.addEventListener("change", () => {
    cancelToolbarSearch();
    state.type = els.typeFilter.value;
    syncFilterLabels();
    loadMemories({ preserveSelection: true });
  });
  els.filterReset?.addEventListener("click", clearFilters);

  $("project-filter-button")?.addEventListener("click", openProjectMenu);
  $("type-filter-button")?.addEventListener("click", openTypeMenu);
  $("sort-button")?.addEventListener("click", openSortMenu);

  // Settings controls
  document.querySelectorAll("[data-read-choice]").forEach((button) => {
    button.addEventListener("click", () => applyReadSize(button.dataset.readChoice));
  });
  document.querySelectorAll("[data-theme-choice]").forEach((button) => {
    button.addEventListener("click", () => setTheme(button.dataset.themeChoice));
  });

  $("new-memory-top")?.addEventListener("click", () => openDialog());
  $("refresh-view")?.addEventListener("click", refreshCurrent);
  $("copy-root")?.addEventListener("click", () => writeClipboard($("settings-root")?.textContent, "根目录已复制", $("copy-root")));
  $("copy-mcp-url")?.addEventListener("click", () => writeClipboard($("settings-mcp-url")?.textContent, "MCP 地址已复制", $("copy-mcp-url")));
  $("copy-token")?.addEventListener("click", () => writeClipboard(state.settings?.token || $("settings-token")?.textContent, "Token 已复制", $("copy-token")));
  $("reindex")?.addEventListener("click", rebuildIndex);
  $("backup-now")?.addEventListener("click", backupNow);
  $("save-aliases")?.addEventListener("click", saveAliases);

  const settingsTabs = [...document.querySelectorAll("#set-tabs [data-set]")];
  settingsTabs.forEach((tab) => {
    tab.addEventListener("click", () => showSettingsPage(tab.dataset.set));
  });
  $("set-tabs")?.addEventListener("keydown", (event) => {
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
  bindSearchInput($("set-q"), {
    debounceMs: 0,
    onCommit: (raw) => applySettingsSearch(raw),
  });

  document.querySelectorAll("[data-import]").forEach((button) => {
    button.addEventListener("click", () => importSource(button.dataset.import, button));
  });

  // React to language toggles immediately (reposition thumbs & refresh date formatting)
  window.addEventListener("marktoast:langchange", () => {
    requestAnimationFrame(() => {
      positionSetThumbs({ immediate: true });
      positionSegThumb();
      positionSideHighlight();
    });
    if (state.settings) renderSettings();
    if (memoryDetailOpen()) renderMemoryDetail();
    if (state.adapterInfo && !els.adapterModal.hidden) renderAdapterInfo(state.adapterInfo);
    if (cmdkOpen()) renderCmdkResults();
  });

  // Modals: Memory Form, Detail, Adapter Info
  $("retry-hub")?.addEventListener("click", retryAll);
  $("close-dialog")?.addEventListener("click", () => closeDialog());
  $("cancel-dialog")?.addEventListener("click", () => closeDialog());
  $("body-mode-write")?.addEventListener("click", () => setFormBodyMode("write"));
  $("body-mode-preview")?.addEventListener("click", () => setFormBodyMode("preview"));
  els.form?.addEventListener("input", syncFormDirtyUI);
  els.form?.addEventListener("change", syncFormDirtyUI);
  els.form?.addEventListener("submit", submitMemory);
  els.modal?.addEventListener("click", (event) => {
    if (event.target === els.modal) closeDialog();
  });
  els.modal?.addEventListener("keydown", (event) => trapModalTab(els.modal, event));

  window.addEventListener("beforeunload", (event) => {
    if (isFormDirty()) {
      event.preventDefault();
      event.returnValue = "";
    }
  });

  $("close-detail")?.addEventListener("click", closeMemoryDetail);
  els.detailModal?.addEventListener("click", (event) => {
    if (event.target === els.detailModal) closeMemoryDetail();
  });
  els.detailModal?.addEventListener("keydown", (event) => trapModalTab(els.detailModal, event));

  $("close-adapter-dialog")?.addEventListener("click", closeAdapterInfo);
  $("adapter-dialog-close")?.addEventListener("click", closeAdapterInfo);
  els.adapterDialogAction?.addEventListener("click", () => {
    const adapter = state.adapterInfo;
    if (!adapter) return;
    if (adapterCanInstall(adapter)) runAdapterAction(adapter, els.adapterDialogAction);
    else if (adapterCanStage(adapter)) stageAdapter(adapter, els.adapterDialogAction);
  });
  els.adapterDialogUninstall?.addEventListener("click", () => {
    const adapter = state.adapterInfo;
    if (adapter) uninstallAdapter(adapter, els.adapterDialogUninstall);
  });
  els.adapterModal?.addEventListener("click", (event) => {
    if (event.target === els.adapterModal) closeAdapterInfo();
  });
  els.adapterModal?.addEventListener("keydown", (event) => trapModalTab(els.adapterModal, event));

  // Sidebar collapse & Brand click
  document.querySelectorAll("#side-toggle, #side-toggle-top").forEach((button) => {
    button.addEventListener("click", () => {
      const wasCollapsed = document.body.classList.contains("side-collapsed");
      const sideRect = wasCollapsed ? null : document.querySelector(".side-scroll")?.getBoundingClientRect();
      const segEl = $("main-nav");
      const topRect = wasCollapsed ? segEl?.getBoundingClientRect() : null;
      if (!wasCollapsed && segEl) segEl.classList.remove("nav-return");
      if (wasCollapsed && segEl) segEl.classList.add("nav-return");
      const collapsed = document.body.classList.toggle("side-collapsed");
      try {
        localStorage.setItem("marktoast-sidebar", collapsed ? "collapsed" : "open");
      } catch {}
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
      focusToolbarSearch();
    });
  });

  window.addEventListener("resize", () => {
    positionSegThumb();
    positionSetThumbs({ immediate: true });
    positionSideHighlight();
    applySideWidth();
  });

  // Note: 127.0.0.1 loopback works without internet access, so we never show
  // a false "browser is offline" hub alert on window.offline.
  window.addEventListener("online", () => {
    if (!state.status) loadStatus();
  });
}

/* ---- Bootstrap ---------------------------------------------------------- */
async function init() {
  if (!isMac) {
    const kbdSpan = els.searchKbd?.querySelector("span");
    if (kbdSpan) kbdSpan.textContent = "Ctrl K";
  }
  syncSidebarControls();
  syncModalLayers();
  initSegmentedObservers();
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
