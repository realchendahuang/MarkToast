"""MarkToast memory provider for Hermes Agent.

Hermes keeps its own built-in memory; this provider points recall and writes at a
MarkToast repository instead. It talks to the local hub over HTTP (starting the
MarkToast app or `marktoast serve` is enough); the loopback bearer token is read
automatically from `<root>/.marktoast/token` (or `MARKTOAST_TOKEN`).

Install:
    cp -R adapters/hermes/marktoast ~/.hermes/plugins/marktoast
    # then set memory.provider: marktoast in ~/.hermes/config.yaml

Environment:
    MARKTOAST_ROOT   memory repository (default ~/Memory)
    MARKTOAST_HUB    hub base URL (default http://127.0.0.1:3777)
    MARKTOAST_TOKEN  hub bearer token override
"""

from __future__ import annotations

from dataclasses import dataclass
import json
import os
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any

try:
    from agent.memory_provider import MemoryProvider, RecallStatus, is_trivial_prompt
except ImportError:  # pragma: no cover - standalone tests / CLI inspection outside Hermes
    class MemoryProvider:  # type: ignore[no-redef]
        """Minimal fallback base when imported outside the Hermes runtime."""

    @dataclass
    class RecallStatus:  # type: ignore[no-redef]
        label: str
        count: int
        glyph: str = ""

    def is_trivial_prompt(prompt: str) -> bool:  # type: ignore[no-redef]
        text = (prompt or "").strip()
        return len(text) < 3 or text.lower() in {"hi", "hello", "hey", "ok", "yes", "no", "thanks"}


_DEFAULT_HUB = "http://127.0.0.1:3777"
_MAX_BODY = 20000
_GLYPH = "🍞"
_VALID_TYPES = frozenset({"user", "feedback", "project", "reference", "decision"})


def _load_token(root: Path) -> str:
    """Hub bearer token: MARKTOAST_TOKEN wins, else <root>/.marktoast/token."""
    token = os.environ.get("MARKTOAST_TOKEN", "").strip()
    if token:
        return token
    try:
        return (root / ".marktoast" / "token").read_text(encoding="utf-8").strip()
    except OSError:
        return ""


def _parse_limit(raw: Any, default: int = 6, minimum: int = 1, maximum: int = 20) -> int:
    if raw is None or isinstance(raw, bool):
        value = default
    else:
        try:
            value = int(raw)
        except (TypeError, ValueError):
            value = default
    return max(minimum, min(value, maximum))


def _coerce_args(raw: Any) -> tuple[dict[str, Any], str | None]:
    if raw is None:
        return {}, None
    if isinstance(raw, dict):
        return raw, None
    if isinstance(raw, str):
        text = raw.strip()
        if not text:
            return {}, None
        try:
            parsed = json.loads(text)
        except ValueError as exc:
            return {}, f"invalid tool arguments JSON: {exc}"
        if isinstance(parsed, dict):
            return parsed, None
        return {}, "tool arguments must be a JSON object"
    return {}, "tool arguments must be a mapping"


