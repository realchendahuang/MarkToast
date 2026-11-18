// MCP surface: MarkToast exposes its memory repository to any MCP client over
// stdio (`marktoast mcp`) and Streamable HTTP (`POST /mcp`).
package hub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/realchendahuang/MarkToast/internal/config"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/service"
	"github.com/realchendahuang/MarkToast/internal/store"
	"github.com/realchendahuang/MarkToast/internal/version"
)

// maxMCPMessageBytes bounds a single inbound JSON-RPC message on both stdio and
// Streamable HTTP. 16 MiB comfortably accommodates the 2 MiB Markdown body
// ceiling (including JSON escaping and frontmatter) while preventing OOM on
// runaway streams.
const maxMCPMessageBytes = 16 << 20

const mcpServerInstructions = "MarkToast durable memory server. Use memory_search to find relevant past context before starting work, memory_get to read full Markdown bodies by path or name, memory_list to browse project facts, and memory_save to persist durable user preferences, feedback, project conventions, references, or decisions."

func boolPtr(v bool) *bool { return &v }

// NewMCPServer builds the MCP server exposing MarkToast tools for root.
func NewMCPServer(root string) *mcp.Server {
	autoCommit := true
	if cfg, err := config.Load(); err == nil {
		autoCommit = cfg.Git.AutoCommit
	}
	return newMCPServerWithOptions(Options{
		Root:             root,
		AutoCommit:       autoCommit,
		commitConfigured: true,
	})
}

