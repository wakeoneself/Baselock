package cli

import (
	"fmt"
	"os"

	"github.com/abyss/server-sec-cli/internal/addons/dokploy"
	"github.com/abyss/server-sec-cli/internal/engine"
	"github.com/abyss/server-sec-cli/internal/plan"
	"github.com/abyss/server-sec-cli/internal/sys"
	"github.com/abyss/server-sec-cli/internal/ui"
	"github.com/abyss/server-sec-cli/internal/wizard"
	"github.com/spf13/cobra"
)

// Version is set via -ldflags.
var Version = "0.1.0"

func New() *cobra.Command {
	p := plan.Plan{Verbose: true, Username: "deploy", CopyRootKeys: true}

	root := &cobra.Command{
		Use:           "sec",
		Short:         "Harden an Ubuntu/Debian box in one command",
		Long:          "Download it. Run it. The server locks itself down — or it asks a few questions first.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(cmd, &p)
		},
	}

	addGlobalFlags(root, &p)
	addModuleFlags(root, &p)

	root.AddCommand(newApplyCmd(&p))
	root.AddCommand(newStatusCmd(&p))
	root.AddCommand(newRevertCmd(&p))
	root.AddCommand(newAddonCmd(&p))
	root.AddCommand(newSetupCmd(&p))
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(Version)
		},
	})
	return root
}

func addGlobalFlags(cmd *cobra.Command, p *plan.Plan) {
	cmd.PersistentFlags().BoolVar(&p.Yes, "yes", false, "no prompts — recommended defaults + auto-detect")
	cmd.PersistentFlags().BoolVar(&p.DryRun, "dry-run", false, "print the plan, change nothing")
	cmd.PersistentFlags().BoolVar(&p.Plain, "plain", false, "no color, no emoji")
	cmd.PersistentFlags().BoolVar(&p.Quiet, "quiet", false, "result and errors only")
	cmd.PersistentFlags().BoolVar(&p.Verbose, "verbose", true, "show file paths and why each step is safe")
	cmd.PersistentFlags().StringVar(&p.Username, "username", "deploy", "operator username")
	cmd.PersistentFlags().StringVar(&p.SSHPubKey, "ssh-pubkey", "", "public key file for the operator")
	cmd.PersistentFlags().BoolVar(&p.CopyRootKeys, "copy-root-keys", true, "copy /root/.ssh/authorized_keys")
	cmd.PersistentFlags().BoolVar(&p.NoPasswdSudo, "nopasswd-sudo", false, "passwordless sudo (off unless asked)")
	cmd.PersistentFlags().BoolVar(&p.PurgeUser, "purge-user", false, "on revert, delete the operator user")
	cmd.PersistentFlags().StringVar(&p.WebhookHost, "webhook-host", "", "public host for Dokploy git webhooks")
	cmd.PersistentFlags().StringVar(&p.Profile, "profile", "", "baseline = recommended module set")
	cmd.PersistentFlags().StringVar(&p.AddonName, "addon", "", "enable an addon (dokploy)")
}

func addModuleFlags(cmd *cobra.Command, p *plan.Plan) {
	cmd.Flags().BoolVar(&p.User, "user", false, "sudo operator + disable root SSH")
	cmd.Flags().BoolVar(&p.UFW, "ufw", false, "firewall + ufw-docker")
	cmd.Flags().BoolVar(&p.SSH, "ssh", false, "disable password authentication")
	cmd.Flags().BoolVar(&p.Fail2ban, "fail2ban", false, "Fail2ban SSH jail")
	cmd.Flags().BoolVar(&p.Updates, "updates", false, "unattended upgrades")
	cmd.Flags().BoolVar(&p.Docker, "docker", false, "harden Docker daemon.json")
	cmd.Flags().Bool("no-user", false, "skip the user module")
	cmd.Flags().Bool("no-ufw", false, "skip the firewall module")
	cmd.Flags().Bool("no-ssh", false, "skip SSH password hardening")
	cmd.Flags().Bool("no-fail2ban", false, "skip Fail2ban")
	cmd.Flags().Bool("no-updates", false, "skip unattended upgrades")
	cmd.Flags().Bool("no-docker", false, "skip Docker daemon harden")
}

func newSetupCmd(p *plan.Plan) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive wizard (same as running sec with no args)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(cmd, p)
		},
	}
	addModuleFlags(cmd, p)
	return cmd
}

func newApplyCmd(p *plan.Plan) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply selected modules (custom path)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if p.AddonName == "dokploy" {
				p.Dokploy = true
			}
			if p.Profile == "baseline" || (p.Yes && !positiveModuleChanged(cmd) && p.Empty()) {
				fillRecommended(p)
			}
			applyNoFlags(cmd, p)
			if p.Empty() {
				return fmt.Errorf("nothing selected — run `sec` for the wizard or pass module flags")
			}
			return runApply(p)
		},
	}
	addModuleFlags(cmd, p)
	return cmd
}

func newStatusCmd(p *plan.Plan) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what is locked down and what is still open",
		RunE: func(cmd *cobra.Command, args []string) error {
			u := newUI(p)
			host := sys.HostInfo()
			u.Banner(Version, host.Hostname, host.OSName, false)
			engine.Status(u)
			return nil
		},
	}
}

