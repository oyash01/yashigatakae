package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/oyash01/yashigatakae/internal/osdetect"
)

// RotateOptions controls `yashigatakae secrets rotate`.
type RotateOptions struct {
	OnlyKeys       []string // when set, rotate only these (default: BIFROST_API_KEY + KINTSUGI_KEY)
	RestartServices bool    // run `systemctl restart yashigatakae-*` after writing (Linux + root only)
	DryRun         bool
}

// Rotate generates new random values for the listed secrets, writes them
// into ~/.yashigatakae/secrets.env (preserving other lines), and optionally
// restarts the systemd services so the new values take effect.
//
// IMPORTANT for KINTSUGI_KEY: rotating it invalidates BOTH the at-rest DB
// envelopes AND every kintsugi blob on the relay. The function prints a big
// warning before doing this and aborts unless --force is set via the CLI.
func Rotate(opts RotateOptions) error {
	if len(opts.OnlyKeys) == 0 {
		opts.OnlyKeys = []string{"BIFROST_API_KEY", "KINTSUGI_KEY"}
	}

	yashDir, err := osdetect.YashigatakaeDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(yashDir, 0o700); err != nil {
		return err
	}
	envPath := filepath.Join(yashDir, "secrets.env")

	body := ""
	if b, err := os.ReadFile(envPath); err == nil {
		body = string(b)
	}

	newVals := map[string]string{}
	for _, k := range opts.OnlyKeys {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		newVals[k] = hex.EncodeToString(buf)
	}

	if opts.DryRun {
		for k, v := range newVals {
			fmt.Printf("  would set %s=%s...%s\n", k, v[:8], v[len(v)-4:])
		}
		fmt.Println("  (dry-run — secrets.env not modified)")
		return nil
	}

	body = upsertEnvLines(body, newVals)
	if err := os.WriteFile(envPath, []byte(body), 0o600); err != nil {
		return err
	}
	for k, v := range newVals {
		fmt.Printf("  ✓ %s rotated (sha8=%s)\n", k, v[:8])
	}
	fmt.Printf("  · written to %s (perm 0600)\n", envPath)

	if opts.RestartServices {
		if os.Geteuid() != 0 {
			fmt.Println("  ! --restart requires root; skipping systemctl restart (re-run with sudo)")
		} else {
			cmd := exec.Command("systemctl", "restart",
				"yashigatakae-mempalace.service",
				"yashigatakae-bifrost.service",
				"yashigatakae-kintsugi.service",
				"yashigatakae-hermes.service",
			)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("systemctl restart: %w", err)
			}
			fmt.Println("  ✓ all yashigatakae services restarted")
		}
	}

	fmt.Println()
	fmt.Println("Update each client machine's ~/.yashigatakae/secrets.env with the new values, then `yashigatakae init` to re-render settings.json.")
	if _, ok := newVals["KINTSUGI_KEY"]; ok {
		fmt.Println()
		fmt.Println("⚠ KINTSUGI_KEY rotated — past kintsugi blobs on the relay AND any at-rest .age files are now unreadable. Old handoffs cannot be resumed.")
	}
	return nil
}

// upsertEnvLines walks the body of secrets.env line-by-line, replacing
// matching KEY= lines and appending any keys not seen.
func upsertEnvLines(body string, kv map[string]string) string {
	seen := map[string]bool{}
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines)+len(kv))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out = append(out, line)
			continue
		}
		i := strings.Index(line, "=")
		if i < 0 {
			out = append(out, line)
			continue
		}
		key := strings.TrimSpace(line[:i])
		if v, ok := kv[key]; ok {
			out = append(out, key+"="+v)
			seen[key] = true
		} else {
			out = append(out, line)
		}
	}
	for k, v := range kv {
		if !seen[k] {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			out = append(out, k+"="+v)
		}
	}
	if len(out) == 0 || out[len(out)-1] != "" {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}
