package mempalace

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP tool I/O types — small, JSON-Schema-tagged so the SDK can advertise them.

type recallIn struct {
	Query   string `json:"query" jsonschema:"the search query to find similar memories"`
	Top     int    `json:"top,omitempty" jsonschema:"max number of hits (default 10)"`
	Project string `json:"project,omitempty" jsonschema:"limit search to a single project"`
}

type recallOut struct {
	Hits []Hit `json:"hits"`
}

type rememberIn struct {
	Body    string `json:"body" jsonschema:"the memory text to store"`
	Project string `json:"project,omitempty" jsonschema:"project tag (e.g. ghostnode)"`
	Tags    string `json:"tags,omitempty" jsonschema:"comma-separated tags"`
	Source  string `json:"source,omitempty" jsonschema:"source label (cli, hook, hermes, claude); default 'mcp'"`
}

type rememberOut struct {
	ID int64 `json:"id"`
}

type forgetIn struct {
	ID int64 `json:"id" jsonschema:"id of the entry to remove"`
}

type forgetOut struct {
	Removed bool `json:"removed"`
}

type statsIn struct{}

// Serve runs an MCP streamable-HTTP server on the given address. The same
// mempalace store is shared across all requests (sqlite handles concurrency).
func Serve(ctx context.Context, addr string) error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "mempalace",
		Version: "v0.2.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mempalace_recall",
		Description: "Search the lifetime memory store by semantic similarity (or keyword fallback if no embedding API key is configured). Returns ranked hits with score, body, source, project, tags.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in recallIn) (*mcp.CallToolResult, recallOut, error) {
		hits, err := Recall(ctx, RecallOptions{Query: in.Query, TopK: in.Top, Project: in.Project})
		if err != nil {
			return nil, recallOut{}, err
		}
		return nil, recallOut{Hits: hits}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mempalace_remember",
		Description: "Store a new memory entry. If an embedding API key is configured (VOYAGE_API_KEY or OPENAI_API_KEY), the entry is embedded and findable by semantic recall.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in rememberIn) (*mcp.CallToolResult, rememberOut, error) {
		source := in.Source
		if source == "" {
			source = "mcp"
		}
		id, err := Remember(ctx, RememberOptions{
			Body:    in.Body,
			Source:  source,
			Project: in.Project,
			Tags:    in.Tags,
		})
		if err != nil {
			return nil, rememberOut{}, err
		}
		return nil, rememberOut{ID: id}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mempalace_forget",
		Description: "Delete a memory entry by ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in forgetIn) (*mcp.CallToolResult, forgetOut, error) {
		ok, err := Forget(ctx, in.ID)
		if err != nil {
			return nil, forgetOut{}, err
		}
		return nil, forgetOut{Removed: ok}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mempalace_stats",
		Description: "Return mempalace store stats (entry count, projects, db size).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in statsIn) (*mcp.CallToolResult, StatsResult, error) {
		s, err := Stats(ctx)
		if err != nil {
			return nil, StatsResult{}, err
		}
		return nil, s, nil
	})

	mux := http.NewServeMux()

	// Health check (used by bifrost + doctor)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	// MCP streamable-HTTP endpoint
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return server
	}, nil))

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown when ctx is cancelled.
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()
	fmt.Printf("mempalace MCP server listening on http://%s/mcp (health: /health)\n", addr)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