func newMCPServerWithOptions(o Options) *mcp.Server {
	quietLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := mcp.NewServer(
		&mcp.Implementation{Name: "marktoast", Version: version.Version},
		&mcp.ServerOptions{
			Instructions:              mcpServerInstructions,
			Logger:                    quietLogger,
			SupportedProtocolVersions: []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"},
			Capabilities: &mcp.ServerCapabilities{
				Tools: &mcp.ToolCapabilities{},
			},
		},
	)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_search",
		Title:       "Search Memories",
		Description: "Full-text search across the user's durable memory repository (supports English and CJK). Returns ranked facts with snippets and scores.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Search Memories",
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
		q := strings.TrimSpace(args.Query)
		if q == "" {
			q = strings.TrimSpace(args.Q)
		}
		if q == "" {
			return nil, nil, fmt.Errorf("query is required")
		}
		typ, err := normalizeMemoryType(args.Type, "")
		if err != nil {
			return nil, nil, err
		}
		limit := clampInt(args.Limit, 10, 1, 100)
		offset := clampInt(args.Offset, 0, 0, maxPaginationOffset)
		results, total, err := index.SearchPage(o.Root, q, index.Options{
			Project: strings.TrimSpace(args.Project),
			Type:    typ,
			Limit:   limit,
			Offset:  offset,
			Sort:    strings.TrimSpace(args.Sort),
		})
		if err != nil {
			return nil, nil, err
		}
		type hit struct {
			Path        string  `json:"path"`
			Name        string  `json:"name"`
			Description string  `json:"description"`
			Project     string  `json:"project"`
			Type        string  `json:"type"`
			Updated     string  `json:"updated"`
			Snippet     string  `json:"snippet"`
			Score       float64 `json:"score"`
		}
		out := make([]hit, 0, len(results))
		for _, r := range results {
			out = append(out, hit{
				Path:        r.Fact.Path,
				Name:        r.Fact.Name,
				Description: r.Fact.Description,
				Project:     r.Fact.Project,
				Type:        r.Fact.Type,
				Updated:     r.Fact.Updated.Format(time.RFC3339),
				Snippet:     r.Snippet,
				Score:       r.Score,
			})
		}
		return jsonResult(map[string]any{
			"query":   q,
			"results": out,
			"count":   len(out),
			"total":   total,
			"offset":  offset,
			"limit":   limit,
		})
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_get",
		Title:       "Read Memory",
		Description: "Read one memory fact by file path or fact name (or id alias), optionally scoped to a project, returning metadata and full Markdown body.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Read Memory",
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getArgs) (*mcp.CallToolResult, any, error) {
		raw, fact, err := readMemoryInProject(o.Root, args.Path, args.nameOrID(), args.Project)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{
			"path":        fact.Path,
			"name":        fact.Name,
			"description": fact.Description,
			"project":     fact.Project,
			"type":        fact.Type,
			"updated":     fact.Updated.Format(time.RFC3339),
			"body":        fact.Body,
			"raw":         raw,
		})
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_save",
		Title:       "Save Memory",
		Description: "Create or update a durable Markdown memory fact, update the search index, and record a Git revision. Allowed types: user, feedback, project, reference, decision.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Save Memory",
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(false),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args saveArgs) (*mcp.CallToolResult, any, error) {
		res, err := saveMemoryFact(o, args)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(map[string]any{
			"path":          res.Path,
			"project":       res.Project,
			"name":          res.Name,
			"type":          res.Type,
			"commitWarning": res.CommitWarning,
		})
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_list",
		Title:       "List Memories",
		Description: "List memory fact metadata, optionally filtered by project and type with pagination.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "List Memories",
			ReadOnlyHint:   true,
			IdempotentHint: true,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listArgs) (*mcp.CallToolResult, any, error) {
		typ, err := normalizeMemoryType(args.Type, "")
		if err != nil {
			return nil, nil, err
		}
		limit := args.Limit
		if limit < 0 {
			limit = 0
		} else if limit > 500 {
			limit = 500
		}
		offset := clampInt(args.Offset, 0, 0, maxPaginationOffset)
		facts, total, err := index.ListPage(o.Root, index.ListOptions{
			Project: strings.TrimSpace(args.Project),
			Type:    typ,
			Limit:   limit,
			Offset:  offset,
			Sort:    strings.ToLower(strings.TrimSpace(args.Sort)),
		})
		if err != nil {
			return nil, nil, err
		}
		type item struct {
			Path        string `json:"path"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Project     string `json:"project"`
			Type        string `json:"type"`
			Updated     string `json:"updated"`
		}
		out := make([]item, 0, len(facts))
		for _, f := range facts {
			out = append(out, item{
				Path: f.Path, Name: f.Name, Description: f.Description,
				Project: f.Project, Type: f.Type, Updated: f.Updated.Format(time.RFC3339),
			})
		}
		return jsonResult(map[string]any{
			"memories": out,
			"count":    len(out),
			"total":    total,
			"offset":   offset,
			"limit":    limit,
		})
	})

	return server
}

// RunMCPStdio runs the MCP server on stdio (for `marktoast mcp`).
func RunMCPStdio(ctx context.Context, root string) error {
	return runMCPStream(ctx, NewMCPServer(root), os.Stdin, os.Stdout)
}

func runMCPStream(ctx context.Context, server *mcp.Server, r io.Reader, w io.Writer) error {
	return server.Run(ctx, &drainingStdioTransport{
		reader:   r,
		writer:   w,
		maxBytes: maxMCPMessageBytes,
	})
}

// MCPHTTPHandler returns a Streamable HTTP handler for the given server.
func MCPHTTPHandler(server *mcp.Server, token string) http.Handler {
	quietLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	streamHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		JSONResponse:        true,
		Logger:              quietLogger,
		MaxRequestBodyBytes: maxMCPMessageBytes,
	})

	if token == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			writeError(w, http.StatusServiceUnavailable, fmt.Errorf("MCP authentication is not configured"))
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !tokenEqual(bearerToken(r), token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, fmt.Errorf("unauthorized"))
			return
		}
		if !allowedMCPOrigin(r) {
			writeError(w, http.StatusForbidden, fmt.Errorf("cross-origin MCP request rejected"))
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
			return
		}

		// Normalise Content-Type and Accept so strict and minimal HTTP MCP
		// clients (curl, scripts, Go http.Client, SDKs) interoperate cleanly.
		hdr := r.Header.Clone()
		ct := strings.TrimSpace(hdr.Get("Content-Type"))
		if ct == "" {
			hdr.Set("Content-Type", "application/json")
		} else if mediaType, _, err := mime.ParseMediaType(ct); err != nil || !strings.EqualFold(mediaType, "application/json") {
			writeJSONRPCErrorHTTP(w, http.StatusUnsupportedMediaType, nil, jsonrpc.CodeInvalidRequest, "Content-Type must be application/json")
			return
		}
		ensureStreamableAcceptHeader(hdr)

		r2 := r.Clone(r.Context())
		r2.Header = hdr
		if r2.Body == nil {
			writeJSONRPCErrorHTTP(w, http.StatusBadRequest, nil, jsonrpc.CodeParseError, "parse error: empty request body")
			return
		}
		r2.Body = http.MaxBytesReader(w, r2.Body, maxMCPMessageBytes)
		body, err := io.ReadAll(r2.Body)
		_ = r2.Body.Close()
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				writeJSONRPCErrorHTTP(w, http.StatusRequestEntityTooLarge, nil, jsonrpc.CodeInvalidRequest, fmt.Sprintf("request body exceeds maximum size of %d bytes", mbe.Limit))
				return
			}
			writeJSONRPCErrorHTTP(w, http.StatusBadRequest, nil, jsonrpc.CodeParseError, "failed to read request body")
			return
		}

		trimmed := bytes.TrimSpace(body)
		if len(trimmed) == 0 {
			writeJSONRPCErrorHTTP(w, http.StatusBadRequest, nil, jsonrpc.CodeParseError, "parse error: empty request body")
			return
		}

		rewritten, handled := preprocessHTTPJSONRPC(w, trimmed)
		if handled {
			return
		}
		r2.Body = io.NopCloser(bytes.NewReader(rewritten))
		r2.ContentLength = int64(len(rewritten))
		streamHandler.ServeHTTP(w, r2)
	})
}

func ensureStreamableAcceptHeader(hdr http.Header) {
	vals := hdr.Values("Accept")
	if len(vals) == 0 {
		hdr.Set("Accept", "application/json, text/event-stream")
		return
	}
	var jsonOK, streamOK bool
	for _, v := range vals {
		for _, part := range strings.Split(v, ",") {
			base, _, _ := strings.Cut(strings.TrimSpace(part), ";")
			switch strings.ToLower(strings.TrimSpace(base)) {
			case "application/json", "application/*":
				jsonOK = true
			case "text/event-stream", "text/*":
				streamOK = true
			case "*/*":
				jsonOK, streamOK = true, true
			}
		}
	}
	if jsonOK && !streamOK {
		hdr.Set("Accept", "application/json, text/event-stream")
	}
}

// supportedHTTPMCPMethods enumerates the standard JSON-RPC methods handled by
// our stateless MCP server. Any other call receives a standard JSON-RPC -32601
// Method Not Found error object rather than a plain-text HTTP 400.
var supportedHTTPMCPMethods = map[string]bool{
	"initialize":               true,
	"ping":                     true,
	"tools/list":               true,
	"tools/call":               true,
	"resources/list":           true,
	"resources/templates/list": true,
	"resources/read":           true,
	"prompts/list":             true,
	"prompts/get":              true,
	"logging/setLevel":         true,
	"server/discover":          true,
}

var methodsRequiringObjectParams = map[string]bool{
	"initialize":       true,
	"tools/call":       true,
	"resources/read":   true,
	"prompts/get":      true,
	"logging/setLevel": true,
}

func isSDKSupportedNotification(method string, params json.RawMessage) bool {
	switch method {
	case "notifications/initialized", "notifications/cancelled", "notifications/roots/list_changed":
		return true
	case "notifications/progress":
		return isJSONObject(params)
	default:
		return false
	}
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}'
}

func validJSONRPCID(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return false
	}
	var v any
	if err := json.Unmarshal(trimmed, &v); err != nil {
		return false
	}
	id, err := jsonrpc.MakeID(v)
	return err == nil && id.IsValid()
}

// preprocessHTTPJSONRPC validates inbound JSON-RPC payloads before handing them
// to the go-sdk StreamableHTTPHandler so that:
//  1. Unknown notifications (messages without an "id") never produce error
//     responses and return 202 Accepted per MCP Streamable HTTP §2.1.4.
//  2. Unknown method calls return standard JSON-RPC -32601 Method Not Found.
//  3. Malformed JSON or missing required params return standard JSON-RPC
//     -32700 / -32600 / -32602 error objects instead of plain-text 400s.
func preprocessHTTPJSONRPC(w http.ResponseWriter, body []byte) ([]byte, bool) {
	if body[0] == '[' {
		var rawBatch []json.RawMessage
		if err := json.Unmarshal(body, &rawBatch); err != nil {
			writeJSONRPCErrorHTTP(w, http.StatusBadRequest, nil, jsonrpc.CodeParseError, "parse error: "+err.Error())
			return nil, true
		}
		if len(rawBatch) == 0 {
			writeJSONRPCErrorHTTP(w, http.StatusBadRequest, nil, jsonrpc.CodeInvalidRequest, "invalid request: empty batch")
			return nil, true
		}
		forward := make([]json.RawMessage, 0, len(rawBatch))
		var immediateResponses []json.RawMessage
		for _, item := range rawBatch {
			keep, errResp := inspectSingleRPCMessage(item)
			if errResp != nil {
				immediateResponses = append(immediateResponses, errResp)
				continue
			}
			if keep {
				forward = append(forward, item)
			}
		}
		if len(forward) == 0 {
			if len(immediateResponses) == 0 {
				w.WriteHeader(http.StatusAccepted)
				return nil, true
			}
			out, _ := json.Marshal(immediateResponses)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(out)
			return nil, true
		}
		if len(immediateResponses) == 0 {
			rewritten, err := json.Marshal(forward)
			if err != nil {
				return body, false
			}
			return rewritten, false
		}
		// Mixed batch with both valid SDK calls and pre-rejected calls: return
		// the error if single, otherwise let valid calls run when no errors.
		out, _ := json.Marshal(immediateResponses)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)
		return nil, true
	}

	keep, errResp := inspectSingleRPCMessage(body)
	if errResp != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(errResp)
		return nil, true
	}
	if !keep {
		w.WriteHeader(http.StatusAccepted)
		return nil, true
	}
	return body, false
}

// inspectSingleRPCMessage inspects one JSON-RPC message.
// Returns (keepForSDK, errorResponseJSON).
func inspectSingleRPCMessage(raw []byte) (bool, json.RawMessage) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false, buildJSONRPCErrorBytes(nil, jsonrpc.CodeParseError, "parse error: "+err.Error())
	}
	var ver string
	if rawVer, ok := fields["jsonrpc"]; !ok || json.Unmarshal(rawVer, &ver) != nil || ver != "2.0" {
		return false, buildJSONRPCErrorBytes(fields["id"], jsonrpc.CodeInvalidRequest, `invalid request: jsonrpc must be "2.0"`)
	}

	rawMethod, hasMethod := fields["method"]
	if !hasMethod {
		// Client response frame (has result or error).
		if _, hasRes := fields["result"]; hasRes {
			return true, nil
		}
		if _, hasErr := fields["error"]; hasErr {
			return true, nil
		}
		return false, buildJSONRPCErrorBytes(fields["id"], jsonrpc.CodeInvalidRequest, "invalid request: missing method")
	}

	var method string
	if err := json.Unmarshal(rawMethod, &method); err != nil || strings.TrimSpace(method) == "" {
		return false, buildJSONRPCErrorBytes(fields["id"], jsonrpc.CodeInvalidRequest, "invalid request: method must be a non-empty string")
	}

	rawID, hasID := fields["id"]
	if !hasID {
		// JSON-RPC 2.0 Notification (no "id" member): MUST NOT return an error.
		if isSDKSupportedNotification(method, fields["params"]) {
			return true, nil
		}
		return false, nil
	}

	if !validJSONRPCID(rawID) {
		return false, buildJSONRPCErrorBytes(nil, jsonrpc.CodeInvalidRequest, "invalid request: id must be a string or number")
	}
	if strings.HasPrefix(method, "notifications/") {
		return false, buildJSONRPCErrorBytes(rawID, jsonrpc.CodeInvalidRequest, fmt.Sprintf("invalid request: notification %q must not include id", method))
	}
	if !supportedHTTPMCPMethods[method] {
		return false, buildJSONRPCErrorBytes(rawID, jsonrpc.CodeMethodNotFound, fmt.Sprintf("method not found: %s", method))
	}
	if methodsRequiringObjectParams[method] && !isJSONObject(fields["params"]) {
		return false, buildJSONRPCErrorBytes(rawID, jsonrpc.CodeInvalidParams, fmt.Sprintf("invalid params for %s: expected JSON object", method))
	}
	return true, nil
}

func buildJSONRPCErrorBytes(rawID json.RawMessage, code int64, message string) json.RawMessage {
	idVal := json.RawMessage("null")
	if validJSONRPCID(rawID) {
		idVal = bytes.TrimSpace(rawID)
	}
	payload := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   struct {
			Code    int64  `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{
		JSONRPC: "2.0",
		ID:      idVal,
	}
	payload.Error.Code = code
	payload.Error.Message = message
	data, _ := json.Marshal(payload)
	return data
}

func writeJSONRPCErrorHTTP(w http.ResponseWriter, httpStatus int, rawID json.RawMessage, code int64, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	_, _ = w.Write(buildJSONRPCErrorBytes(rawID, code, message))
}

// drainingStdioTransport is an MCP newline-delimited JSON transport that:
//   - buffers single frames up to maxBytes (16 MiB, avoiding 64 KiB Scanner truncation),
//   - serialises all outbound writes atomically with a mutex,
//   - silently absorbs unknown JSON-RPC notifications without error replies,
//   - gates sequential call delivery while allowing out-of-band cancellation
//     notifications through immediately, and
//   - drains all in-flight calls before surfacing EOF when stdin is piped.
type drainingStdioTransport struct {
	reader   io.Reader
	writer   io.Writer
	maxBytes int
}

func (t *drainingStdioTransport) Connect(context.Context) (mcp.Connection, error) {
	limit := t.maxBytes
	if limit <= 0 {
		limit = maxMCPMessageBytes
	}
	c := &drainingStdioConn{
		reader:      t.reader,
		writer:      t.writer,
		maxBytes:    limit,
		activeCalls: make(map[jsonrpc.ID]struct{}),
		batches:     make(map[jsonrpc.ID]*stdioBatch),
	}
	c.cond = sync.NewCond(&c.mu)
	go c.readLoop()
	return c, nil
}

type inboundEnvelope struct {
	msgs    []jsonrpc.Message
	isBatch bool
	rawErr  []byte
}

type stdioBatch struct {
	indexByID map[jsonrpc.ID]int
	remaining int
	responses []*jsonrpc.Response
}

type drainingStdioConn struct {
	reader   io.Reader
	writer   io.Writer
	maxBytes int

	writeMu sync.Mutex

	mu          sync.Mutex
	cond        *sync.Cond
	inbox       []inboundEnvelope
	pending     []jsonrpc.Message // remaining messages of an active batch
	activeCalls map[jsonrpc.ID]struct{}
	batches     map[jsonrpc.ID]*stdioBatch
	readDone    bool
	readErr     error
	closed      bool
}

func (c *drainingStdioConn) SessionID() string { return "" }

var errFrameTooLong = errors.New("JSON-RPC frame exceeds maximum allowed length")

func readLineLimited(br *bufio.Reader, maxBytes int) ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, err := br.ReadSlice('\n')
		if buf.Len()+len(chunk) > maxBytes {
			// Drain the rest of the oversized line so subsequent frames remain aligned.
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = br.ReadSlice('\n')
			}
			return nil, errFrameTooLong
		}
		buf.Write(chunk)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) && buf.Len() > 0 {
				return buf.Bytes(), nil
			}
			return nil, err
		}
		return buf.Bytes(), nil
	}
}

