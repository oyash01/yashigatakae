// Command yashigatakae is the Claude Code orchestrator + lifetime memory + cross-device continuity tool.
// See https://github.com/oyash01/yashigatakae for the full design.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/oyash01/yashigatakae/internal/bifrost"
	"github.com/oyash01/yashigatakae/internal/caveman"
	"github.com/oyash01/yashigatakae/internal/doctor"
	"github.com/oyash01/yashigatakae/internal/graphify"
	"github.com/oyash01/yashigatakae/internal/hermes"
	"github.com/oyash01/yashigatakae/internal/kintsugi"
	"github.com/oyash01/yashigatakae/internal/mempalace"
	"github.com/oyash01/yashigatakae/internal/secrets"
	"github.com/oyash01/yashigatakae/internal/state"
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X main.Version=v0.1.0".
var Version = "v0.1.0-dev"

func main() {
	root := &cobra.Command{
		Use:           "yashigatakae",
		Short:         "Claude Code orchestrator + lifetime memory + cross-device continuity",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.AddCommand(newInitCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newCavemanCmd())
	root.AddCommand(newStateCmd())
	root.AddCommand(newSecretsCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newUpgradeCmd())
	root.AddCommand(newMempalaceCmd())
	root.AddCommand(newBifrostCmd())
	root.AddCommand(notYet("graphify", "v0.4", graphify.Help))
	root.AddCommand(notYet("hermes", "v0.5", hermes.Help))
	root.AddCommand(notYet("handoff", "v0.3", kintsugi.HelpHandoff))
	root.AddCommand(notYet("resume", "v0.3", kintsugi.HelpResume))
	root.AddCommand(notYet("sessions", "v0.3", kintsugi.HelpSessions))
	root.AddCommand(notYet("link", "v0.6", state.HelpLink))

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newInitCmd() *cobra.Command {
	var vps, github, skipGstack bool
	var stateRepo string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Bootstrap this machine: gstack, caveman hooks, state render, MCP placeholder",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := state.InitOptions{
				VPS:            vps,
				GitHub:         github,
				LocalStateRepo: stateRepo,
				SkipGstack:     skipGstack,
			}
			return state.Run(opts)
		},
	}

	cmd.Flags().BoolVar(&vps, "vps", false, "Run VPS-side bootstrap (mempalace, hermes, bifrost, kintsugi relay)")
	cmd.Flags().BoolVar(&github, "github", false, "Create GitHub repos (state, memory-mirror)")
	cmd.Flags().StringVar(&stateRepo, "state-repo", "", "Path to local yashigatakae-state checkout (overrides github fetch)")
	cmd.Flags().BoolVar(&skipGstack, "skip-gstack", false, "Skip the gstack ./setup step (useful for dogfood / CI; gstack still expected to be installed separately)")

	return cmd
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Verify everything is wired correctly; print fixits",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doctor.Run()
		},
	}
}

func newCavemanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "caveman <lite|full|ultra|off>",
		Short: "Switch caveman compression level",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return caveman.SetLevel(args[0])
		},
	}
	return cmd
}

func newStateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Manage the yashigatakae-state repo (skills/hooks/templates)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "render",
		Short: "Render templates from state repo into ~/.claude/",
		RunE: func(cmd *cobra.Command, args []string) error {
			return state.Render()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "pull",
		Short: "Git pull the state repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			return state.Pull()
		},
	})
	return cmd
}

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage encrypted secrets.env synced via SSH from VPS",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "pull",
		Short: "Pull ~/.yashigatakae/secrets.env from VPS",
		RunE: func(cmd *cobra.Command, args []string) error {
			return secrets.Pull()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "push",
		Short: "Push ~/.yashigatakae/secrets.env to VPS",
		RunE: func(cmd *cobra.Command, args []string) error {
			return secrets.Push()
		},
	})
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print machine cluster, sync state, drift, VPS health",
		RunE: func(cmd *cobra.Command, args []string) error {
			return doctor.Status()
		},
	}
}

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Push/pull static state and diff memory across machines",
		RunE: func(cmd *cobra.Command, args []string) error {
			return state.Sync()
		},
	}
}

func newUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Self-update yashigatakae, then upgrade gstack and skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("upgrade: ships in v0.6 — for now, re-run install.sh / install.ps1")
			return nil
		},
	}
}

// notYet builds a placeholder cobra command that explains when a subsystem ships.
func notYet(name, milestone string, help func() string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("(stub — ships in %s) %s", milestone, name),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("[%s] not yet implemented — scheduled for %s.\n\n%s\n", name, milestone, help())
			return nil
		},
	}
}

func newMempalaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mempalace",
		Short: "Lifetime semantic memory store (remember / recall / forget / stats)",
	}

	// remember
	{
		var project, tags, source string
		sub := &cobra.Command{
			Use:   "remember <text>",
			Short: "Store a memory entry (semantically embedded if API key is set)",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				body := joinArgs(args)
				id, err := mempalace.Remember(context.Background(), mempalace.RememberOptions{
					Body:    body,
					Source:  source,
					Project: project,
					Tags:    tags,
				})
				if err != nil {
					return err
				}
				fmt.Printf("✓ remembered #%d\n", id)
				return nil
			},
		}
		sub.Flags().StringVar(&project, "project", "", "Project tag (e.g. ghostnode)")
		sub.Flags().StringVar(&tags, "tags", "", "Comma-separated tags")
		sub.Flags().StringVar(&source, "source", "cli", "Source label (cli, hook, hermes, etc.)")
		cmd.AddCommand(sub)
	}

	// recall
	{
		var top int
		var project string
		var asJSON bool
		sub := &cobra.Command{
			Use:   "recall <query>",
			Short: "Search memory by semantic similarity (or keyword fallback)",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				query := joinArgs(args)
				hits, err := mempalace.Recall(context.Background(), mempalace.RecallOptions{
					Query:   query,
					TopK:    top,
					Project: project,
				})
				if err != nil {
					return err
				}
				if asJSON {
					b, _ := json.MarshalIndent(hits, "", "  ")
					fmt.Println(string(b))
					return nil
				}
				if len(hits) == 0 {
					fmt.Println("(no hits)")
					return nil
				}
				for _, h := range hits {
					body := h.Body
					if len(body) > 200 {
						body = body[:200] + "…"
					}
					fmt.Printf("  #%-5d  %.3f  [%s/%s]  %s\n", h.ID, h.Score, h.Source, h.Project, body)
				}
				return nil
			},
		}
		sub.Flags().IntVar(&top, "top", 10, "Max number of hits to return")
		sub.Flags().StringVar(&project, "project", "", "Limit search to one project")
		sub.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
		cmd.AddCommand(sub)
	}

	// forget
	{
		sub := &cobra.Command{
			Use:   "forget <id>",
			Short: "Delete a memory entry by ID",
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				id, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					return fmt.Errorf("id must be integer: %w", err)
				}
				ok, err := mempalace.Forget(context.Background(), id)
				if err != nil {
					return err
				}
				if !ok {
					fmt.Printf("? no entry with id=%d\n", id)
					return nil
				}
				fmt.Printf("✓ forgot #%d\n", id)
				return nil
			},
		}
		cmd.AddCommand(sub)
	}

	// stats
	{
		var asJSON bool
		sub := &cobra.Command{
			Use:   "stats",
			Short: "Show entry counts and store path",
			RunE: func(c *cobra.Command, args []string) error {
				stats, err := mempalace.Stats(context.Background())
				if err != nil {
					return err
				}
				if asJSON {
					b, _ := json.MarshalIndent(stats, "", "  ")
					fmt.Println(string(b))
					return nil
				}
				fmt.Printf("  path:           %s\n", stats.Path)
				fmt.Printf("  total_entries:  %d\n", stats.TotalEntries)
				fmt.Printf("  with_embedding: %d\n", stats.WithEmbedding)
				fmt.Printf("  size_bytes:     %d\n", stats.SizeBytes)
				if len(stats.Projects) > 0 {
					fmt.Printf("  projects:       %v\n", stats.Projects)
				}
				return nil
			},
		}
		sub.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
		cmd.AddCommand(sub)
	}

	// serve — HTTP MCP server
	{
		var addr string
		sub := &cobra.Command{
			Use:   "serve",
			Short: "Run mempalace as an HTTP MCP server (recall / remember / forget / stats tools)",
			RunE: func(c *cobra.Command, args []string) error {
				ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				return mempalace.Serve(ctx, addr)
			},
		}
		sub.Flags().StringVar(&addr, "addr", "127.0.0.1:8765", "HTTP listen address")
		cmd.AddCommand(sub)
	}

	return cmd
}

func newBifrostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bifrost",
		Short: "MCP gateway: one endpoint that fans out to N downstream MCP servers",
		RunE: func(c *cobra.Command, args []string) error {
			fmt.Println(bifrost.Help())
			return nil
		},
	}

	{
		var addr, mempalaceURL, apiKey string
		sub := &cobra.Command{
			Use:   "serve",
			Short: "Run the bifrost gateway HTTP server",
			RunE: func(c *cobra.Command, args []string) error {
				ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				if apiKey == "" {
					apiKey = os.Getenv("BIFROST_API_KEY")
				}
				return bifrost.Serve(ctx, bifrost.Config{
					Listen: addr,
					APIKey: apiKey,
					Downstreams: []bifrost.Downstream{
						{Name: "mempalace", URL: mempalaceURL},
					},
				})
			},
		}
		sub.Flags().StringVar(&addr, "addr", "127.0.0.1:8443", "HTTP listen address")
		sub.Flags().StringVar(&mempalaceURL, "mempalace", "http://127.0.0.1:8765/mcp", "mempalace MCP endpoint to proxy to")
		sub.Flags().StringVar(&apiKey, "api-key", "", "Require Bearer token on incoming requests (also reads BIFROST_API_KEY env)")
		cmd.AddCommand(sub)
	}

	return cmd
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
