package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

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
	t.Setenv("HOME", t.TempDir())
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
		for _, field := range []string{"path", "name", "id", "project"} {
			if _, ok := schema.Properties[field]; !ok {
				t.Fatalf("memory_get schema is missing %q: %s", field, raw)
			}
		}
		return
	}
	t.Fatal("memory_get was not listed")
}

// TestMCPStdioPipelineLargePayloadAndProtocol exercises the exact pipelined
// stdio pattern (`cat requests.jsonl | marktoast mcp`): >200 KiB payload,
// notifications without IDs, ping, malformed JSON recovery, unknown method
// -32601, missing params -32602, and strict FIFO ordering on immediate EOF.
func TestMCPStdioPipelineLargePayloadAndProtocol(t *testing.T) {
	root := mcpRoot(t)
	largeBody := strings.Repeat("MarkToast large durable memory line 0123456789\n", 5000) // ~235 KiB (> 64 KiB Scanner limit)

	linesIn := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"stdio-pipe","version":"1.0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/unknown_custom_event","params":{"foo":"bar"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		mustJSONLine(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      4,
			"method":  "tools/call",
			"params": map[string]any{
				"name": "memory_save",
				"arguments": map[string]any{
					"name":        "stdio-235kb-doc",
					"description": "large payload over stdio",
					"type":        "decision",
					"body":        largeBody,
				},
			},
		}),
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"memory_get","arguments":{"name":"stdio-235kb-doc"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"unknown/method"}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call"}`,
		`{malformed json line`,
		`[{"jsonrpc":"2.0","method":"notifications/ignored_in_batch"},{"jsonrpc":"2.0","id":8,"method":"ping"},{"jsonrpc":"2.0","id":9,"method":"resources/list"}]`,
	}

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input := strings.Join(linesIn, "\n") + "\n"
	if err := runMCPStream(ctx, NewMCPServer(root), strings.NewReader(input), &out); err != nil {
		t.Fatalf("runMCPStream error: %v", err)
	}

	respLines := strings.Split(strings.TrimSpace(out.String()), "\n")
	// Expected responses: id=1, id=2, id=3, id=4, id=5, id=6 (-32601), id=7 (-32602), id=null (-32700), batch [id=8, id=9] => 9 lines!
	if len(respLines) != 9 {
		t.Fatalf("expected 9 response frames (notifications must produce 0 responses), got %d:\n%s", len(respLines), out.String()[:min(len(out.String()), 600)])
	}

	// Verify id=1 negotiated 2024-11-05
	var initResp struct {
		ID     int `json:"id"`
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(respLines[0]), &initResp); err != nil || initResp.ID != 1 || initResp.Result.ProtocolVersion != "2024-11-05" {
		t.Fatalf("unexpected initialize response: %s (err=%v)", respLines[0], err)
	}

	// Verify id=5 returned the entire 235 KiB body intact without truncation
	var getResp struct {
		ID     int                 `json:"id"`
		Result *mcp.CallToolResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(respLines[4]), &getResp); err != nil || getResp.ID != 5 || getResp.Result == nil || getResp.Result.IsError {
		t.Fatalf("unexpected memory_get response: id=%d err=%v", getResp.ID, err)
	}
	var savedDoc struct {
		Type string `json:"type"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(textOf(getResp.Result)), &savedDoc); err != nil {
		t.Fatalf("decode memory_get payload: %v", err)
	}
	if savedDoc.Type != "decision" || strings.TrimRight(savedDoc.Body, "\n") != strings.TrimRight(largeBody, "\n") {
		t.Fatalf("round-trip mismatch: type=%q gotLen=%d wantLen=%d", savedDoc.Type, len(savedDoc.Body), len(largeBody))
	}

	// Verify id=6 returned -32601 Method Not Found
	assertRPCErrorCode(t, respLines[5], -32601)
	// Verify id=7 returned -32602 Invalid Params
	assertRPCErrorCode(t, respLines[6], -32602)
	// Verify malformed JSON line returned -32700 Parse Error
	assertRPCErrorCode(t, respLines[7], -32700)

	// Verify batch response [id=8, id=9]
	var batchResp []map[string]any
	if err := json.Unmarshal([]byte(respLines[8]), &batchResp); err != nil || len(batchResp) != 2 {
		t.Fatalf("unexpected batch response: %s (err=%v)", respLines[8], err)
	}
}

func TestMCPStdioOversizedFrameRecoveryAndConcurrentWrites(t *testing.T) {
	root := mcpRoot(t)
	oversized := `{"jsonrpc":"2.0","id":1,"method":"ping","padding":"` + strings.Repeat("x", 8192) + `"}`
	validInit := `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	validPing := `{"jsonrpc":"2.0","id":3,"method":"ping"}`

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := NewMCPServer(root).Run(ctx, &drainingStdioTransport{
		reader:   strings.NewReader(oversized + "\n" + validInit + "\n" + validPing + "\n"),
		writer:   &out,
		maxBytes: 2048,
	})
	if err != nil {
		t.Fatalf("Run failed after oversized frame: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 frames (error + init + ping), got %d: %s", len(lines), out.String())
	}
	assertRPCErrorCode(t, lines[0], -32600)

	// Verify concurrent writeRawBytes calls never interleave partial JSON lines.
	var concurrentBuf bytes.Buffer
	conn := &drainingStdioConn{writer: &concurrentBuf}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := mustJSONLine(t, map[string]any{"jsonrpc": "2.0", "id": i, "result": strings.Repeat("z", 512)})
			_ = conn.writeRawBytes([]byte(payload))
		}(i)
	}
	wg.Wait()
	rawLines := strings.Split(strings.TrimSpace(concurrentBuf.String()), "\n")
	if len(rawLines) != 20 {
		t.Fatalf("expected 20 intact JSON lines, got %d", len(rawLines))
	}
	for _, l := range rawLines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("corrupted concurrent JSON line: %v", err)
		}
	}
}