func (c *drainingStdioConn) readLoop() {
	br := bufio.NewReaderSize(c.reader, 64*1024)
	for {
		line, err := readLineLimited(br, c.maxBytes)
		if errors.Is(err, errFrameTooLong) {
			c.enqueueEnvelope(inboundEnvelope{
				rawErr: buildJSONRPCErrorBytes(nil, jsonrpc.CodeInvalidRequest, err.Error()),
			})
			continue
		}
		if err != nil {
			c.mu.Lock()
			c.readDone = true
			c.readErr = err
			c.cond.Broadcast()
			c.mu.Unlock()
			return
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}

		env, immediateOut, ok := c.decodeInboundFrame(trimmed)
		if len(immediateOut) > 0 {
			c.enqueueEnvelope(inboundEnvelope{rawErr: immediateOut})
			continue
		}
		if !ok || len(env.msgs) == 0 {
			continue
		}
		if !c.enqueueEnvelope(env) {
			return
		}
	}
}

func (c *drainingStdioConn) enqueueEnvelope(env inboundEnvelope) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.inbox = append(c.inbox, env)
	c.cond.Broadcast()
	return true
}

func (c *drainingStdioConn) decodeInboundFrame(trimmed []byte) (inboundEnvelope, []byte, bool) {
	if trimmed[0] == '[' {
		var rawItems []json.RawMessage
		if err := json.Unmarshal(trimmed, &rawItems); err != nil {
			return inboundEnvelope{}, buildJSONRPCErrorBytes(nil, jsonrpc.CodeParseError, "parse error: "+err.Error()), false
		}
		if len(rawItems) == 0 {
			return inboundEnvelope{}, buildJSONRPCErrorBytes(nil, jsonrpc.CodeInvalidRequest, "invalid request: empty batch"), false
		}
		var msgs []jsonrpc.Message
		seenIDs := make(map[jsonrpc.ID]bool)
		for _, item := range rawItems {
			msg, errBytes, keep := decodeOneStdioMessage(item)
			if errBytes != nil {
				return inboundEnvelope{}, errBytes, false
			}
			if !keep || msg == nil {
				continue
			}
			if req, ok := msg.(*jsonrpc.Request); ok && req.IsCall() {
				if seenIDs[req.ID] {
					return inboundEnvelope{}, buildJSONRPCErrorBytes(nil, jsonrpc.CodeInvalidRequest, "duplicate request id in batch"), false
				}
				seenIDs[req.ID] = true
			}
			msgs = append(msgs, msg)
		}
		return inboundEnvelope{msgs: msgs, isBatch: true}, nil, len(msgs) > 0
	}

	msg, errBytes, keep := decodeOneStdioMessage(trimmed)
	if errBytes != nil {
		return inboundEnvelope{}, errBytes, false
	}
	if !keep || msg == nil {
		return inboundEnvelope{}, nil, false
	}
	return inboundEnvelope{msgs: []jsonrpc.Message{msg}}, nil, true
}

