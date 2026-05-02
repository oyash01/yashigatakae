// Package bifrost is the single MCP gateway. Claude Code connects to one
// HTTP endpoint here; bifrost fans out tool calls to N downstream MCP servers
// (mempalace, hermes, etc.). v0.2.0-rc3 ships with a single hardcoded
// downstream (mempalace); a yaml config arrives in v0.5 when hermes lands.
package bifrost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Downstream describes one MCP server to proxy to.
type Downstream struct {
	Name string // human label ("mempalace", "hermes")
	URL  string // streamable-HTTP endpoint, e.g. http://127.0.0.1:8765/mcp
}

// Config groups the gateway's runtime knobs.
type Config struct {
	Listen      string       // ":8443"
	Downstreams []Downstream // ordered by priority for tool name conflicts
	APIKey      string       // when set, requests must carry "Authorization: Bearer <key>"
}

// Help is what `yashigatakae bifrost` prints when invoked without subcommand.
func Help() string {
	return `bifrost — MCP gateway. One endpoint that fans out to N MCP servers.

  yashigatakae bifrost serve --addr :8443 [--mempalace URL] [--api-key KEY]

The Mac/Win client registers a single MCP server (bifrost) in
~/.claude/settings.json. Behind it, bifrost proxies recall/remember/forget/stats
to mempalace (and, in v0.5, hermes). One auth surface, one tool list,
no token bloat from multiple registrations.`
}

// Serve runs the gateway. Blocks until ctx is cancelled.
func Serve(ctx context.Context, cfg Config) error {
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8443"
	}
	if len(cfg.Downstreams) == 0 {
		return fmt.Errorf("no downstreams configured")
	}

	// Connect to each downstream, build the routing table.
	routes := newRouter()
	for _, d := range cfg.Downstreams {
		if err := routes.attach(ctx, d); err != nil {
			return fmt.Errorf("attach downstream %s: %w", d.Name, err)
		}
	}

	// Build the bifrost server, register one passthrough tool per downstream tool.
	// We use map[string]any as the input type so AddTool generates a JSON
	// Schema "object" — which is what the MCP spec requires for tool inputs.
	// We re-marshal the map to json.RawMessage when forwarding to the
	// downstream session, which expects Arguments as RawMessage.
	server := mcp.NewServer(&mcp.Implementation{Name: "bifrost", Version: "v0.2.0"}, nil)
	for _, t := range routes.tools {
		t := t // capture
		// Preserve the downstream's input schema so Claude sees the right hints.
		toolDef := &mcp.Tool{
			Name:        t.Tool.Name,
			Description: "(via " + t.DownstreamName + ") " + t.Tool.Description,
			InputSchema: t.Tool.InputSchema,
		}
		mcp.AddTool(server, toolDef, func(ctx context.Context, req *mcp.CallToolRequest, args map[string]any) (*mcp.CallToolResult, any, error) {
			raw, err := json.Marshal(args)
			if err != nil {
				return nil, nil, err
			}
			res, err := t.Session.CallTool(ctx, &mcp.CallToolParams{
				Name:      t.Tool.Name,
				Arguments: json.RawMessage(raw),
			})
			if err != nil {
				return nil, nil, err
			}
			return res, nil, nil
		})
	}
	fmt.Printf("  bifrost: %d tools registered across %d downstream(s)\n", len(routes.tools), len(cfg.Downstreams))
	for _, t := range routes.tools {
		fmt.Printf("    %s/%s\n", t.DownstreamName, t.Tool.Name)
	}

	// HTTP wiring: optional bearer-token gate, /health, /mcp.
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, nil)
	mux.Handle("/mcp", apiKeyGate(cfg.APIKey, mcpHandler))

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()
	fmt.Printf("bifrost listening on http://%s/mcp (health: /health)\n", cfg.Listen)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		routes.closeAll()
		return nil
	case err := <-errCh:
		routes.closeAll()
		return err
	}
}

// apiKeyGate is a thin middleware that enforces Bearer-token auth when
// cfg.APIKey is non-empty. Empty key = unauthenticated (only safe on
// loopback or behind a firewall).
func apiKeyGate(key string, next http.Handler) http.Handler {
	if key == "" {
		return next
	}
	expected := "Bearer " + key
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// router maintains the active downstream sessions + the unified tool table.
type router struct {
	mu       sync.Mutex
	sessions []*mcp.ClientSession
	tools    []routedTool
}

type routedTool struct {
	DownstreamName string
	Tool           *mcp.Tool
	Session        *mcp.ClientSession
}

func newRouter() *router { return &router{} }

func (r *router) attach(ctx context.Context, d Downstream) error {
	client := mcp.NewClient(&mcp.Implementation{Name: "bifrost-client", Version: "v0.2.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: d.URL}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect %s: %w", d.URL, err)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		_ = session.Close()
		return fmt.Errorf("list tools from %s: %w", d.URL, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions = append(r.sessions, session)
	for _, t := range tools.Tools {
		r.tools = append(r.tools, routedTool{
			DownstreamName: d.Name,
			Tool:           t,
			Session:        session,
		})
	}
	return nil
}

func (r *router) closeAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		_ = s.Close()
	}
}
