# MarkToast provider for Hermes

Copy this directory into Hermes' user plugin directory:

```bash
cp -R adapters/hermes/marktoast ~/.hermes/plugins/marktoast
```

Then activate it (it replaces Hermes' built-in memory provider):

```yaml
# ~/.hermes/config.yaml
memory:
  provider: marktoast
```

Environment (optional):

- `MARKTOAST_ROOT` — repository root (default `~/Memory`)
- `MARKTOAST_HUB` — hub URL (default `http://127.0.0.1:3777`)

The provider uses the hub for recall, `marktoast_search` / `marktoast_save`
tools, and queues session-end candidates into the MarkToast inbox. Start the
MarkToast app or `marktoast serve` before a Hermes session for best results;
recall returns empty rather than failing when the hub is down.