func decodeOneStdioMessage(raw []byte) (jsonrpc.Message, []byte, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, buildJSONRPCErrorBytes(nil, jsonrpc.CodeParseError, "parse error: "+err.Error()), false
	}
	var ver string
	if rawVer, ok := fields["jsonrpc"]; !ok || json.Unmarshal(rawVer, &ver) != nil || ver != "2.0" {
		return nil, buildJSONRPCErrorBytes(fields["id"], jsonrpc.CodeInvalidRequest, `invalid request: jsonrpc must be "2.0"`), false
	}

	if rawMethod, hasMethod := fields["method"]; hasMethod {
		var method string
		if err := json.Unmarshal(rawMethod, &method); err != nil || strings.TrimSpace(method) == "" {
			return nil, buildJSONRPCErrorBytes(fields["id"], jsonrpc.CodeInvalidRequest, "invalid request: method must be a non-empty string"), false
		}
		rawID, hasID := fields["id"]
		if !hasID {
			// JSON-RPC 2.0 Notification: ignore unrecognised notifications silently.
			if !isSDKSupportedNotification(method, fields["params"]) {
				return nil, nil, false
			}
		} else {
			if !validJSONRPCID(rawID) {
				return nil, buildJSONRPCErrorBytes(nil, jsonrpc.CodeInvalidRequest, "invalid request: id must be a string or number"), false
			}
			if strings.HasPrefix(method, "notifications/") {
				return nil, buildJSONRPCErrorBytes(rawID, jsonrpc.CodeInvalidRequest, fmt.Sprintf("invalid request: notification %q must not include id", method)), false
			}
			if !supportedHTTPMCPMethods[method] {
				return nil, buildJSONRPCErrorBytes(rawID, jsonrpc.CodeMethodNotFound, fmt.Sprintf("method not found: %s", method)), false
			}
			if methodsRequiringObjectParams[method] && !isJSONObject(fields["params"]) {
				return nil, buildJSONRPCErrorBytes(rawID, jsonrpc.CodeInvalidParams, fmt.Sprintf("invalid params for %s: expected JSON object", method)), false
			}
		}
	}

	msg, err := jsonrpc.DecodeMessage(raw)
	if err != nil {
		return nil, buildJSONRPCErrorBytes(fields["id"], jsonrpc.CodeInvalidRequest, err.Error()), false
	}
	return msg, nil, true
}

