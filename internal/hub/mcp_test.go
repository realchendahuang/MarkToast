package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
)

// connectMCP wires a client to the real MCP server over in-memory transports so
// the tool schemas and the SDK's argument validation are exercised for real.
func connectMCP(t *testing.T, root string) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	session, err := NewMCPServer(root).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}

func mcpRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFact(root, store.GlobalProject, store.Fact{
		Name: "badge-icon", Description: "icon notes", Type: "reference", Body: "safe-area limit",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Rebuild(root); err != nil {
		t.Fatal(err)
	}
	return root
}

// memory_get used to reject {"id": ...}: clients call the fact name an id, and
// the schema had additionalProperties=false with no id property.
func TestMemoryGetAcceptsIDAlias(t *testing.T) {
	session := connectMCP(t, mcpRoot(t))
	ctx := context.Background()

	for name, args := range map[string]map[string]any{
		"name": {"name": "badge-icon"},
		"id":   {"id": "badge-icon"},
	} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "memory_get", Arguments: args})
		if err != nil {
			t.Fatalf("%s: call failed: %v", name, err)
		}
		if res.IsError {
			t.Fatalf("%s: tool error: %s", name, textOf(res))
		}
		if !strings.Contains(textOf(res), "safe-area limit") {
			t.Fatalf("%s: body missing from result: %s", name, textOf(res))
		}
	}
}

func TestMemoryGetIDAliasReportsMiss(t *testing.T) {
	session := connectMCP(t, mcpRoot(t))
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "memory_get", Arguments: map[string]any{"id": "does-not-exist"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(textOf(res), `memory "does-not-exist" not found`) {
		t.Fatalf("expected a not-found tool error, got isError=%v: %s", res.IsError, textOf(res))
	}
}

func TestMemoryGetSchemaAdvertisesID(t *testing.T) {
	session := connectMCP(t, mcpRoot(t))
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "memory_get" {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"path", "name", "id"} {
			if _, ok := schema.Properties[field]; !ok {
				t.Fatalf("memory_get schema is missing %q: %s", field, raw)
			}
		}
		return
	}
	t.Fatal("memory_get was not listed")
}

func textOf(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}