def _request(
    url: str, *, data: dict[str, Any] | None = None, token: str = "", timeout: float = 2.0
) -> dict[str, Any]:
    payload = None
    headers = {"Accept": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    if data is not None:
        payload = json.dumps(data, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(url, data=payload, headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            body = response.read().decode("utf-8", errors="replace").strip()
    except urllib.error.HTTPError as exc:
        detail = ""
        try:
            err_body = exc.read().decode("utf-8", errors="replace").strip()
            if err_body:
                parsed_err = json.loads(err_body)
                if isinstance(parsed_err, dict) and parsed_err.get("error"):
                    detail = f": {parsed_err['error']}"
        except Exception:
            pass
        finally:
            try:
                exc.close()
            except Exception:
                pass
        raise RuntimeError(f"HTTP {exc.code}{detail}") from exc
    if not body:
        return {}
    parsed = json.loads(body)
    return parsed if isinstance(parsed, dict) else {"data": parsed}


class MarkToastMemoryProvider(MemoryProvider):
    """Recall and write Markdown memories through the MarkToast hub."""

    def __init__(self) -> None:
        raw_root = os.environ.get("MARKTOAST_ROOT", "").strip()
        self._root = Path(raw_root).expanduser() if raw_root else (Path.home() / "Memory")
        raw_hub = os.environ.get("MARKTOAST_HUB", "").strip() or _DEFAULT_HUB
        self._hub = raw_hub.rstrip("/")
        self._token = _load_token(self._root)
        self._session = ""
        self._last_status: RecallStatus | None = None

    def _call_hub(
        self, path_and_query: str, *, data: dict[str, Any] | None = None, timeout: float = 2.0
    ) -> dict[str, Any]:
        """Request the hub, lazily reloading the token file on first start or 401."""
        if not self._token:
            self._token = _load_token(self._root)
        url = f"{self._hub}{path_and_query}"
        try:
            return _request(url, data=data, token=self._token, timeout=timeout)
        except RuntimeError as exc:
            if "HTTP 401" in str(exc):
                refreshed = _load_token(self._root)
                if refreshed and refreshed != self._token:
                    self._token = refreshed
                    return _request(url, data=data, token=self._token, timeout=timeout)
            raise

    # -- lifecycle -------------------------------------------------------
    def name(self) -> str:
        return "marktoast"

    def is_available(self) -> bool:
        try:
            return (self._root / "projects").is_dir() or (self._root / "global").is_dir()
        except OSError:
            return False

    def unavailable_reason(self) -> str:
        return f"MarkToast repository not found at {self._root}; run `marktoast init` first."

    def initialize(self, session_id: str, **kwargs: Any) -> None:
        self._session = str(session_id or "")

    def shutdown(self) -> None:
        self._session = ""

    def system_prompt_block(self) -> str:
        return (
            "Long-term memory is provided by MarkToast (a local, Git-versioned Markdown repository). "
            "Use the `marktoast_search` tool before asking the user to repeat context, and "
            "`marktoast_save` for durable decisions, preferences and project facts."
        )

    # -- recall ----------------------------------------------------------
    def prefetch(self, query: str, *, session_id: str = "") -> str:
        text = str(query or "").strip()
        if not text or is_trivial_prompt(text):
            self._last_status = RecallStatus("MarkToast", 0, _GLYPH)
            return ""
        try:
            params = urllib.parse.urlencode({"q": text, "limit": 6})
            data = self._call_hub(f"/api/search?{params}")
        except Exception:
            self._last_status = None
            return ""
        raw_results = data.get("results") if isinstance(data, dict) else None
        if not isinstance(raw_results, list) or not raw_results:
            self._last_status = RecallStatus("MarkToast", 0, _GLYPH)
            return ""
        lines = ["## MarkToast memory", ""]
        shown = 0
        for item in raw_results[:6]:
            if not isinstance(item, dict):
                continue
            name = str(item.get("name") or "memory").strip() or "memory"
            description = str(item.get("description") or item.get("snippet") or "").strip()
            project = str(item.get("project") or "").strip()
            suffix = f" ({project})" if project else ""
            lines.append(f"- **{name}**{suffix}: {description[:240]}")
            shown += 1
        if shown == 0:
            self._last_status = RecallStatus("MarkToast", 0, _GLYPH)
            return ""
        lines.append("")
        lines.append("Use `marktoast_search` for exact details; `marktoast_save` writes a new memory.")
        self._last_status = RecallStatus("MarkToast", shown, _GLYPH)
        return "\n".join(lines)

    def recall_status(self) -> RecallStatus | None:
        return self._last_status

    # -- write -----------------------------------------------------------
    def on_memory_write(
        self, action: str, target: str, content: str, metadata: dict[str, Any] | None = None
    ) -> None:
        text = str(content or "").strip()
        if str(action or "") != "add" or not text:
            return
        self._candidate(text, str(target or "memory"), "hermes-native-write")

    def on_session_end(self, messages: list[dict[str, Any]]) -> None:
        """Queue a reviewable candidate from the last substantive assistant message."""
        if not isinstance(messages, list):
            return
        for message in reversed(messages):
            if not isinstance(message, dict) or message.get("role") != "assistant":
                continue
            content = message.get("content")
            if isinstance(content, list):
                content = "".join(
                    str(part.get("text", "")) if isinstance(part, dict) else str(part)
                    for part in content
                )
            text = str(content or "").strip()
            if len(text) >= 80:
                self._candidate(text, "hermes-session", "hermes-session-end")
                return

    # -- tools -----------------------------------------------------------
    def get_tool_schemas(self) -> list[dict[str, Any]]:
        return [
            {
                "name": "marktoast_search",
                "description": "Search the user's MarkToast long-term memory.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "query": {"type": "string", "description": "Search query"},
                        "limit": {"type": "integer", "minimum": 1, "maximum": 20, "default": 6},
                    },
                    "required": ["query"],
                },
            },
            {
                "name": "marktoast_save",
                "description": "Save a durable decision, preference or project fact to MarkToast.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "name": {"type": "string", "description": "Short fact name"},
                        "description": {"type": "string", "description": "One-line description"},
                        "body": {"type": "string", "description": "Markdown body"},
                        "project": {"type": "string", "description": "Project key (default global)"},
                        "type": {
                            "type": "string",
                            "enum": ["user", "feedback", "project", "reference", "decision"],
                            "default": "reference",
                        },
                    },
                    "required": ["name", "body"],
                },
            },
        ]

    def handle_tool_call(self, tool_name: str, args: dict[str, Any] | str | None, **kwargs: Any) -> str:
        arguments, arg_err = _coerce_args(args)
        if arg_err:
            return json.dumps({"error": arg_err}, ensure_ascii=False)
        if tool_name == "marktoast_search":
            query = str(arguments.get("query") or "").strip()
            if not query:
                return json.dumps({"error": "query is required"}, ensure_ascii=False)
            limit = _parse_limit(arguments.get("limit"), default=6)
            try:
                params = urllib.parse.urlencode({"q": query, "limit": limit})
                data = self._call_hub(f"/api/search?{params}")
                return json.dumps(data, ensure_ascii=False)
            except Exception as exc:
                return json.dumps({"error": f"MarkToast hub unavailable: {exc}"}, ensure_ascii=False)
        if tool_name == "marktoast_save":
            name = str(arguments.get("name") or "").strip()
            body = str(arguments.get("body") or "").strip()[:_MAX_BODY]
            if not name or not body:
                return json.dumps({"error": "name and body are required"}, ensure_ascii=False)
            project = str(arguments.get("project") or "").strip() or "global"
            mem_type = str(arguments.get("type") or "").strip() or "reference"
            if mem_type not in _VALID_TYPES:
                mem_type = "reference"
            payload = {
                "name": name,
                "description": str(arguments.get("description") or "").strip(),
                "body": body,
                "project": project,
                "type": mem_type,
            }
            try:
                data = self._call_hub("/api/memory", data=payload)
                return json.dumps({"path": data.get("path", "")}, ensure_ascii=False)
            except Exception as exc:
                return json.dumps({"error": f"MarkToast hub unavailable: {exc}"}, ensure_ascii=False)
        return json.dumps({"error": f"unknown tool {tool_name}"}, ensure_ascii=False)

    # -- helpers ---------------------------------------------------------
    def _candidate(self, body: str, agent: str, source: str) -> None:
        payload = {
            "name": f"Hermes · {source}",
            "description": "Hermes session candidate",
            "body": body[:_MAX_BODY],
            "agent": agent,
            "session": self._session,
        }
        try:
            self._call_hub("/api/inbox/candidate", data=payload)
        except Exception:
            # The hub may not be running; canonical memory still works without it.
            pass


def register(ctx: Any) -> None:
    ctx.register_memory_provider(MarkToastMemoryProvider())
