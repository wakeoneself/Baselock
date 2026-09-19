package dokploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestrictRuleKeepsHost(t *testing.T) {
	got := RestrictRule("Host(`dokploy.example.com`)")
	if !strings.Contains(got, "Host(`dokploy.example.com`)") {
		t.Fatalf("host lost: %s", got)
	}
	if !strings.Contains(got, "/api/deploy") || !strings.Contains(got, "/api/providers") {
		t.Fatalf("paths missing: %s", got)
	}
}

func TestRestrictRuleIdempotent(t *testing.T) {
	once := RestrictRule("Host(`x.com`)")
	twice := RestrictRule(once)
	if once != twice {
		t.Fatalf("not idempotent:\n%s\n%s", once, twice)
	}
}

func TestExtractHostFromRule(t *testing.T) {
	h := ExtractHostFromRule("Host(`deploy.example.com`) && PathPrefix(`/api`)")
	if h != "deploy.example.com" {
		t.Fatalf("got %q", h)
	}
}

func TestRestrictPanelRoutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dokploy.yml")
	src := `
http:
  routers:
    dokploy-router-app:
      rule: Host(` + "`panel.example.com`" + `)
      service: dokploy-service-app
      entryPoints:
        - web
  services:
    dokploy-service-app:
      loadBalancer:
        servers:
          - url: http://dokploy:3000
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	tf, err := LoadTraefik(path)
	if err != nil {
		t.Fatal(err)
	}
	if !tf.LooksLikeDokployPanel() {
		t.Fatal("expected panel detection")
	}
	if !tf.RestrictPanelRoutes() {
		t.Fatal("expected change")
	}
	if tf.DetectPublicHost() != "panel.example.com" {
		t.Fatalf("host = %q", tf.DetectPublicHost())
	}
	rules := tf.RouterRules()
	if len(rules) != 1 || !strings.Contains(rules[0], "PathPrefix(`/api/deploy`)") {
		t.Fatalf("rules = %#v", rules)
	}
}

func TestWebhookOnlyDoc(t *testing.T) {
	doc := WebhookOnlyDoc("git.example.com")
	http := doc["http"].(map[string]any)
	routers := http["routers"].(map[string]any)
	r := routers["dokploy-webhooks"].(map[string]any)
	rule := r["rule"].(string)
	if !strings.Contains(rule, "git.example.com") || !strings.Contains(rule, "/api/deploy") {
		t.Fatalf("rule = %s", rule)
	}
}
