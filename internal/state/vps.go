package state

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// RunVPS is the `yashigatakae init --vps` flow. Installs systemd units for
// mempalace + bifrost, generates secrets, and prints the URL/key the Mac/Win
// clients should use to connect.
//
// Linux only — Mac and Windows aren't supported as the always-on host. The
// rest of init flow has already run (caveman hooks are no-ops on a headless
// VPS, but state-repo + mempalace store are still wanted).
func RunVPS() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("--vps is supported on Linux only (this is %s)", runtime.GOOS)
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("--vps must be run as root (use sudo)")
	}

	yashi, err := os.Executable()
	if err != nil {
		return fmt.Errorf("os.Executable: %w", err)
	}

	// 1. Generate / load secrets.env
	apiKey, err := ensureSecret("BIFROST_API_KEY")
	if err != nil {
		return err
	}
	if _, err := ensureSecret("KINTSUGI_KEY"); err != nil {
		return err
	}

	// 2. Write systemd units
	if err := writeMempalaceUnit(yashi); err != nil {
		return err
	}
	if err := writeBifrostUnit(yashi); err != nil {
		return err
	}
	if err := writeKintsugiUnit(yashi); err != nil {
		return err
	}
	if err := writeHermesUnit(yashi); err != nil {
		return err
	}

	// 3. Reload + enable + start
	for _, args := range [][]string{
		{"daemon-reload"},
		{"enable", "yashigatakae-mempalace.service"},
		{"enable", "yashigatakae-bifrost.service"},
		{"enable", "yashigatakae-kintsugi.service"},
		{"enable", "yashigatakae-hermes.service"},
		{"restart", "yashigatakae-mempalace.service"},
		{"restart", "yashigatakae-bifrost.service"},
		{"restart", "yashigatakae-kintsugi.service"},
		{"restart", "yashigatakae-hermes.service"},
	} {
		cmd := exec.Command("systemctl", args...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("systemctl %v: %w", args, err)
		}
	}

	// 4. Final report
	publicIP := detectPublicIP()
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println(" VPS install complete.  Mac/Win clients connect via:")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Printf("   URL:  http://%s:8443/mcp\n", publicIP)
	fmt.Printf("   Auth: Authorization: Bearer %s\n", apiKey)
	fmt.Println()
	fmt.Println("   On every client:")
	fmt.Println("     1. Set BIFROST_URL and BIFROST_API_KEY in ~/.yashigatakae/secrets.env")
	fmt.Println("     2. Re-run `yashigatakae init` (or `yashigatakae state render`)")
	fmt.Println("     3. ~/.claude/settings.json's bifrost MCP entry will point at the VPS")
	fmt.Println()
	fmt.Println("   Recommended next: front this with Caddy or nginx for HTTPS.")
	fmt.Println("   (Built-in TLS support arrives in v0.7.)")
	fmt.Println("══════════════════════════════════════════════════════════════")

	return nil
}

// ensureSecret reads ~/.yashigatakae/secrets.env, returns the value of `key`
// if set, otherwise generates a 32-byte hex random, appends `KEY=value` to
// secrets.env, and returns the new value.
func ensureSecret(key string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	envPath := filepath.Join(home, ".yashigatakae", "secrets.env")
	if err := os.MkdirAll(filepath.Dir(envPath), 0o700); err != nil {
		return "", err
	}
	body := ""
	if b, err := os.ReadFile(envPath); err == nil {
		body = string(b)
	}
	if v := readEnvVar(body, key); v != "" {
		return v, nil
	}
	// Generate.
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	val := hex.EncodeToString(buf)
	if !strings.HasSuffix(body, "\n") && body != "" {
		body += "\n"
	}
	body += fmt.Sprintf("%s=%s\n", key, val)
	if err := os.WriteFile(envPath, []byte(body), 0o600); err != nil {
		return "", err
	}
	fmt.Printf("  · generated %s in %s\n", key, envPath)
	return val, nil
}

func readEnvVar(body, key string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "="); i > 0 && line[:i] == key {
			return strings.Trim(line[i+1:], `"' `)
		}
	}
	return ""
}

func writeMempalaceUnit(yashi string) error {
	unit := fmt.Sprintf(`[Unit]
Description=yashigatakae mempalace MCP server (semantic memory)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s mempalace serve --addr 127.0.0.1:8765
Restart=always
RestartSec=3
EnvironmentFile=-%s/.yashigatakae/secrets.env
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, yashi, mustHome())
	return os.WriteFile("/etc/systemd/system/yashigatakae-mempalace.service", []byte(unit), 0o644)
}

func writeKintsugiUnit(yashi string) error {
	unit := fmt.Sprintf(`[Unit]
Description=yashigatakae kintsugi relay (cross-device session blobs)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s kintsugi serve --addr 0.0.0.0:8444
Restart=always
RestartSec=3
EnvironmentFile=-%s/.yashigatakae/secrets.env
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, yashi, mustHome())
	return os.WriteFile("/etc/systemd/system/yashigatakae-kintsugi.service", []byte(unit), 0o644)
}

func writeHermesUnit(yashi string) error {
	unit := fmt.Sprintf(`[Unit]
Description=yashigatakae hermes worker (background self-learning agent)
After=network-online.target yashigatakae-mempalace.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s hermes serve --poll 10s
Restart=always
RestartSec=10
EnvironmentFile=-%s/.yashigatakae/secrets.env
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, yashi, mustHome())
	return os.WriteFile("/etc/systemd/system/yashigatakae-hermes.service", []byte(unit), 0o644)
}

func writeBifrostUnit(yashi string) error {
	unit := fmt.Sprintf(`[Unit]
Description=yashigatakae bifrost MCP gateway
After=yashigatakae-mempalace.service network-online.target
Wants=yashigatakae-mempalace.service network-online.target

[Service]
Type=simple
ExecStart=%s bifrost serve --addr 0.0.0.0:8443 --mempalace http://127.0.0.1:8765/mcp
Restart=always
RestartSec=3
EnvironmentFile=-%s/.yashigatakae/secrets.env
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, yashi, mustHome())
	return os.WriteFile("/etc/systemd/system/yashigatakae-bifrost.service", []byte(unit), 0o644)
}

func mustHome() string {
	h, _ := os.UserHomeDir()
	if h == "" {
		return "/root"
	}
	return h
}

// detectPublicIP picks a sensible default for the URL we print at the end.
// Falls back to the literal placeholder on failure (the user can edit).
func detectPublicIP() string {
	// Try `hostname -I` (linux). First IP wins.
	out, err := exec.Command("hostname", "-I").Output()
	if err == nil {
		fields := strings.Fields(string(out))
		if len(fields) > 0 {
			return fields[0]
		}
	}
	return "<your-vps-ip>"
}
