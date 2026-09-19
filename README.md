# sec

**Harden an Ubuntu/Debian box in one command.**

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Ubuntu / Debian](https://img.shields.io/badge/OS-Ubuntu%20%2F%20Debian-E95420?logo=ubuntu&logoColor=white)](https://docs.dokploy.com/docs/core/guides/production-hardening)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Download it. Run it. The server locks itself down — or it asks a few questions first.

```bash
curl -sSL https://raw.githubusercontent.com/abyss/server-sec-cli/main/install.sh | sudo bash
sudo sec
```

No flags required. Press Enter to accept the recommended defaults.

Already know you want the full baseline with zero prompts?

```bash
sudo sec --yes
```

---

## What it does

- 🔑 Creates a sudo operator and turns off **root SSH** (root account stays for console)
- 🚪 Allows SSH keys only — password logins go away
- 🛡️ UFW default-deny, plus [ufw-docker](https://github.com/chaifeng/ufw-docker) so Docker cannot bypass the firewall
- 🔥 Fail2ban on SSH
- 📦 Unattended security updates
- 🐳 Hardens `daemon.json` (no TCP Docker socket)
- 🔒 **Dokploy addon:** admin UI only via SSH tunnel; git webhooks and apps stay on 80/443

---

## Wizard

`sudo sec` opens a short English wizard. Every question has a one-line *why* and a safe default.

```text
┌  sec  v0.1.0  ·  box.example.com  ·  Ubuntu 24.04
│
│  🔑  Create a sudo user and turn off root SSH?     Yes
│      Username                                      deploy
│      Copy SSH keys from root?                      Yes
│  🛡️  Firewall (SSH / 80 / 443 only)?               Yes
│  🚪  Block password SSH logins?                    Yes
│  🔥  Fail2ban + automatic security updates?        Yes
│  🐳  Harden the Docker daemon?                     Yes
│  🔒  Dokploy found — hide the admin UI?            Yes
│
│  Press Enter to apply.
└
```

When it finishes you get the next step, not a lecture:

```text
ssh deploy@your-server
ssh -L 3000:127.0.0.1:3000 deploy@your-server   # then open http://127.0.0.1:3000
```

---

## Dokploy addon

Dokploy publishes its panel on port **3000**. Closing that port with no other path locks you out. Hiding the *entire* Dokploy domain behind localhost breaks [GitHub / GitLab auto-deploy](https://docs.dokploy.com/docs/core/auto-deploy).

`sec` does the split from the [official hardening guide](https://docs.dokploy.com/docs/core/guides/production-hardening):

| Stay public | Go private |
|---|---|
| Apps on **80 / 443** | Admin UI on **127.0.0.1:3000** |
| `/api/deploy/*` webhooks | Dashboard `/` |
| `/api/providers/*` git OAuth callbacks | Raw `IP:3000` |

```bash
sudo sec addon dokploy lock      # hide the panel
sudo sec addon dokploy status
sudo sec addon dokploy unlock    # restore the previous publish + Traefik config
```

If the panel has **no domain** and webhooks still point at `http://IP:3000/...`, lock asks for `--webhook-host` (or the wizard does). It will not guess — a wrong host would kill git deploys.

Access the panel:

```bash
ssh -L 3000:127.0.0.1:3000 deploy@your-server
# open http://127.0.0.1:3000
```

Keep provider webhook URLs on `https://<dokploy-or-webhook-host>/api/deploy/...`.

Docs: [Dokploy Core](https://docs.dokploy.com/docs/core) · [Installation](https://docs.dokploy.com/docs/core/installation) · [GitHub](https://docs.dokploy.com/docs/core/github) · [GitLab](https://docs.dokploy.com/docs/core/gitlab)

---

## Customize

The wizard is the default path. Flags are optional.

```bash
sudo sec apply --user --username deploy --copy-root-keys
sudo sec apply --ufw --no-ssh
sudo sec apply --profile baseline --addon dokploy --webhook-host deploy.example.com
sudo sec apply --dry-run --yes
```

| Flag | What it does |
|---|---|
| `--user` / `--no-user` | Sudo operator + `PermitRootLogin no` |
| `--username` | Operator name (default `deploy`) |
| `--ssh-pubkey` | Public key file for the operator |
| `--copy-root-keys` | Copy `/root/.ssh/authorized_keys` |
| `--nopasswd-sudo` | Passwordless sudo (off unless you ask) |
| `--ufw` / `--no-ufw` | Firewall + ufw-docker |
| `--ssh` / `--no-ssh` | Disable password authentication |
| `--fail2ban` / `--no-fail2ban` | SSH jail |
| `--updates` / `--no-updates` | Unattended upgrades |
| `--docker` / `--no-docker` | Harden Docker daemon |
| `--addon dokploy` | Lock the Dokploy panel |
| `--webhook-host` | Public host for git webhooks (required if no panel domain) |
| `--yes` | No prompts — recommended defaults + auto-detect |
| `--dry-run` | Print the plan, change nothing |
| `--plain` | No color, no emoji (also honors `NO_COLOR`) |
| `--quiet` | Result and errors only |

```bash
sudo sec status
sudo sec revert
sudo sec revert --module user
sudo sec revert --module user --purge-user
```

`--profile baseline` is the same recommended set as `--yes`: user, ssh, ufw, fail2ban, updates (and docker when Docker is present).

---

## Safety

- Every apply writes a snapshot under `/var/lib/sec/backups/<timestamp>/`
- Root SSH is **not** disabled until the operator has an `authorized_keys` file
- The `root` account is never deleted — VPS console login still works
- `--dry-run` is always safe
- `sec revert` puts sshd, sudoers, UFW, Fail2ban, Docker, and Dokploy publish/Traefik back
- Docker publishes ports around UFW; `sec` installs ufw-docker so those rules actually apply

Non-interactive environments (pipe, cron) cannot run the wizard. Pass `--yes` or explicit module flags.

---

## Requirements

- Ubuntu or Debian (the same families [Dokploy targets](https://docs.dokploy.com/docs/core/installation))
- Root or sudo
- A TTY for the wizard, or `--yes` / flags

Build from this repo:

```bash
go build -o sec ./cmd/sec
sudo ./install.sh
```

---

## License

MIT
