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

	prev, _ := os.ReadFile(dockerDaemon)
	current := map[string]any{}
	if len(prev) > 0 {
		if err := json.Unmarshal(prev, &current); err != nil {
			return fmt.Errorf("parse %s: %w", dockerDaemon, err)
		}
	}

	swarm := sys.DockerSwarmActive()
	if swarm {
		// live-restore is incompatible with Swarm (Dokploy). dockerd refuses to start.
		delete(current, "live-restore")
		ctx.UI.Detail("Swarm is active — skipping live-restore (would break dockerd)")
	} else {
		current["live-restore"] = true
	}
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
	// Never restart dockerd here — on Dokploy/Swarm that can take down
	// networking and leave sshd dead. File is picked up on the next reboot.
	if swarm {
		ctx.UI.Detail("daemon.json: log rotate · Swarm (no live-restore) · no docker restart")
	} else {
		ctx.UI.Detail("daemon.json: live-restore, log rotate · no docker restart (reboot later)")
	}
	return nil
}

func restoreDaemonJSON(prev []byte) error {
	if len(prev) == 0 {
		if sys.FileExists(dockerDaemon) {
			return os.Remove(dockerDaemon)
		}
		return nil
	}
	return sys.WriteFile(dockerDaemon, prev, 0o644)
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
	if sys.DockerSwarmActive() {
		if live, _ := m["live-restore"].(bool); live {
			return warn("docker", "live-restore is set on a Swarm node (can prevent dockerd from starting)", "remove live-restore from daemon.json")
		}
		return ok("docker", "daemon.json hardened · Swarm · no TCP socket")
	}
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
	ctx.UI.Detail("restored daemon.json — reboot later if dockerd should reload it")
	return nil
}
