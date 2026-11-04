"""MarkToast memory provider for Hermes Agent.

Hermes keeps its own built-in memory; this provider points recall and writes at a
MarkToast repository instead. It talks to the local hub over HTTP (starting the
MarkToast app or `marktoast serve` is enough); no credentials are needed beyond
the loopback hub.

Install:
    cp -R adapters/hermes/marktoast ~/.hermes/plugins/marktoast
    # then set memory.provider: marktoast in ~/.hermes/config.yaml

Environment:
    MARKTOAST_ROOT   memory repository (default ~/Memory)
    MARKTOAST_HUB    hub base URL (default http://127.0.0.1:3777)
"""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any

from agent.memory_provider import MemoryProvider, RecallStatus, is_trivial_prompt

_DEFAULT_HUB = "http://127.0.0.1:3777"
_MAX_BODY = 20000
_GLYPH = "🍞"


def _request(url: str, *, data: dict[str, Any] | None = None, timeout: float = 2.0) -> Any:
    payload = None
    headers = {"Accept": "application/json"}
    if data is not None:
        payload = json.dumps(data, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(url, data=payload, headers=headers)
    with urllib.request.urlopen(request, timeout=timeout) as response:
        body = response.read().decode("utf-8", errors="replace")
    return json.loads(body) if body else {}


class MarkToastMemoryProvider(MemoryProvider):
    """Recall and write Markdown memories through the MarkToast hub."""

    def __init__(self) -> None:
        self._root = Path(os.environ.get("MARKTOAST_ROOT", str(Path.home() / "Memory"))).expanduser()
        self._hub = os.environ.get("MARKTOAST_HUB", _DEFAULT_HUB).rstrip("/")
        self._session = ""
        self._last_status: RecallStatus | None = None

    # -- lifecycle -------------------------------------------------------
    def name(self) -> str:
        return "marktoast"

    def is_available(self) -> bool:
        return (self._root / "projects").is_dir() or (self._root / "global").is_dir()

    def unavailable_reason(self) -> str:
        return f"MarkToast repository not found at {self._root}; run `marktoast init` first."

    def initialize(self, session_id: str, **kwargs) -> None:
        self._session = session_id or ""

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
        if is_trivial_prompt(query):
            self._last_status = RecallStatus("MarkToast", 0, _GLYPH)
            return ""
        try:
            params = urllib.parse.urlencode({"q": query, "limit": 6})
            data = _request(f"{self._hub}/api/search?{params}")
        except Exception:
            self._last_status = None
            return ""
        results = data.get("results") or []
        if not results:
            self._last_status = RecallStatus("MarkToast", 0, _GLYPH)
            return ""
        lines = ["## MarkToast memory", ""]
        for item in results[:6]:
            name = str(item.get("name") or "memory")
            description = str(item.get("description") or item.get("snippet") or "").strip()
            project = str(item.get("project") or "")
            suffix = f" ({project})" if project else ""
            lines.append(f"- **{name}**{suffix}: {description[:240]}")
        lines.append("")
        lines.append("Use `marktoast_search` for exact details; `marktoast_save` writes a new memory.")
        self._last_status = RecallStatus("MarkToast", len(results), _GLYPH)
        return "\n".join(lines)

    def recall_status(self) -> RecallStatus | None:
        return self._last_status

    # -- write -----------------------------------------------------------
    def on_memory_write(self, action: str, target: str, content: str, metadata: dict[str, Any] | None = None) -> None:
        if action != "add" or not content.strip():
            return
        self._candidate(content, target or "memory", "hermes-native-write")

    def on_session_end(self, messages: list[dict[str, Any]]) -> None:
        """Queue a reviewable candidate from the last substantive assistant message."""
        for message in reversed(messages or []):
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
                        "type": {"type": "string", "enum": ["user", "feedback", "project", "reference"], "default": "reference"},
                    },
                    "required": ["name", "body"],
                },
            },
        ]

    def handle_tool_call(self, tool_name: str, args: dict[str, Any], **kwargs) -> str:
        arguments = args or {}
        if tool_name == "marktoast_search":
            query = str(arguments.get("query") or "").strip()
            if not query:
                return json.dumps({"error": "query is required"}, ensure_ascii=False)
            limit = max(1, min(int(arguments.get("limit") or 6), 20))
            try:
                params = urllib.parse.urlencode({"q": query, "limit": limit})
                data = _request(f"{self._hub}/api/search?{params}")
                return json.dumps(data, ensure_ascii=False)
            except Exception as exc:
                return json.dumps({"error": f"MarkToast hub unavailable: {exc}"}, ensure_ascii=False)
        if tool_name == "marktoast_save":
            name = str(arguments.get("name") or "").strip()
            body = str(arguments.get("body") or "").strip()[:_MAX_BODY]
            if not name or not body:
                return json.dumps({"error": "name and body are required"}, ensure_ascii=False)
            payload = {
                "name": name,
                "description": str(arguments.get("description") or "").strip(),
                "body": body,
                "project": str(arguments.get("project") or "global"),
                "type": str(arguments.get("type") or "reference"),
            }
            try:
                data = _request(f"{self._hub}/api/memory", data=payload)
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
            _request(f"{self._hub}/api/inbox/candidate", data=payload)
        except Exception:
            # The hub may not be running; canonical memory still works without it.
            pass


def register(ctx) -> None:
    ctx.register_memory_provider(MarkToastMemoryProvider())