func TestMCPHTTPStreamableComplianceClientSDKAndEdgeCases(t *testing.T) {
	root := mcpRoot(t)
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef"
	srv := httptest.NewServer(MCPHTTPHandler(NewMCPServer(root), token))
	defer srv.Close()

	// 1. Official MCP Go SDK StreamableClientTransport end-to-end interoperability
	authedClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			r2 := req.Clone(req.Context())
			r2.Header.Set("Authorization", "Bearer "+token)
			return http.DefaultTransport.RoundTrip(r2)
		}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sdkClient := mcp.NewClient(&mcp.Implementation{Name: "sdk-http-client", Version: "1.0"}, nil)
	cs, err := sdkClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   srv.URL,
		HTTPClient: authedClient,
	}, nil)
	if err != nil {
		t.Fatalf("StreamableClientTransport Connect failed: %v", err)
	}
	defer cs.Close()
	if err := cs.Ping(ctx, nil); err != nil {
		t.Fatalf("Ping over Streamable HTTP failed: %v", err)
	}
	toolsRes, err := cs.ListTools(ctx, nil)
	if err != nil || len(toolsRes.Tools) != 4 {
		t.Fatalf("ListTools over Streamable HTTP: tools=%d err=%v", len(toolsRes.Tools), err)
	}

	// 2. Minimal HTTP client WITHOUT Accept header (curl / script / stdlib style)
	postMCP := func(body string, headers map[string]string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// Unknown future protocolVersion in initialize negotiates down cleanly
	resp := postMCP(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2099-01-01","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}`, nil)
	rawBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(rawBody), `"protocolVersion"`) {
		t.Fatalf("initialize status=%d body=%s", resp.StatusCode, rawBody)
	}

	// Notifications (both standard and custom) MUST return 202 Accepted with empty body
	for _, notif := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/custom_agent_ping","params":{"ts":1}}`,
	} {
		resp = postMCP(notif, map[string]string{"Accept": "application/json"})
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted || len(b) != 0 {
			t.Fatalf("notification %s: status=%d body=%q, want 202 empty", notif, resp.StatusCode, b)
		}
	}

	// Unknown method returns JSON-RPC -32601 Method Not Found (never plain-text 400)
	resp = postMCP(`{"jsonrpc":"2.0","id":42,"method":"foo/bar"}`, nil)
	rawBody, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("expected JSON Content-Type on -32601, got %q: %s", resp.Header.Get("Content-Type"), rawBody)
	}
	assertRPCErrorCode(t, string(rawBody), -32601)

	// resources/list and prompts/list return valid JSON-RPC responses
	for _, m := range []string{"resources/list", "prompts/list"} {
		resp = postMCP(`{"jsonrpc":"2.0","id":43,"method":"`+m+`"}`, nil)
		rawBody, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(rawBody), `"result"`) {
			t.Fatalf("%s status=%d body=%s", m, resp.StatusCode, rawBody)
		}
	}

	// Missing required params on tools/call returns JSON-RPC -32602 Invalid Params
	resp = postMCP(`{"jsonrpc":"2.0","id":44,"method":"tools/call"}`, nil)
	rawBody, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assertRPCErrorCode(t, string(rawBody), -32602)

	// Malformed JSON returns JSON-RPC -32700 Parse Error
	resp = postMCP(`{bad`, nil)
	rawBody, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assertRPCErrorCode(t, string(rawBody), -32700)

	// Cross-origin Origin header rejected with 403; loopback Origin allowed
	resp = postMCP(`{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Origin": "https://evil.example"})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin MCP request, got %d", resp.StatusCode)
	}
	resp = postMCP(`{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Origin": "http://127.0.0.1:3777"})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for loopback Origin MCP request, got %d", resp.StatusCode)
	}

	// GET on stateless /mcp returns 405 with Allow: POST
	getReq, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != http.MethodPost {
		t.Fatalf("GET /mcp status=%d Allow=%q", resp.StatusCode, resp.Header.Get("Allow"))
	}
}

