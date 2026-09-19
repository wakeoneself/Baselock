package dokploy

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/wakeoneself/Baselock/internal/backup"
	"github.com/wakeoneself/Baselock/internal/modules"
	"github.com/wakeoneself/Baselock/internal/sys"
	"github.com/wakeoneself/Baselock/internal/ui"
)

const (
	serviceName   = "dokploy"
	dokployRoot   = "/etc/dokploy"
	webhookFile   = "/etc/dokploy/traefik/dynamic/sec-webhooks.yml"
	defaultUIPort = "3000"
)

type Addon struct{}

func (Addon) Name() string { return "dokploy" }

func Detected() bool {
	if sys.CommandExists("docker") {
		if _, err := sys.Run("docker", "service", "inspect", serviceName); err == nil {
			return true
		}
	}
	return sys.FileExists(dokployRoot)
}

func (Addon) Apply(ctx modules.Context) error {
	return Lock(ctx)
}

func Lock(ctx modules.Context) error {
	if !Detected() {
		return fmt.Errorf("Dokploy not found (no docker service %q and no %s)", serviceName, dokployRoot)
	}

	files, _ := FindPanelFiles("")
	host := ctx.Plan.WebhookHost
	if host == "" {
		for _, f := range files {
			tf, err := LoadTraefik(f)
			if err != nil {
				continue
			}
			if h := tf.DetectPublicHost(); h != "" {
				host = h
				break
			}
		}
	}
	if host == "" && len(files) == 0 {
		return fmt.Errorf("Dokploy has no public panel domain; pass --webhook-host so git webhooks keep working on 80/443")
	}

	if ctx.Snapshot != nil {
		if spec, err := sys.Run("docker", "service", "inspect", serviceName); err == nil {
			_ = ctx.Snapshot.SaveNote("dokploy-service.json", []byte(spec))
		}
		for _, f := range files {
			_ = ctx.Snapshot.SaveFile(f)
		}
		_ = ctx.Snapshot.SaveFile(webhookFile)
		_ = ctx.Snapshot.SaveNote("dokploy-webhook-host", []byte(host))
	}

	if ctx.DryRun {
		ctx.UI.Detail("would rebind Dokploy UI to 127.0.0.1:" + defaultUIPort)
		ctx.UI.Detail("would keep 80/443 open for apps and webhooks")
		if len(files) > 0 {
			ctx.UI.Detail("would restrict panel Traefik routers to /api/deploy and /api/providers")
			for _, f := range files {
				ctx.UI.Detail("  " + f)
			}
		} else {
			ctx.UI.Detail("would write webhook-only Traefik file for Host(`" + host + "`)")
		}
		return nil
	}

	if err := rebindLoopback(ctx.UI); err != nil {
		return err
	}
	if err := denyPublic3000(ctx.UI); err != nil {
		ctx.UI.Detail("firewall 3000: " + err.Error())
	}

	if len(files) > 0 {
		for _, f := range files {
			tf, err := LoadTraefik(f)
			if err != nil {
				return err
			}
			if tf.RestrictPanelRoutes() {
				if err := tf.Write(); err != nil {
					return err
				}
				ctx.UI.Detail("restricted " + f)
			} else {
				ctx.UI.Detail(f + " already restricted")
			}
		}
	} else {
		if err := WriteWebhookOnly(webhookFile, host); err != nil {
			return err
		}
		ctx.UI.Detail("wrote " + webhookFile)
	}

	return nil
}

func Unlock(ctx modules.Context, snap *backup.Snapshot) error {
	if snap == nil {
		return fmt.Errorf("no snapshot to unlock from — run sec revert")
	}
	if ctx.DryRun {
		ctx.UI.Detail("would restore Dokploy publish and Traefik files")
		return nil
	}
	if spec, err := snap.Note("dokploy-service.json"); err == nil && len(spec) > 0 {
		if err := restorePublishFromInspect(spec); err != nil {
			ctx.UI.Detail("publish restore: " + err.Error())
			_ = sys.RunOK("docker", "service", "update",
				"--publish-rm", "published=3000,target=3000,mode=host",
				"--publish-add", "published=3000,target=3000,mode=host",
				serviceName)
		}
	}
	for path := range snap.Files {
		if strings.Contains(path, "dokploy") || strings.Contains(path, "traefik") {
			if err := snap.RestoreFile(path); err != nil {
				return err
			}
			ctx.UI.Detail("restored " + path)
		}
	}
	if sys.CommandExists("ufw") {
		_ = sys.RunOK("ufw", "delete", "deny", "3000/tcp")
	}
	return nil
}

func (Addon) Revert(ctx modules.Context, snap *backup.Snapshot) error {
	return Unlock(ctx, snap)
}

func rebindLoopback(u *ui.UI) error {
	if !sys.CommandExists("docker") {
		return fmt.Errorf("docker not found")
	}
	// Official removal of the public host publish.
	_ = sys.RunOK("docker", "service", "update",
		"--publish-rm", "published=3000,target=3000,mode=host",
		serviceName)

	// Prefer a loopback mapping so `ssh -L 3000:127.0.0.1:3000` works.
	if err := sys.RunOK("docker", "service", "update",
		"--publish-add", "127.0.0.1:3000:3000",
		serviceName); err != nil {
		u.Detail("ingress 127.0.0.1:3000 failed, trying host-mode publish + firewall")
		if err2 := sys.RunOK("docker", "service", "update",
			"--publish-add", "published=3000,target=3000,mode=host",
			serviceName); err2 != nil {
			return fmt.Errorf("rebind port 3000: %v / %v", err, err2)
		}
	}
	u.Detail("Dokploy UI published on 127.0.0.1:3000 (or host 3000 behind UFW)")
	return nil
}

