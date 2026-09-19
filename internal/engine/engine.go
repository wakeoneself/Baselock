package engine

import (
	"fmt"
	"strings"

	"github.com/wakeoneself/Baselock/internal/addons/dokploy"
	"github.com/wakeoneself/Baselock/internal/backup"
	"github.com/wakeoneself/Baselock/internal/modules"
	"github.com/wakeoneself/Baselock/internal/plan"
	"github.com/wakeoneself/Baselock/internal/sys"
	"github.com/wakeoneself/Baselock/internal/ui"
)

func Enabled(p plan.Plan) []modules.Module {
	var list []modules.Module
	if p.User {
		list = append(list, modules.User{})
	}
	if p.SSH {
		list = append(list, modules.SSH{})
	}
	if p.UFW {
		list = append(list, modules.UFW{})
	}
	if p.Fail2ban {
		list = append(list, modules.Fail2ban{})
	}
	if p.Updates {
		list = append(list, modules.Updates{})
	}
	if p.Docker {
		list = append(list, modules.Docker{})
	}
	if p.Dokploy {
		list = append(list, dokploy.Addon{})
	}
	return list
}

func All() []modules.Module {
	list := []modules.Module{
		modules.User{},
		modules.SSH{},
		modules.UFW{},
		modules.Fail2ban{},
		modules.Updates{},
		modules.Docker{},
	}
	if dokploy.Detected() {
		list = append(list, dokploy.Addon{})
	}
	return list
}

func Apply(p plan.Plan, u *ui.UI) error {
	mods := Enabled(p)
	if len(mods) == 0 {
		return fmt.Errorf("nothing selected — run `sec` for the wizard or pass module flags")
	}

	var snap *backup.Snapshot
	var err error
	if !p.DryRun {
		snap, err = backup.New(p.ModuleNames())
		if err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		u.Detail("snapshot " + snap.Dir)
	}

	ctx := modules.Context{Plan: p, UI: u, Snapshot: snap, DryRun: p.DryRun}
	var failed string
	for _, m := range mods {
		title := stepTitle(m.Name())
		if err := u.Step(title, func() error {
			return m.Apply(ctx)
		}); err != nil {
			failed = m.Name()
			u.FailDetail(err.Error(), "step "+m.Name()+" stopped", "sec revert   (restores /var/lib/sec/backups)")
			break
		}
	}
	if failed != "" {
		return fmt.Errorf("stopped at %s", failed)
	}

	printNext(p, u)
	return nil
}

func Status(u *ui.UI) {
	var rows []ui.Row
	for _, m := range All() {
		c := m.Status()
		rows = append(rows, ui.Row{Name: c.Name, Level: c.Level, Reason: c.Reason, Next: c.Next})
	}
	u.Table(rows)
}

func Revert(p plan.Plan, u *ui.UI, module string) error {
	snap, err := backup.Latest()
	if err != nil {
		return err
	}
	u.Detail("restoring snapshot " + snap.ID)

	ctx := modules.Context{Plan: p, UI: u, Snapshot: snap, DryRun: p.DryRun}
	targets := All()
	if module != "" {
		var one []modules.Module
		for _, m := range All() {
			if m.Name() == module {
				one = append(one, m)
			}
		}
		if len(one) == 0 {
			return fmt.Errorf("unknown module %q", module)
		}
		targets = one
	}
	for _, m := range targets {
		if err := u.Step("revert "+m.Name(), func() error {
			return m.Revert(ctx, snap)
		}); err != nil {
			return err
		}
	}
	u.Success("restored snapshot " + snap.ID)
	return nil
}

func stepTitle(name string) string {
	switch name {
	case "user":
		return "Creating sudo operator and turning off root SSH"
	case "ssh":
		return "Disabling password SSH"
	case "ufw":
		return "Configuring UFW + ufw-docker"
	case "fail2ban":
		return "Enabling Fail2ban"
	case "updates":
		return "Enabling unattended upgrades"
	case "docker":
		return "Hardening Docker daemon"
	case "dokploy":
		return "Locking Dokploy admin UI"
	default:
		return name
	}
}

func printNext(p plan.Plan, u *ui.UI) {
	host := sys.PublicIP()
	user := p.Username
	if user == "" {
		user = "deploy"
	}
	var lines []string
	if p.User {
		lines = append(lines, fmt.Sprintf("ssh %s@%s", user, host))
		lines = append(lines, user+" has no password — SSH key only, sudo does not ask")
		lines = append(lines, "root SSH is closed — VPS console root still works")
	}
	if p.Dokploy {
		lines = append(lines, dokploy.TunnelHint(user)...)
		dokploy.PrintLockSummary(u, user, p.WebhookHost)
	}
	if len(lines) > 0 {
		u.NextStep("Harden complete", unique(lines))
	} else {
		u.Success("Done")
	}
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
