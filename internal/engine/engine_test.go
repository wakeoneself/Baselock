package engine

import (
	"testing"

	"github.com/abyss/server-sec-cli/internal/plan"
)

func TestEnabledOrder(t *testing.T) {
	p := plan.Recommended()
	p.Dokploy = true
	mods := Enabled(p)
	var names []string
	for _, m := range mods {
		names = append(names, m.Name())
	}
	want := []string{"user", "ssh", "ufw", "fail2ban", "updates", "docker", "dokploy"}
	if len(names) != len(want) {
		t.Fatalf("got %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got %v want %v", names, want)
		}
	}
}
