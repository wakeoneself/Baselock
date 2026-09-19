package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

const VersionFallback = "0.1.0"

type Options struct {
	Plain   bool
	Quiet   bool
	Verbose bool
	Out     io.Writer
	Err     io.Writer
	In      io.Reader
}

type UI struct {
	plain   bool
	quiet   bool
	verbose bool
	tty     bool
	color   bool
	out     io.Writer
	err     io.Writer
	in      io.Reader

	success lipgloss.Style
	warn    lipgloss.Style
	fail    lipgloss.Style
	info    lipgloss.Style
	muted   lipgloss.Style
	title   lipgloss.Style
	box     lipgloss.Style
}

func New(opt Options) *UI {
	out := opt.Out
	if out == nil {
		out = os.Stdout
	}
	errW := opt.Err
	if errW == nil {
		errW = os.Stderr
	}
	in := opt.In
	if in == nil {
		in = os.Stdin
	}

	tty := false
	if f, ok := out.(*os.File); ok {
		tty = isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
	}

	noColor := os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"
	color := tty && !opt.Plain && !noColor

	u := &UI{
		plain:   opt.Plain || !tty || noColor,
		quiet:   opt.Quiet,
		verbose: opt.Verbose && !opt.Quiet,
		tty:     tty,
		color:   color,
		out:     out,
		err:     errW,
		in:      in,
	}
	u.initStyles()
	return u
}

func (u *UI) initStyles() {
	if !u.color {
		u.success = lipgloss.NewStyle()
		u.warn = lipgloss.NewStyle()
		u.fail = lipgloss.NewStyle()
		u.info = lipgloss.NewStyle()
		u.muted = lipgloss.NewStyle()
		u.title = lipgloss.NewStyle().Bold(true)
		u.box = lipgloss.NewStyle().Padding(0, 1)
		return
	}
	u.success = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	u.warn = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	u.fail = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	u.info = lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	u.muted = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	u.title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
	u.box = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("51")).
		Padding(0, 1)
}

func (u *UI) Color() bool { return u.color }
func (u *UI) TTY() bool   { return u.tty }
func (u *UI) Plain() bool { return u.plain }
func (u *UI) Quiet() bool { return u.quiet }

func (u *UI) emoji(s string) string {
	if u.plain {
		return ""
	}
	return s + " "
}

func (u *UI) Banner(version, host, osName string, dryRun bool) {
	if u.quiet {
		return
	}
	badge := ""
	if dryRun {
		badge = u.warn.Render(" DRY-RUN ")
	}
	line := fmt.Sprintf("%ssec%s  v%s  ·  %s  ·  %s%s",
		u.emoji("🛡️"),
		"",
		version,
		host,
		osName,
		badge,
	)
	if u.plain {
		fmt.Fprintln(u.out, strings.TrimSpace(fmt.Sprintf("sec v%s · %s · %s%s", version, host, osName, map[bool]string{true: " [dry-run]", false: ""}[dryRun])))
		return
	}
	fmt.Fprintln(u.out, u.box.Render(u.title.Render(line)))
	fmt.Fprintln(u.out)
}

func (u *UI) PlanBox(title string, lines []string) {
	if u.quiet {
		return
	}
	body := title + "\n" + strings.Join(lines, "\n")
	if u.plain {
		fmt.Fprintln(u.out, title)
		for _, l := range lines {
			fmt.Fprintln(u.out, "  "+l)
		}
		return
	}
	fmt.Fprintln(u.out, u.box.Render(body))
	fmt.Fprintln(u.out)
}

func (u *UI) NextStep(title string, lines []string) {
	if u.quiet {
		fmt.Fprintln(u.out, title)
		for _, l := range lines {
			fmt.Fprintln(u.out, l)
		}
		return
	}
	prefixed := make([]string, 0, len(lines)+1)
	prefixed = append(prefixed, u.success.Render(u.emoji("✓")+title))
	for _, l := range lines {
		prefixed = append(prefixed, "  "+l)
	}
	u.PlanBox(u.emoji("🚀")+"What to do next", prefixed)
}

func (u *UI) Detail(msg string) {
	if !u.verbose || u.quiet {
		return
	}
	fmt.Fprintln(u.out, u.muted.Render("    "+msg))
}

func (u *UI) Info(msg string) {
	if u.quiet {
		return
	}
	fmt.Fprintln(u.out, u.info.Render(u.emoji("·")+msg))
}

func (u *UI) Warn(msg string) {
	fmt.Fprintln(u.err, u.warn.Render(u.emoji("⚠")+msg))
}

func (u *UI) Error(msg string) {
	fmt.Fprintln(u.err, u.fail.Render(u.emoji("✗")+msg))
}

func (u *UI) Success(msg string) {
	if u.quiet {
		return
	}
	fmt.Fprintln(u.out, u.success.Render(u.emoji("✓")+msg))
}

func (u *UI) FailDetail(what, why, how string) {
	u.Error(what)
	if why != "" {
		fmt.Fprintln(u.err, u.muted.Render("    why:  "+why))
	}
	if how != "" {
		fmt.Fprintln(u.err, u.muted.Render("    fix:  "+how))
	}
}

type Row struct {
	Name   string
	Level  string // OK WARN FAIL SKIP
	Reason string
	Next   string
}

func (u *UI) Table(rows []Row) {
	if len(rows) == 0 {
		return
	}
	nameW, levelW := 12, 6
	for _, r := range rows {
		if len(r.Name) > nameW {
			nameW = len(r.Name)
		}
		if len(r.Level) > levelW {
			levelW = len(r.Level)
		}
	}
	for _, r := range rows {
		mark := "·"
		style := u.muted
		switch r.Level {
		case "OK":
			mark = "✓"
			style = u.success
		case "WARN":
			mark = "⚠"
			style = u.warn
		case "FAIL":
			mark = "✗"
			style = u.fail
		}
		if u.plain {
			mark = r.Level
		}
		line := fmt.Sprintf("%s  %-*s  %-*s  %s", mark, nameW, r.Name, levelW, r.Level, r.Reason)
		if r.Next != "" {
			line += "  → " + r.Next
		}
		fmt.Fprintln(u.out, style.Render(line))
	}
}

func (u *UI) Confirm(prompt string) (bool, error) {
	if !u.tty {
		return false, fmt.Errorf("run from a terminal or pass --yes")
	}
	fmt.Fprint(u.out, u.info.Render(prompt+" [y/N] "))
	reader := bufio.NewReader(u.in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes", nil
}

func (u *UI) Step(title string, fn func() error) error {
	if u.quiet || u.plain || !u.tty {
		if !u.quiet {
			fmt.Fprintln(u.out, title+"...")
		}
		err := fn()
		if err != nil {
			u.Error(title + " failed")
			return err
		}
		if !u.quiet {
			u.Success(title)
		}
		return nil
	}

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	done := make(chan error, 1)
	go func() { done <- fn() }()

	var mu sync.Mutex
	stop := make(chan struct{})
	go func() {
		i := 0
		t := time.NewTicker(80 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				mu.Lock()
				fmt.Fprintf(u.out, "\r%s %s", u.info.Render(frames[i%len(frames)]), title)
				mu.Unlock()
				i++
			}
		}
	}()

	err := <-done
	close(stop)
	time.Sleep(90 * time.Millisecond)
	mu.Lock()
	fmt.Fprint(u.out, "\r\033[2K")
	mu.Unlock()
	if err != nil {
		fmt.Fprintln(u.out, u.fail.Render("✗ "+title))
		return err
	}
	fmt.Fprintln(u.out, u.success.Render("✓ "+title))
	return nil
}

func OnOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
