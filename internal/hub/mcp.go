// MCP surface: MarkToast exposes its memory repository to any MCP client.
package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
	"github.com/realchendahuang/MarkToast/internal/version"
)

// NewMCPServer builds the MCP server exposing MarkToast tools.
func NewMCPServer(root string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "marktoast", Version: version.Version}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_search",
		Description: "Search the user's durable memory repository. Returns ranked facts with snippets.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
		results, err := index.Search(root, args.Query, index.Options{
			Project: args.Project,
			Type:    args.Type,
			Limit:   args.Limit,
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
		return jsonResult(map[string]any{"results": out, "count": len(out)})
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_get",
		Description: "Read one memory fact by path or name, including its Markdown body.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getArgs) (*mcp.CallToolResult, any, error) {
		raw, fact, err := readMemory(root, args.Path, args.Name)
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
		Description: "Save a durable memory fact to the repository (writes Markdown, indexes it and commits).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args saveArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Name) == "" {
			return nil, nil, fmt.Errorf("name is required")
		}
		if strings.TrimSpace(args.Project) == "" {
			args.Project = "global"
		}
		path, err := store.WriteFact(root, args.Project, store.Fact{
			Name:        args.Name,
			Description: args.Description,
			Type:        args.Type,
			Body:        args.Body,
		})
		if err != nil {
			return nil, nil, err
		}
		if err := upsertIndex(root, path); err != nil {
			return nil, nil, err
		}
		if err := store.WriteIndexes(root); err != nil {
			return nil, nil, err
		}
		if gitx.Available() {
			if _, err := gitx.CommitAll(root, fmt.Sprintf("mem(%s): save %s", args.Project, args.Name)); err != nil {
				return nil, nil, err
			}
		}
		return jsonResult(map[string]any{"path": path, "project": args.Project, "name": args.Name})
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "memory_list",
		Description: "List memory facts, optionally filtered by project.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listArgs) (*mcp.CallToolResult, any, error) {
		facts, err := store.ListFacts(root)
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
			if args.Project != "" && f.Project != args.Project {
				continue
			}
			out = append(out, item{
				Path: f.Path, Name: f.Name, Description: f.Description,
				Project: f.Project, Type: f.Type, Updated: f.Updated.Format(time.RFC3339),
			})
		}
		return jsonResult(map[string]any{"memories": out, "count": len(out)})
	})

	return server
}

// RunMCPStdio runs the MCP server on stdio (for `marktoast mcp`).
func RunMCPStdio(ctx context.Context, root string) error {
	return NewMCPServer(root).Run(ctx, &mcp.StdioTransport{})
}

// MCPHTTPHandler returns a Streamable HTTP handler for the given server.
func MCPHTTPHandler(server *mcp.Server, token string) http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	if token == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

type searchArgs struct {
	Query   string `json:"query" jsonschema:"search terms"`
	Project string `json:"project,omitempty" jsonschema:"optional project key"`
	Type    string `json:"type,omitempty" jsonschema:"optional memory type"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max results (default 10)"`
}

type getArgs struct {
	Path string `json:"path,omitempty" jsonschema:"file path or repository-relative path"`
	Name string `json:"name,omitempty" jsonschema:"fact name, when path is unknown"`
}

type saveArgs struct {
	Project     string `json:"project,omitempty" jsonschema:"project key (default global)"`
	Name        string `json:"name" jsonschema:"fact name"`
	Description string `json:"description,omitempty" jsonschema:"one-line description"`
	Type        string `json:"type,omitempty" jsonschema:"user|feedback|project|reference"`
	Body        string `json:"body,omitempty" jsonschema:"Markdown body"`
}

type listArgs struct {
	Project string `json:"project,omitempty" jsonschema:"optional project key"`
}

func jsonResult(v any) (*mcp.CallToolResult, any, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, v, nil
}

// readMemory resolves a path or name to a fact.
func readMemory(root, path, name string) (string, store.Fact, error) {
	if path != "" {
		abs := path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, path)
		}
		if !underRoot(root, abs) {
			return "", store.Fact{}, fmt.Errorf("path escapes memory repository")
		}
		fact, err := store.ParseFile(abs)
		if err != nil {
			return "", store.Fact{}, err
		}
		fact.Project = projectFromPath(root, abs)
		raw, err := os.ReadFile(abs)
		if err != nil {
			return "", store.Fact{}, err
		}
		return string(raw), fact, nil
	}
	if name == "" {
		return "", store.Fact{}, fmt.Errorf("path or name is required")
	}
	facts, err := store.ListFacts(root)
	if err != nil {
		return "", store.Fact{}, err
	}
	for _, f := range facts {
		if f.Name == name {
			raw, err := os.ReadFile(f.Path)
			if err != nil {
				return "", store.Fact{}, err
			}
			return string(raw), f, nil
		}
	}
	return "", store.Fact{}, fmt.Errorf("memory %q not found", name)
}

func underRoot(root, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	return pathAbs == rootAbs || strings.HasPrefix(pathAbs, rootAbs+string(filepath.Separator))
}

func projectFromPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) >= 2 && parts[0] == "projects" {
		return parts[1]
	}
	if len(parts) >= 2 && parts[0] == "global" {
		return "global"
	}
	return ""
}

func upsertIndex(root, path string) error {
	d, err := index.Open(root)
	if err != nil {
		return err
	}
	defer d.Close()
	fact, err := store.ParseFile(path)
	if err != nil {
		return err
	}
	fact.Project = projectFromPath(root, path)
	return d.Upsert(fact)
}
