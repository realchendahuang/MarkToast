"""Unit tests for the Hermes MarkToast memory provider plugin."""

from __future__ import annotations

import json
import os
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import marktoast as provider_mod  # noqa: E402


class _MockHubHandler(BaseHTTPRequestHandler):
    expected_token = "tok-initial"
    last_payload: dict | None = None

    def log_message(self, format: str, *args: object) -> None:  # noqa: A002
        return

    def _check_auth(self) -> bool:
        if self.headers.get("Authorization") != f"Bearer {self.expected_token}":
            self.send_response(401)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"error":"unauthorized"}')
            return False
        return True

    def do_GET(self) -> None:  # noqa: N802
        if not self._check_auth():
            return
        if self.path.startswith("/api/search"):
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            body = json.dumps(
                {"results": [{"name": "pref-lang", "description": "始终用中文回复", "project": "global"}]},
                ensure_ascii=False,
            ).encode("utf-8")
            self.wfile.write(body)
            return
        self.send_response(404)
        self.end_headers()

    def do_POST(self) -> None:  # noqa: N802
        if not self._check_auth():
            return
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length).decode("utf-8")
        _MockHubHandler.last_payload = json.loads(raw) if raw else {}
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"path":"/tmp/mock/global/pref.md"}')


class HermesProviderTests(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        (self.root / "global").mkdir(parents=True)
        (self.root / ".marktoast").mkdir(parents=True)
        self.server = HTTPServer(("127.0.0.1", 0), _MockHubHandler)
        _MockHubHandler.expected_token = "tok-initial"
        _MockHubHandler.last_payload = None
        port = self.server.server_address[1]
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self._old_env = {k: os.environ.get(k) for k in ("MARKTOAST_ROOT", "MARKTOAST_HUB", "MARKTOAST_TOKEN")}
        os.environ["MARKTOAST_ROOT"] = str(self.root)
        os.environ["MARKTOAST_HUB"] = f"http://127.0.0.1:{port}"
        os.environ.pop("MARKTOAST_TOKEN", None)

    def tearDown(self) -> None:
        self.server.shutdown()
        self.server.server_close()
        for k, v in self._old_env.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v
        self._tmp.cleanup()

    def test_lazy_token_and_401_refresh(self) -> None:
        # Provider initialized BEFORE hub writes .marktoast/token.
        prov = provider_mod.MarkToastMemoryProvider()
        self.assertTrue(prov.is_available())
        (self.root / ".marktoast" / "token").write_text("tok-initial\n", encoding="utf-8")
        out = prov.prefetch("之前的部署架构约定是什么？")
        self.assertIn("pref-lang", out)
        self.assertIn("始终用中文回复", out)

        # Token rotates on disk -> 401 triggers automatic token reload & retry.
        _MockHubHandler.expected_token = "tok-rotated"
        (self.root / ".marktoast" / "token").write_text("tok-rotated\n", encoding="utf-8")
        res = json.loads(prov.handle_tool_call("marktoast_search", {"query": "部署", "limit": "not-an-int"}))
        self.assertEqual(len(res["results"]), 1)

    def test_defensive_tool_arguments_and_session_end(self) -> None:
        (self.root / ".marktoast" / "token").write_text("tok-initial", encoding="utf-8")
        prov = provider_mod.MarkToastMemoryProvider()
        prov.initialize("sess-42")

        # JSON string args + invalid memory type falls back cleanly to reference.
        res = json.loads(
            prov.handle_tool_call(
                "marktoast_save",
                json.dumps({"name": "rule", "body": "content", "type": "bogus", "project": "  "}),
            )
        )
        self.assertEqual(res["path"], "/tmp/mock/global/pref.md")
        self.assertEqual(_MockHubHandler.last_payload["type"], "reference")
        self.assertEqual(_MockHubHandler.last_payload["project"], "global")

        # Malformed JSON args must not raise.
        bad = json.loads(prov.handle_tool_call("marktoast_search", "{broken"))
        self.assertIn("error", bad)

        # Malformed session messages must not raise.
        prov.on_session_end([None, "bad", {"role": "assistant", "content": "x" * 100}])  # type: ignore[list-item]
        self.assertEqual(_MockHubHandler.last_payload["session"], "sess-42")


if __name__ == "__main__":
    unittest.main()