func isOutOfBandImmediate(env inboundEnvelope) bool {
	if env.isBatch || len(env.msgs) != 1 {
		return false
	}
	switch m := env.msgs[0].(type) {
	case *jsonrpc.Response:
		return true
	case *jsonrpc.Request:
		return !m.IsCall()
	default:
		return false
	}
}

func (c *drainingStdioConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	stopWake := context.AfterFunc(ctx, func() {
		c.mu.Lock()
		c.cond.Broadcast()
		c.mu.Unlock()
	})
	defer stopWake()

	c.mu.Lock()
	defer c.mu.Unlock()

	for {
		if c.closed {
			return nil, io.EOF
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Deliver remaining messages of an already-started batch without gating.
		if len(c.pending) > 0 {
			next := c.pending[0]
			c.pending = c.pending[1:]
			return next, nil
		}

		// When a call is in flight, still let notifications (e.g. cancelled) and
		// client responses bypass the queue immediately.
		if len(c.activeCalls) > 0 && len(c.inbox) > 0 {
			for i, env := range c.inbox {
				if isOutOfBandImmediate(env) {
					c.inbox = append(c.inbox[:i], c.inbox[i+1:]...)
					return env.msgs[0], nil
				}
			}
		}

		if len(c.activeCalls) == 0 && len(c.inbox) > 0 {
			env := c.inbox[0]
			c.inbox = c.inbox[1:]
			if len(env.rawErr) > 0 {
				rawErr := env.rawErr
				c.mu.Unlock()
				_ = c.writeRawBytes(rawErr)
				c.mu.Lock()
				continue
			}
			c.registerEnvelopeLocked(env)
			if len(env.msgs) > 1 {
				c.pending = append(c.pending, env.msgs[1:]...)
			}
			return env.msgs[0], nil
		}

		if c.readDone && len(c.inbox) == 0 && len(c.activeCalls) == 0 {
			if c.readErr != nil {
				return nil, c.readErr
			}
			return nil, io.EOF
		}

		c.cond.Wait()
	}
}

func (c *drainingStdioConn) registerEnvelopeLocked(env inboundEnvelope) {
	var batch *stdioBatch
	for _, m := range env.msgs {
		req, ok := m.(*jsonrpc.Request)
		if !ok || !req.IsCall() {
			continue
		}
		c.activeCalls[req.ID] = struct{}{}
		if env.isBatch {
			if batch == nil {
				batch = &stdioBatch{indexByID: make(map[jsonrpc.ID]int)}
			}
			batch.indexByID[req.ID] = len(batch.responses)
			batch.responses = append(batch.responses, nil)
			batch.remaining++
			c.batches[req.ID] = batch
		}
	}
}

func (c *drainingStdioConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if resp, ok := msg.(*jsonrpc.Response); ok && resp.ID.IsValid() {
		c.mu.Lock()
		batch, inBatch := c.batches[resp.ID]
		if inBatch {
			delete(c.batches, resp.ID)
			delete(c.activeCalls, resp.ID)
			if idx, ok := batch.indexByID[resp.ID]; ok {
				batch.responses[idx] = resp
				delete(batch.indexByID, resp.ID)
				batch.remaining--
			}
			ready := batch.remaining <= 0
			var batchResps []*jsonrpc.Response
			if ready {
				batchResps = batch.responses
			}
			c.mu.Unlock()

			if !ready {
				return nil
			}
			rawList := make([]json.RawMessage, 0, len(batchResps))
			for _, r := range batchResps {
				if r == nil {
					continue
				}
				b, err := jsonrpc.EncodeMessage(r)
				if err != nil {
					return err
				}
				rawList = append(rawList, b)
			}
			encoded, err := json.Marshal(rawList)
			if err != nil {
				return err
			}
			writeErr := c.writeRawBytes(encoded)
			c.mu.Lock()
			c.cond.Broadcast()
			c.mu.Unlock()
			return writeErr
		}
		c.mu.Unlock()

		data, err := jsonrpc.EncodeMessage(msg)
		if err != nil {
			return err
		}
		writeErr := c.writeRawBytes(data)

		c.mu.Lock()
		delete(c.activeCalls, resp.ID)
		c.cond.Broadcast()
		c.mu.Unlock()
		return writeErr
	}

	data, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return err
	}
	return c.writeRawBytes(data)
}

