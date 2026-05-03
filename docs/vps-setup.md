# VPS Setup

Production-grade always-on brain. Five minutes if your DNS is already pointed.

## Requirements

- A VPS (anything with 1 vCPU + 1 GB RAM works; mempalace + hermes + bifrost + kintsugi total ~150 MB resident)
- Ubuntu 22.04 / 24.04 (Debian works too)
- Root SSH access
- (Optional but recommended) A domain name with a DNS A record pointing at the VPS

## 1. Install the binary

```bash
curl -fsSL https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.sh | sh
```

## 2. Bootstrap the VPS

```bash
sudo yashigatakae init --vps
```

This:
- Generates `BIFROST_API_KEY` + `KINTSUGI_KEY` (32 random bytes each, hex-encoded)
- Writes systemd units for `mempalace`, `bifrost`, `kintsugi`, `hermes`
- Hardens with `Restart=always`, `StartLimitIntervalSec=0`, `KillSignal=SIGTERM`
- Wires `ExecStartPre`/`ExecStopPost` for at-rest sqlite encryption
- Opens UFW ports 22, 80, 443, 8443, 8444 (gated on UFW being active)
- Starts and enables every service
- Prints a 4-line client snippet

The output looks like:
```
══════════════════════════════════════════════════════════════
 VPS install complete.  Each client machine runs:
══════════════════════════════════════════════════════════════

   # one-time setup on every client (Mac/Win/Linux)
   curl -fsSL https://raw.githubusercontent.com/oyash01/yashigatakae/main/installers/install.sh | sh

   # then add to ~/.yashigatakae/secrets.env
   BIFROST_URL=https://203.0.113.10:8443/mcp
   BIFROST_API_KEY=fae7b3...
   KINTSUGI_KEY=8d2c91...

   # then init
   yashigatakae init
```

Copy those 3 env-var lines into every client's `~/.yashigatakae/secrets.env`, then re-run `yashigatakae init` on the client.

## 3. Real TLS (optional but recommended)

The default `init --vps` ships self-signed certs (browsers warn; `curl --insecure` works). For Let's Encrypt:

```bash
# On the VPS — DNS A record must already point at this host
sudo systemctl stop yashigatakae-bifrost.service
sudo yashigatakae bifrost serve --addr 0.0.0.0:8443 \
    --tls --tls-domain yashi.example.com \
    --mempalace http://127.0.0.1:8765/mcp &
```

Or edit `/etc/systemd/system/yashigatakae-bifrost.service` to add `--tls --tls-domain yashi.example.com` to the `ExecStart` line, then `systemctl daemon-reload && systemctl restart yashigatakae-bifrost.service`.

The autocert library handles ACME negotiation automatically. Cert + key cached in `~/.yashigatakae/autocert/`.

## 4. fail2ban (recommended)

```bash
sudo cp installers/yashigatakae.fail2ban.conf /etc/fail2ban/jail.d/
sudo cp installers/yashigatakae.fail2ban-filter.conf /etc/fail2ban/filter.d/
sudo systemctl restart fail2ban
sudo fail2ban-client status yashigatakae
```

Default: ban any IP that triggers ≥10 401/403 responses in 5 minutes for 1 hour.

## 5. Logrotate

Audit log at `/var/log/yashigatakae/audit.log` grows ~10 MB/day on a busy VPS. Add:

```
/var/log/yashigatakae/audit.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    copytruncate
}
```

## 6. Verify

```bash
yashigatakae status
curl -fsSI https://yashi.example.com:8443/health      # 200, valid cert chain
curl -fsSI https://yashi.example.com:8444/health      # 200
sudo journalctl -u yashigatakae-bifrost -f            # live logs
```

## Key rotation

```bash
sudo yashigatakae secrets rotate --restart
```

Generates new BIFROST_API_KEY + KINTSUGI_KEY, writes them to `~/.yashigatakae/secrets.env`, restarts all 4 services. Then update every client's `secrets.env` with the new values.

⚠️  **Rotating KINTSUGI_KEY invalidates every kintsugi blob on the relay AND every at-rest sqlite encryption envelope.** Old session handoffs cannot be resumed after rotation. Use only when there's reason to (suspected compromise, departing team member).

## Hardening checklist

- [x] systemd `Restart=always` + `StartLimitIntervalSec=0`
- [x] UFW open only on 22 / 80 / 443 / 8443 / 8444
- [x] TLS via autocert OR self-signed
- [x] Bearer-token auth on bifrost + kintsugi
- [x] Per-key rate limiting (200 req/min authed, 60/min unauth)
- [x] JSONL audit log + fail2ban template
- [x] At-rest encryption for mempalace.db / hermes.db
- [x] Secrets rotation with one command
- [ ] OS package updates: `sudo unattended-upgrades` (you set up; not part of yashigatakae)
- [ ] Backup of `~/.yashigatakae/` to a second VPS or S3 (your call)

## Troubleshooting

| Symptom | Fix |
|---|---|
| `bifrost serve: address already in use :8443` | `sudo lsof -i :8443` then kill the offender |
| `autocert: HTTP-01 challenge failed` | Verify port 80 is reachable AND DNS A record resolves to this host BEFORE starting |
| Client can't connect: `tls: certificate signed by unknown authority` | Either set up real TLS (above) or use `BIFROST_INSECURE=1` env var on the client |
| `fail2ban: jail not started` | Check `/var/log/fail2ban.log` — usually a regex syntax issue in the filter conf |
| `Restart=always` but service still down | `systemctl status` will show the actual error; common: missing `KINTSUGI_KEY` in secrets.env |