func newRevertCmd(p *plan.Plan) *cobra.Command {
	var module string
	cmd := &cobra.Command{
		Use:   "revert",
		Short: "Restore the latest snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := preflight(p, false); err != nil {
				return err
			}
			u := newUI(p)
			host := sys.HostInfo()
			u.Banner(Version, host.Hostname, host.OSName, p.DryRun)
			if !p.Yes && !p.DryRun {
				ok, err := u.Confirm("Restore the latest snapshot?")
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("cancelled")
				}
			}
			return engine.Revert(*p, u, module)
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "revert a single module (user, ssh, ufw, fail2ban, docker, dokploy)")
	return cmd
}

func newAddonCmd(p *plan.Plan) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "addon",
		Short: "Addon commands",
	}
	dok := &cobra.Command{
		Use:   "dokploy",
		Short: "Secure the Dokploy admin UI",
	}
	dok.AddCommand(&cobra.Command{
		Use:   "lock",
		Short: "Bind the panel to 127.0.0.1 and keep git webhooks public",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := preflight(p, true); err != nil {
				return err
			}
			pl := *p
			pl.Dokploy = true
			if err := dokploy.HasWebhookHostOrDomain(pl.WebhookHost); err != nil {
				return err
			}
			return runApply(&pl)
		},
	})
	dok.AddCommand(&cobra.Command{
		Use:   "unlock",
		Short: "Restore the previous Dokploy publish and Traefik config",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := preflight(p, true); err != nil {
				return err
			}
			u := newUI(p)
			return engine.Revert(*p, u, "dokploy")
		},
	})
	dok.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Check whether the panel is public",
		Run: func(cmd *cobra.Command, args []string) {
			u := newUI(p)
			c := dokploy.Addon{}.Status()
			u.Table([]ui.Row{{Name: c.Name, Level: c.Level, Reason: c.Reason, Next: c.Next}})
		},
	})
	cmd.AddCommand(dok)
	return cmd
}

func runSetup(cmd *cobra.Command, p *plan.Plan) error {
	if err := preflight(p, true); err != nil {
		return err
	}
	u := newUI(p)
	host := sys.HostInfo()
	u.Banner(Version, host.Hostname, host.OSName, p.DryRun)

	if p.AddonName == "dokploy" {
		p.Dokploy = true
	}
	positive := positiveModuleChanged(cmd)

	var err error
	switch {
	case p.Yes || p.Profile == "baseline":
		if !positive {
			fillRecommended(p)
		}
	case positive:
		// keep explicit --user / --ufw / …
	default:
		*p, err = wizard.Run(*p, u)
		if err != nil {
			return err
		}
	}
	applyNoFlags(cmd, p)

	u.PlanBox("Plan", p.Lines())
	if !p.Yes && !p.DryRun {
		ok, err := u.Confirm("Press Enter / y to apply")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("cancelled")
		}
	}
	return engine.Apply(*p, u)
}

func runApply(p *plan.Plan) error {
	if err := preflight(p, true); err != nil {
		return err
	}
	if p.Dokploy {
		if err := dokploy.HasWebhookHostOrDomain(p.WebhookHost); err != nil {
			return err
		}
	}
	u := newUI(p)
	host := sys.HostInfo()
	u.Banner(Version, host.Hostname, host.OSName, p.DryRun)
	u.PlanBox("Plan", p.Lines())
	if !p.Yes && !p.DryRun {
		ok, err := u.Confirm("Apply this plan?")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("cancelled")
		}
	}
	return engine.Apply(*p, u)
}

func fillRecommended(p *plan.Plan) {
	r := plan.Recommended()
	p.User = r.User
	p.UFW = r.UFW
	p.SSH = r.SSH
	p.Fail2ban = r.Fail2ban
	p.Updates = r.Updates
	p.Docker = sys.DockerInstalled()
	if p.Username == "" {
		p.Username = r.Username
	}
	if dokploy.Detected() {
		p.Dokploy = true
	}
}

func applyNoFlags(cmd *cobra.Command, p *plan.Plan) {
	no := func(name string) bool {
		f := cmd.Flags().Lookup(name)
		if f != nil && f.Changed && f.Value.String() == "true" {
			return true
		}
		f = cmd.PersistentFlags().Lookup(name)
		return f != nil && f.Changed && f.Value.String() == "true"
	}
	if no("no-user") {
		p.User = false
	}
	if no("no-ufw") {
		p.UFW = false
	}
	if no("no-ssh") {
		p.SSH = false
	}
	if no("no-fail2ban") {
		p.Fail2ban = false
	}
	if no("no-updates") {
		p.Updates = false
	}
	if no("no-docker") {
		p.Docker = false
	}
}

func flagChanged(cmd *cobra.Command, name string) bool {
	if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
		return true
	}
	if f := cmd.PersistentFlags().Lookup(name); f != nil && f.Changed {
		return true
	}
	return false
}

func positiveModuleChanged(cmd *cobra.Command) bool {
	for _, name := range []string{"user", "ufw", "ssh", "fail2ban", "updates", "docker", "addon"} {
		if flagChanged(cmd, name) {
			return true
		}
	}
	return false
}

func newUI(p *plan.Plan) *ui.UI {
	return ui.New(ui.Options{Plain: p.Plain, Quiet: p.Quiet, Verbose: p.Verbose})
}

func preflight(p *plan.Plan, needDebian bool) error {
	if p.DryRun {
		return nil
	}
	if err := sys.RequireRoot(); err != nil {
		return err
	}
	if needDebian {
		return sys.RequireDebian()
	}
	return nil
}

func Execute() {
	root := New()
	if err := root.Execute(); err != nil {
		u := ui.New(ui.Options{Plain: os.Getenv("NO_COLOR") != ""})
		u.Error(err.Error())
		os.Exit(1)
	}
}