func (c *drainingStdioConn) writeRawBytes(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	line := make([]byte, len(payload)+1)
	copy(line, payload)
	line[len(payload)] = '\n'
	_, err := c.writer.Write(line)
	return err
}

func (c *drainingStdioConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.cond.Broadcast()
	c.mu.Unlock()
	return nil
}

type searchArgs struct {
	Query   string `json:"query,omitempty" jsonschema:"Search keywords or phrase (supports English and CJK)"`
	Q       string `json:"q,omitempty" jsonschema:"Search keywords (alias for query)"`
	Project string `json:"project,omitempty" jsonschema:"Optional project key filter (for example global)"`
	Type    string `json:"type,omitempty" jsonschema:"Optional memory type filter (user, feedback, project, reference, or decision)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum results to return (1-100, default 10)"`
	Offset  int    `json:"offset,omitempty" jsonschema:"Pagination offset (0 or positive integer, default 0)"`
	Sort    string `json:"sort,omitempty" jsonschema:"Sort order (relevance, updated, or title)"`
}

type getArgs struct {
	Path string `json:"path,omitempty" jsonschema:"File path or repository-relative Markdown path"`
	Name string `json:"name,omitempty" jsonschema:"Memory fact name when path is unknown"`
	// ID is an alias for Name. Clients routinely call the fact name an id;
	// accepting both keeps the tool callable instead of failing validation on
	// an unexpected property.
	ID      string `json:"id,omitempty" jsonschema:"Memory fact name (alias for name)"`
	Project string `json:"project,omitempty" jsonschema:"Optional project key to disambiguate facts sharing a name"`
}