func TestMCPToolTypesAliasesPreviousPathAndProjectDisambiguation(t *testing.T) {
	root := mcpRoot(t)
	session := connectMCP(t, root)
	ctx := context.Background()

	// All 5 canonical memory types succeed (case-insensitive)
	for _, typ := range []string{"user", "feedback", "project", "reference", "DECISION"} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "memory_save",
			Arguments: map[string]any{
				"name":    "type-check-" + strings.ToLower(typ),
				"type":    typ,
				"content": "saved via content alias for " + typ,
			},
		})
		if err != nil || res.IsError {
			t.Fatalf("type %q failed: err=%v toolErr=%s", typ, err, textOf(res))
		}
	}

	// Invalid type is rejected with clear enumeration message
	badTypeRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "memory_save",
		Arguments: map[string]any{"name": "bad-type", "type": "invalid_xyz", "body": "no"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !badTypeRes.IsError || !strings.Contains(textOf(badTypeRes), "decision") {
		t.Fatalf("expected invalid type error listing valid types, got isError=%v: %s", badTypeRes.IsError, textOf(badTypeRes))
	}

	// Disambiguate same-named memory across two projects via memory_get project parameter
	for proj, body := range map[string]string{
		"global":                  "global convention body",
		"proj-a-0123456789abcdef": "project-a specific body",
	} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "memory_save",
			Arguments: map[string]any{"project": proj, "name": "shared-name", "body": body},
		})
		if err != nil || res.IsError {
			t.Fatalf("save %s: %v %s", proj, err, textOf(res))
		}
	}

	getProjRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "memory_get",
		Arguments: map[string]any{"name": "shared-name", "project": "proj-a-0123456789abcdef"},
	})
	if err != nil || getProjRes.IsError || !strings.Contains(textOf(getProjRes), "project-a specific body") {
		t.Fatalf("project-scoped memory_get failed: err=%v body=%s", err, textOf(getProjRes))
	}

	// memory_search supports "q" alias, type filter, and pagination
	searchRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "memory_search",
		Arguments: map[string]any{"q": "content alias", "type": "decision", "limit": 5, "offset": 0},
	})
	if err != nil || searchRes.IsError || !strings.Contains(textOf(searchRes), "type-check-decision") {
		t.Fatalf("memory_search q alias + decision filter failed: err=%v body=%s", err, textOf(searchRes))
	}

	// memory_list supports type, limit, and offset
	listRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "memory_list",
		Arguments: map[string]any{"type": "decision", "limit": 2, "offset": 0},
	})
	if err != nil || listRes.IsError || !strings.Contains(textOf(listRes), "type-check-decision") {
		t.Fatalf("memory_list filter failed: err=%v body=%s", err, textOf(listRes))
	}

	// Rename via previousPath cleans up the old Markdown file
	saveOld, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "memory_save",
		Arguments: map[string]any{"name": "before-rename", "body": "initial"},
	})
	if err != nil || saveOld.IsError {
		t.Fatal(err)
	}
	var oldPayload struct{ Path string }
	_ = json.Unmarshal([]byte(textOf(saveOld)), &oldPayload)

	saveRenamed, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "memory_save",
		Arguments: map[string]any{
			"name":         "after-rename",
			"body":         "updated",
			"previousPath": oldPayload.Path,
		},
	})
	if err != nil || saveRenamed.IsError {
		t.Fatalf("rename save failed: %v %s", err, textOf(saveRenamed))
	}
	if _, statErr := os.Stat(oldPayload.Path); !os.IsNotExist(statErr) {
		t.Fatalf("expected old file %s to be deleted after rename, err=%v", oldPayload.Path, statErr)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mustJSONLine(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertRPCErrorCode(t *testing.T, raw string, wantCode int64) {
	t.Helper()
	var resp struct {
		Error *struct {
			Code    int64  `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("invalid JSON-RPC error response %q: %v", raw, err)
	}
	if resp.Error == nil || resp.Error.Code != wantCode {
		t.Fatalf("got error=%+v, want code %d in %s", resp.Error, wantCode, raw)
	}
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