func denyPublic3000(u *ui.UI) error {
	if !sys.CommandExists("ufw") {
		return fmt.Errorf("ufw not installed")
	}
	_ = sys.RunOK("ufw", "deny", "3000/tcp")
	if sys.FileExists("/usr/local/bin/ufw-docker") {
		_ = sys.RunOK("/usr/local/bin/ufw-docker", "deny", serviceName, "3000")
	}
	u.Detail("UFW denies public tcp/3000")
	return nil
}

func restorePublishFromInspect(raw []byte) error {
	var inspect []struct {
		Endpoint struct {
			Ports []struct {
				Protocol      string `json:"Protocol"`
				TargetPort    int    `json:"TargetPort"`
				PublishedPort int    `json:"PublishedPort"`
				PublishMode   string `json:"PublishMode"`
			} `json:"Ports"`
		} `json:"Endpoint"`
		Spec struct {
			EndpointSpec struct {
				Ports []struct {
					Protocol      string `json:"Protocol"`
					TargetPort    int    `json:"TargetPort"`
					PublishedPort int    `json:"PublishedPort"`
					PublishMode   string `json:"PublishMode"`
				} `json:"Ports"`
			} `json:"EndpointSpec"`
		} `json:"Spec"`
	}
	if err := json.Unmarshal(raw, &inspect); err != nil {
		return err
	}
	if len(inspect) == 0 {
		return fmt.Errorf("empty inspect")
	}
	ports := inspect[0].Spec.EndpointSpec.Ports
	if len(ports) == 0 {
		ports = inspect[0].Endpoint.Ports
	}
	args := []string{"service", "update"}
	_ = sys.RunOK("docker", "service", "update",
		"--publish-rm", "published=3000,target=3000,mode=host",
		"--publish-rm", "3000",
		serviceName)
	for _, p := range ports {
		if p.TargetPort != 3000 && p.PublishedPort != 3000 {
			continue
		}
		mode := p.PublishMode
		if mode == "" {
			mode = "host"
		}
		spec := fmt.Sprintf("published=%d,target=%d,mode=%s,protocol=%s",
			p.PublishedPort, p.TargetPort, mode, orDefault(p.Protocol, "tcp"))
		args = append(args, "--publish-add", spec)
	}
	if len(args) == 2 {
		return sys.RunOK("docker", "service", "update",
			"--publish-add", "published=3000,target=3000,mode=host",
			serviceName)
	}
	args = append(args, serviceName)
	return sys.RunOK("docker", args...)
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func (Addon) Status() modules.Check {
	if !Detected() {
		return modules.Check{Name: "dokploy", Level: "SKIP", Reason: "Dokploy not installed"}
	}
	public, loop := port3000View()
	if public {
		return modules.Check{
			Name:   "dokploy",
			Level:  "FAIL",
			Reason: "UI port 3000 is reachable on a public address",
			Next:   "run: sudo sec addon dokploy lock",
		}
	}
	if !loop {
		return modules.Check{
			Name:   "dokploy",
			Level:  "WARN",
			Reason: "port 3000 is not on loopback — tunnel may fail",
			Next:   "run: sudo sec addon dokploy lock",
		}
	}
	return modules.Check{
		Name:   "dokploy",
		Level:  "OK",
		Reason: "UI on 127.0.0.1:3000 · webhooks stay on 80/443",
	}
}

func port3000View() (public bool, loopback bool) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false, listenLocal3000()
	}
	publicIPs := map[string]bool{}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if ip4 := ipnet.IP.To4(); ip4 != nil {
			publicIPs[ip4.String()] = true
		}
	}
	loopback = listenLocal3000()
	for ip := range publicIPs {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, defaultUIPort), 400*time.Millisecond)
		if err == nil {
			_ = c.Close()
			public = true
			break
		}
	}
	return public, loopback
}

func listenLocal3000() bool {
	c, err := net.DialTimeout("tcp", "127.0.0.1:3000", 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func TunnelHint(username string) []string {
	if username == "" {
		username = "deploy"
	}
	host := sys.PublicIP()
	return []string{
		fmt.Sprintf("ssh -L 3000:127.0.0.1:3000 %s@%s", username, host),
		"open http://127.0.0.1:3000",
		"git webhooks stay on https://<host>/api/deploy/…  (80/443)",
	}
}

func PrintLockSummary(u *ui.UI, username, webhookHost string) {
	closed := []string{
		"Dokploy admin UI is off the public internet",
		"Raw IP:3000 is closed",
	}
	open := []string{
		"Apps on ports 80 and 443",
		"/api/deploy  (GitHub / GitLab / Gitea / Bitbucket auto-deploy)",
		"/api/providers  (git OAuth callbacks)",
	}
	if webhookHost != "" {
		open = append(open, "webhook host: "+webhookHost)
	}
	lines := []string{"Closed:"}
	for _, l := range closed {
		lines = append(lines, "  • "+l)
	}
	lines = append(lines, "Still public:")
	for _, l := range open {
		lines = append(lines, "  • "+l)
	}
	lines = append(lines, "")
	lines = append(lines, TunnelHint(username)...)
	u.PlanBox("🔒  Dokploy locked", lines)
}

func HasWebhookHostOrDomain(webhookHost string) error {
	if webhookHost != "" {
		return nil
	}
	files, _ := FindPanelFiles("")
	for _, f := range files {
		tf, err := LoadTraefik(f)
		if err != nil {
			continue
		}
		if tf.DetectPublicHost() != "" {
			return nil
		}
	}
	return fmt.Errorf("need --webhook-host (no Dokploy panel domain found) — lock would break git auto-deploy")
}