// nameOrID returns the fact name, falling back to the id alias. A client that
// sends both is taken at its word on name.
func (a getArgs) nameOrID() string {
	if strings.TrimSpace(a.Name) != "" {
		return strings.TrimSpace(a.Name)
	}
	return strings.TrimSpace(a.ID)
}

type saveArgs struct {
	Project      string `json:"project,omitempty" jsonschema:"Project key (defaults to global)"`
	Name         string `json:"name" jsonschema:"Unique, concise memory fact name"`
	Description  string `json:"description,omitempty" jsonschema:"One-line summary shown in memory indexes"`
	Type         string `json:"type,omitempty" jsonschema:"Memory category (user, feedback, project, reference, or decision; default reference)"`
	Body         string `json:"body,omitempty" jsonschema:"Markdown content of the memory"`
	Content      string `json:"content,omitempty" jsonschema:"Markdown content (alias for body)"`
	PreviousPath string `json:"previousPath,omitempty" jsonschema:"Optional previous memory file path to remove when renaming or moving"`
}

type listArgs struct {
	Project string `json:"project,omitempty" jsonschema:"Optional project key filter"`
	Type    string `json:"type,omitempty" jsonschema:"Optional memory type filter (user, feedback, project, reference, or decision)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum memories to return (0 for all, max 500)"`
	Offset  int    `json:"offset,omitempty" jsonschema:"Pagination offset (0 or positive integer, default 0)"`
	Sort    string `json:"sort,omitempty" jsonschema:"Sort order (title or updated)"`
}

