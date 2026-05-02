package bifrost

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oyash01/yashigatakae/internal/hermes"
)

// Input/output structs for builtin tools. The MCP go-sdk auto-derives the
// JSON Schema from the struct tags via reflection.

type hermesEnqueueIn struct {
	Project        string `json:"project" jsonschema:"project label this task belongs to (required)"`
	Prompt         string `json:"prompt" jsonschema:"the prompt sent to claude -p (required)"`
	CWD            string `json:"cwd,omitempty" jsonschema:"working directory for the claude subprocess"`
	Note           string `json:"note,omitempty" jsonschema:"free-text note saved on the task row"`
	IdempotencyKey string `json:"idempotency_key,omitempty" jsonschema:"dedupe key — same key within 7 days returns the existing task id"`
	Priority       int    `json:"priority,omitempty" jsonschema:"higher number wins (default 5)"`
	MaxRetries     int    `json:"max_retries,omitempty" jsonschema:"attempts before DLQ (default 5)"`
	DependsOn      int64  `json:"depends_on,omitempty" jsonschema:"task id that must reach status=done before this one runs"`
}

type hermesEnqueueOut struct {
	ID         int64  `json:"id"`
	DedupeHit  bool   `json:"dedupe_hit"`
	Project    string `json:"project"`
}

type hermesStatusIn struct {
	ID int64 `json:"id" jsonschema:"hermes task id"`
}

// registerBuiltins adds tools that bifrost serves itself (no downstream MCP
// server needed). v0.12.0 ships hermes_enqueue + hermes_status so a Mac
// client can queue background work from inside Claude Code without SSH-ing
// to the VPS.
func registerBuiltins(server *mcp.Server) []string {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "hermes_enqueue",
		Description: "(builtin) Queue a Claude task on the always-on hermes worker. Returns the task id and a dedupe_hit flag (true if an existing task with the same idempotency_key was matched).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in hermesEnqueueIn) (*mcp.CallToolResult, hermesEnqueueOut, error) {
		id, hit, err := hermes.Enqueue(ctx, hermes.Task{
			Project:        in.Project,
			Prompt:         in.Prompt,
			CWD:            in.CWD,
			Note:           in.Note,
			IdempotencyKey: in.IdempotencyKey,
			Priority:       in.Priority,
			MaxRetries:     in.MaxRetries,
			DependencyID:   in.DependsOn,
		})
		if err != nil {
			return nil, hermesEnqueueOut{}, err
		}
		return nil, hermesEnqueueOut{ID: id, DedupeHit: hit, Project: in.Project}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "hermes_status",
		Description: "(builtin) Fetch a hermes task by id (status, retry count, log path, etc.).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in hermesStatusIn) (*mcp.CallToolResult, hermes.Task, error) {
		tasks, err := hermes.List(ctx, "", 500)
		if err != nil {
			return nil, hermes.Task{}, err
		}
		for _, t := range tasks {
			if t.ID == in.ID {
				return nil, t, nil
			}
		}
		return nil, hermes.Task{}, fmt.Errorf("task %d not found", in.ID)
	})

	return []string{"hermes_enqueue", "hermes_status"}
}
