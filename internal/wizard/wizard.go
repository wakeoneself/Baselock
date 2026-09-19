package wizard

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/wakeoneself/Baselock/internal/addons/dokploy"
	"github.com/wakeoneself/Baselock/internal/plan"
	"github.com/wakeoneself/Baselock/internal/sys"
	"github.com/wakeoneself/Baselock/internal/ui"
)

func Run(base plan.Plan, u *ui.UI) (plan.Plan, error) {
	if !u.TTY() {
		return base, fmt.Errorf("run from a terminal or pass --yes")
	}

	p := plan.Recommended()
	p.DryRun = base.DryRun
	p.Plain = base.Plain
	p.Quiet = base.Quiet
	p.Verbose = base.Verbose
	if base.Username != "" {
		p.Username = base.Username
	}
	if base.SSHPubKey != "" {
		p.SSHPubKey = base.SSHPubKey
	}
	if base.WebhookHost != "" {
		p.WebhookHost = base.WebhookHost
	}

	dockerPresent := sys.DockerInstalled()
	dokployPresent := dokploy.Detected()

	doUser := true
	doUFW := true
	doSSH := true
	doProtect := true
	doDocker := dockerPresent
	doDokploy := dokployPresent
	copyKeys := true
	username := p.Username
	webhook := p.WebhookHost

	theme := huh.ThemeCharm()
	if u.Plain() {
		theme = huh.ThemeBase()
	}

	groups := []*huh.Group{
		huh.NewGroup(
			huh.NewNote().
				Title("Recommended defaults").
				Description("Press Enter to keep Yes on everything. You can turn pieces off."),
			huh.NewConfirm().
				Title("🔑  Create a sudo user (passwordless sudo, SSH keys)?").
				Description("Why: daily work as a non-root user. Root SSH with keys stays as break-glass — serial consoles need a password we never set.").
				Value(&doUser).
				Affirmative("Yes").
				Negative("No"),
			huh.NewInput().
				Title("Operator username").
				Value(&username).
				Validate(func(s string) error {
					if s == "" || s == "root" {
						return fmt.Errorf("pick a non-root username")
					}
					return nil
				}),
			huh.NewConfirm().
				Title("Copy SSH keys from root onto that user?").
				Description("Why: without a key, sec will refuse to lock password SSH.").
				Value(&copyKeys).
				Affirmative("Yes").
				Negative("No"),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("🛡️  Firewall (SSH / 80 / 443 only)?").
				Description("Why: default-deny incoming. Includes ufw-docker so published containers cannot skip UFW.").
				Value(&doUFW).
				Affirmative("Yes").
				Negative("No"),
			huh.NewConfirm().
				Title("🚪  Block password SSH logins?").
				Description("Why: keys only. Skipped automatically if no authorized_keys exist.").
				Value(&doSSH).
				Affirmative("Yes").
				Negative("No"),
			huh.NewConfirm().
				Title("🔥  Fail2ban + automatic security updates?").
				Description("Why: ban brute-force SSH and patch in the background.").
				Value(&doProtect).
				Affirmative("Yes").
				Negative("No"),
		),
	}

	if dockerPresent {
		groups = append(groups, huh.NewGroup(
			huh.NewConfirm().
				Title("🐳  Harden the Docker daemon?").
				Description("Why: live-restore, log rotation, never expose the Docker socket over TCP.").
				Value(&doDocker).
				Affirmative("Yes").
				Negative("No"),
		))
	}

	if dokployPresent {
		groups = append(groups, huh.NewGroup(
			huh.NewConfirm().
				Title("🔒  Dokploy found — hide the admin UI?").
				Description("Why: panel only via SSH tunnel. Git webhooks and apps stay on 80/443.").
				Value(&doDokploy).
				Affirmative("Yes").
				Negative("No"),
		))
	}

	if err := huh.NewForm(groups...).WithTheme(theme).Run(); err != nil {
		return p, err
	}

	if doDokploy && dokployPresent && webhook == "" {
		if err := dokploy.HasWebhookHostOrDomain(""); err != nil {
			if err := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Webhook host (required)").
						Description("No panel domain found. Git providers must reach https://HOST/api/deploy/…").
						Placeholder("deploy.example.com").
						Value(&webhook).
						Validate(func(s string) error {
							if s == "" {
								return fmt.Errorf("required — lock would break auto-deploy")
							}
							return nil
						}),
				),
			).WithTheme(theme).Run(); err != nil {
				return p, err
			}
		}
	}

	p.User = doUser
	p.Username = username
	p.CopyRootKeys = copyKeys
	p.UFW = doUFW
	p.SSH = doSSH
	p.Fail2ban = doProtect
	p.Updates = doProtect
	p.Docker = doDocker && dockerPresent
	p.Dokploy = doDokploy && dokployPresent
	p.WebhookHost = webhook
	return p, nil
}