func jsonResult(v any) (*mcp.CallToolResult, any, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, v, nil
}

// readMemory resolves a path or name to a fact across all projects.
func readMemory(root, path, name string) (string, store.Fact, error) {
	return readMemoryInProject(root, path, name, "")
}

// readMemoryInProject resolves a path or name (optionally scoped to project) to a fact.
func readMemoryInProject(root, path, name, project string) (string, store.Fact, error) {
	path = strings.TrimSpace(path)
	name = strings.TrimSpace(name)
	project = strings.TrimSpace(project)
	svc := service.New(root)
	if path != "" {
		raw, fact, err := svc.ReadMemoryFile(path)
		if err != nil {
			return "", store.Fact{}, err
		}
		if project != "" && fact.Project != project {
			return "", store.Fact{}, fmt.Errorf("%w: memory %q is in project %q, not %q", errMemoryNotFound, path, fact.Project, project)
		}
		return raw, fact, nil
	}
	if name == "" {
		return "", store.Fact{}, fmt.Errorf("path, name or id is required")
	}
	if project != "" {
		if err := store.ValidateProject(project); err != nil {
			return "", store.Fact{}, err
		}
	}
	facts, err := store.ListFacts(root)
	if err != nil {
		return "", store.Fact{}, err
	}
	var ciMatch *store.Fact
	for i := range facts {
		f := &facts[i]
		if project != "" && f.Project != project {
			continue
		}
		if f.Name == name {
			return svc.ReadMemoryFile(f.Path)
		}
		if ciMatch == nil && strings.EqualFold(f.Name, name) {
			ciMatch = f
		}
	}
	if ciMatch != nil {
		return svc.ReadMemoryFile(ciMatch.Path)
	}
	if project != "" {
		return "", store.Fact{}, fmt.Errorf("%w: memory %q not found in project %q", errMemoryNotFound, name, project)
	}
	return "", store.Fact{}, fmt.Errorf("%w: memory %q not found", errMemoryNotFound, name)
}
