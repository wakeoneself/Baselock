package modules

import (
	"github.com/abyss/server-sec-cli/internal/backup"
	"github.com/abyss/server-sec-cli/internal/plan"
	"github.com/abyss/server-sec-cli/internal/ui"
)

type Check struct {
	Name   string
	Level  string // OK WARN FAIL SKIP
	Reason string
	Next   string
}

type Context struct {
	Plan     plan.Plan
	UI       *ui.UI
	Snapshot *backup.Snapshot
	DryRun   bool
}

type Module interface {
	Name() string
	Apply(ctx Context) error
	Status() Check
	Revert(ctx Context, snap *backup.Snapshot) error
}

func skip(name, reason string) Check {
	return Check{Name: name, Level: "SKIP", Reason: reason}
}

func ok(name, reason string) Check {
	return Check{Name: name, Level: "OK", Reason: reason}
}

func warn(name, reason, next string) Check {
	return Check{Name: name, Level: "WARN", Reason: reason, Next: next}
}

func fail(name, reason, next string) Check {
	return Check{Name: name, Level: "FAIL", Reason: reason, Next: next}
}
