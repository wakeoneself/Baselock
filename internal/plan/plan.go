package plan

type Plan struct {
	User           bool
	Username       string
	SSHPubKey      string
	CopyRootKeys   bool
	NoPasswdSudo   bool
	PurgeUser      bool
	DisableRootSSH bool

	UFW      bool
	SSH      bool
	Fail2ban bool
	Updates  bool
	Docker   bool

	Dokploy     bool
	AddonName   string
	WebhookHost string

	DryRun  bool
	Yes     bool
	Plain   bool
	Quiet   bool
	Verbose bool
	Profile string
}

func Recommended() Plan {
	return Plan{
		User:         true,
		Username:     "deploy",
		CopyRootKeys: true,
		UFW:          true,
		SSH:          true,
		Fail2ban:     true,
		Updates:      true,
		Docker:       true,
		Verbose:      true,
	}
}

func (p Plan) Lines() []string {
	var lines []string
	on := func(flag bool, label string) {
		state := "off"
		if flag {
			state = "on"
		}
		lines = append(lines, label+": "+state)
	}
	userLine := "user: off"
	if p.User {
		userLine = "user: on  (" + p.Username + ")"
	}
	lines = append(lines, userLine)
	on(p.SSH, "ssh")
	on(p.UFW, "ufw")
	on(p.Fail2ban, "fail2ban")
	on(p.Updates, "updates")
	on(p.Docker, "docker")
	if p.Dokploy {
		extra := ""
		if p.WebhookHost != "" {
			extra = "  webhook-host=" + p.WebhookHost
		}
		lines = append(lines, "addon dokploy: on"+extra)
	} else {
		lines = append(lines, "addon dokploy: off")
	}
	if p.DryRun {
		lines = append(lines, "mode: dry-run")
	}
	return lines
}

func (p Plan) Empty() bool {
	return !p.User && !p.UFW && !p.SSH && !p.Fail2ban && !p.Updates && !p.Docker && !p.Dokploy
}

func (p Plan) ModuleNames() []string {
	var names []string
	if p.User {
		names = append(names, "user")
	}
	if p.SSH {
		names = append(names, "ssh")
	}
	if p.UFW {
		names = append(names, "ufw")
	}
	if p.Fail2ban {
		names = append(names, "fail2ban")
	}
	if p.Updates {
		names = append(names, "updates")
	}
	if p.Docker {
		names = append(names, "docker")
	}
	if p.Dokploy {
		names = append(names, "dokploy")
	}
	return names
}
