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

	// 4. Open firewall ports if ufw is active. Best-effort; not fatal.
	if out, _ := exec.Command("ufw", "status").Output(); strings.Contains(string(out), "Status: active") {
		for _, port := range []string{"22/tcp", "80/tcp", "443/tcp", "8443/tcp", "8444/tcp"} {
			_ = exec.Command("ufw", "allow", port).Run()
		}
		fmt.Println("  · ufw rules added for 22, 80, 443, 8443, 8444")
	}

	// 5. Get the kintsugi key for the client snippet too.
	kintKey, _ := readSecret("KINTSUGI_KEY")

	// 6. Final report.
	publicIP := detectPublicIP()
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println(" VPS install complete.  Each client machine runs:")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("   # one-time setup on every client (Mac/Win/Linux)")
	fmt.Println("   curl -fsSL https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.sh | sh")
	fmt.Println()
	fmt.Println("   # then add to ~/.yashigatakae/secrets.env")
	fmt.Printf("   BIFROST_URL=https://%s:8443/mcp\n", publicIP)
	fmt.Printf("   BIFROST_API_KEY=%s\n", apiKey)
	fmt.Printf("   KINTSUGI_KEY=%s\n", kintKey)
	fmt.Println()
	fmt.Println("   # then init")
	fmt.Println("   yashigatakae init")
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println("   Always-on:    `sudo yashigatakae enable`")
	fmt.Println("   TLS:          re-run a service with --tls-domain DOMAIN")
	fmt.Println("                 (requires DNS A record + :80 reachable)")
	fmt.Println("   fail2ban:     installers/yashigatakae.fail2ban.conf")
	fmt.Println("   Audit log:    /var/log/yashigatakae/audit.log")
	fmt.Println("   Rotate keys:  `yashigatakae secrets rotate --restart`")
	fmt.Println("══════════════════════════════════════════════════════════════")

	return nil
}

// readSecret retrieves a key from secrets.env (best-effort, returns "").
func readSecret(key string) (string, error) {
	home, _ := os.UserHomeDir()
	body, err := os.ReadFile(filepath.Join(home, ".yashigatakae", "secrets.env"))
	if err != nil {
		return "", err
	}
	return readEnvVar(string(body), key), nil
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

// hardenedService is the shared block of always-on options every yashigatakae
// systemd unit gets. Tuned so:
//   - any non-zero exit triggers a restart (Restart=always)
//   - first restart fires after 3 s
//   - the systemd start-rate-limit is disabled (StartLimitIntervalSec=0) so a
//     pathological tight-crash loop still gets retried indefinitely instead of
//     being held in a "failed" state after 5 starts
//   - SIGTERM is sent on stop with a 10-second grace before SIGKILL
//   - journal capture for both stdout + stderr
const hardenedServiceBlock = `Restart=always
RestartSec=3
StartLimitIntervalSec=0
TimeoutStopSec=10
KillSignal=SIGTERM
EnvironmentFile=-%s/.yashigatakae/secrets.env
StandardOutput=journal
StandardError=journal
`

func writeMempalaceUnit(yashi string) error {
	home := mustHome()
	unit := fmt.Sprintf(`[Unit]
Description=yashigatakae mempalace MCP server (semantic memory)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStartPre=-%s db unlock %s/.yashigatakae/mempalace.db
ExecStart=%s mempalace serve --addr 127.0.0.1:8765
ExecStopPost=-%s db lock %s/.yashigatakae/mempalace.db
`+hardenedServiceBlock+`
[Install]
WantedBy=multi-user.target
`, yashi, home, yashi, yashi, home, home)
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
`+hardenedServiceBlock+`
[Install]
WantedBy=multi-user.target
`, yashi, mustHome())
	return os.WriteFile("/etc/systemd/system/yashigatakae-kintsugi.service", []byte(unit), 0o644)
}

func writeHermesUnit(yashi string) error {
	home := mustHome()
	unit := fmt.Sprintf(`[Unit]
Description=yashigatakae hermes worker (background self-learning agent)
After=network-online.target yashigatakae-mempalace.service
Wants=network-online.target

[Service]
Type=simple
ExecStartPre=-%s db unlock %s/.yashigatakae/hermes.db
ExecStart=%s hermes serve --poll 10s
ExecStopPost=-%s db lock %s/.yashigatakae/hermes.db
`+hardenedServiceBlock+`
[Install]
WantedBy=multi-user.target
`, yashi, home, yashi, yashi, home, home)
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
`+hardenedServiceBlock+`
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
