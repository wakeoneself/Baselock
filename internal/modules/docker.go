package modules

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/wakeoneself/Baselock/internal/backup"
	"github.com/wakeoneself/Baselock/internal/sys"
)

const dockerDaemon = "/etc/docker/daemon.json"

type Docker struct{}

func (Docker) Name() string { return "docker" }

func (Docker) Apply(ctx Context) error {
	if !sys.DockerInstalled() {
		ctx.UI.Detail("Docker not installed — skipping daemon harden")
		return nil
	}
	if ctx.Snapshot != nil {
		_ = ctx.Snapshot.SaveFile(dockerDaemon)
	}

	current := map[string]any{}
	if data, err := os.ReadFile(dockerDaemon); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &current)
	}
	current["live-restore"] = true
	current["userland-proxy"] = false
	current["log-driver"] = "json-file"
	current["log-opts"] = map[string]string{
		"max-size": "10m",
		"max-file": "3",
	}
	raw, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	if ctx.DryRun {
		ctx.UI.Detail("would write " + dockerDaemon)
		ctx.UI.Detail(strings.TrimSpace(string(raw)))
		return nil
	}
	if err := sys.WriteFile(dockerDaemon, raw, 0o644); err != nil {
		return err
	}
	if err := sys.RunOK("systemctl", "restart", "docker"); err != nil {
		return fmt.Errorf("restart docker: %w", err)
	}
	ctx.UI.Detail("daemon.json: live-restore, log rotate, no userland-proxy")
	return nil
}

func (Docker) Status() Check {
	if !sys.HostInfo().Debian {
		return skip("docker", "not Ubuntu/Debian")
	}
	if !sys.DockerInstalled() {
		return skip("docker", "docker not installed")
	}
	if tcpDockerExposed() {
		return fail("docker", "Docker socket looks exposed over TCP", "remove dockerd -H tcp://…")
	}
	if !sys.FileExists(dockerDaemon) {
		return warn("docker", "no daemon.json hardening", "run: sudo sec --docker")
	}
	data, _ := os.ReadFile(dockerDaemon)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if live, _ := m["live-restore"].(bool); !live {
		return warn("docker", "daemon.json missing live-restore", "run: sudo sec --docker")
	}
	return ok("docker", "daemon.json hardened · no TCP socket")
}

func tcpDockerExposed() bool {
	if data, err := os.ReadFile("/lib/systemd/system/docker.service"); err == nil {
		if strings.Contains(string(data), "tcp://") {
			return true
		}
	}
	if data, err := os.ReadFile("/etc/systemd/system/docker.service.d/override.conf"); err == nil {
		if strings.Contains(string(data), "tcp://") {
			return true
		}
	}
	return false
}

func (Docker) Revert(ctx Context, snap *backup.Snapshot) error {
	if snap == nil {
		return fmt.Errorf("no snapshot")
	}
	if ctx.DryRun {
		ctx.UI.Detail("would restore " + dockerDaemon)
		return nil
	}
	if err := snap.RestoreFile(dockerDaemon); err != nil {
		return err
	}
	if sys.DockerInstalled() {
		return sys.RunOK("systemctl", "restart", "docker")
	}
	return nil
}
