// MarkToast JS plugin example.
// Drop into <memory-root>/.marktoast/plugins/ and restart the hub.

marktoast.registerAdapter({
  id: "hello-harness",
  title: "Hello Harness",
  detect: ["~/.hello-harness"],
  tier: "projection",
  capabilities: ["read"],
  notes: "Example plugin adapter."
});

marktoast.on("memory.saved", function (payload) {
  marktoast.log("saved memory: " + payload.name);
});

marktoast.on("inbox.promoted", function (payload) {
  marktoast.log("promoted candidate: " + payload.path);
});
