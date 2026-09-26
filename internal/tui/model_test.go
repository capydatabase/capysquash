package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// sgr matches the colour and style escapes Lip Gloss emits.
var sgr = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

var (
	keySpace = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyHelp  = tea.KeyPressMsg{Code: '?', Text: "?"}
	keyQuit  = tea.KeyPressMsg{Code: 'q', Text: "q"}
	keyValid = tea.KeyPressMsg{Code: 'v', Text: "v"}
)

// newTestModel builds a model over a fixture migration set, sized like a
// terminal. The working directory is a temp dir because the progress view
// writes squashed/ and the config view saves relative to it.
func newTestModel(t *testing.T) *Model {
	t.Helper()
	dir, err := filepath.Abs("../../test-fixtures/fk_cycles/original")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	t.Chdir(tmp)

	m := NewModel(dir, filepath.Join(tmp, "capysquash.config.json"))
	drive(t, m, m.Init())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

// drive executes cmd the way the program would and feeds each resulting
// message back into the model until nothing is left to do.
func drive(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			drive(t, m, c)
		}
	default:
		_, next := m.Update(msg)
		drive(t, m, next)
	}
}

func press(t *testing.T, m *Model, key tea.KeyPressMsg) {
	t.Helper()
	_, cmd := m.Update(key)
	drive(t, m, cmd)
}

func content(m *Model) string {
	return m.View().Content
}

func TestKeysSwitchViews(t *testing.T) {
	m := newTestModel(t)

	if got := m.currentView.Type(); got != ViewDashboard {
		t.Fatalf("start view = %v, want dashboard", got)
	}

	// Space selects the first menu item (Analyze). Bubble Tea v2 reports it
	// as "space", not " ".
	press(t, m, keySpace)
	if got := m.currentView.Type(); got != ViewAnalysis {
		t.Fatalf("after space: view = %v, want analysis", got)
	}
	if c := content(m); !strings.Contains(c, "Migration Analysis") {
		t.Fatalf("analysis did not finish loading:\n%s", c)
	}

	press(t, m, keyEsc)
	if got := m.currentView.Type(); got != ViewDashboard {
		t.Fatalf("after esc: view = %v, want dashboard", got)
	}

	press(t, m, keyHelp)
	if got := m.currentView.Type(); got != ViewHelp {
		t.Fatalf("after ?: view = %v, want help", got)
	}
	press(t, m, keyHelp)
	if got := m.currentView.Type(); got != ViewDashboard {
		t.Fatalf("after second ?: view = %v, want dashboard", got)
	}

	press(t, m, keyDown)
	press(t, m, keyEnter)
	if got := m.currentView.Type(); got != ViewConfig {
		t.Fatalf("after down+enter: view = %v, want config", got)
	}

	_, cmd := m.Update(keyQuit)
	if cmd == nil {
		t.Fatal("q returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q did not quit")
	}
}

// `capysquash tui analyze|deps|config` start on a view other than the
// dashboard; that view must be entered (and load its data) on start.
func TestStartViewIsEntered(t *testing.T) {
	cases := []struct {
		view    ViewType
		heading string
	}{
		{ViewDashboard, "Migration Overview"},
		{ViewAnalysis, "Migration Analysis"},
		{ViewDependencyGraph, "Dependency Graph - Forward Dependencies"},
		{ViewConfig, "Configuration Wizard"},
	}
	for _, tc := range cases {
		t.Run((&Model{}).getViewName(tc.view), func(t *testing.T) {
			dir, err := filepath.Abs("../../test-fixtures/fk_cycles/original")
			if err != nil {
				t.Fatal(err)
			}
			t.Chdir(t.TempDir())

			m := NewModel(dir, "capysquash.config.json")
			m.startAt(tc.view)
			drive(t, m, m.Init())
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

			if got := m.currentView.Type(); got != tc.view {
				t.Fatalf("view = %v, want %v", got, tc.view)
			}
			if c := content(m); !strings.Contains(c, tc.heading) {
				t.Fatalf("start view did not load; missing %q in:\n%s", tc.heading, c)
			}
		})
	}
}

func TestEveryViewRenders(t *testing.T) {
	cases := []struct {
		view    ViewType
		heading string
	}{
		{ViewDashboard, "capysquash Interactive Dashboard"},
		{ViewAnalysis, "Migration Analysis"},
		{ViewConfig, "Configuration Wizard"},
		{ViewDependencyGraph, "Dependency Graph"},
		{ViewProgress, "Migration Squashing Progress"},
		{ViewValidation, "Migration Validation"},
		{ViewHelp, "Help & Keyboard Shortcuts"},
	}
	for _, tc := range cases {
		t.Run((&Model{}).getViewName(tc.view), func(t *testing.T) {
			m := newTestModel(t)
			_, cmd := m.navigateTo(tc.view)
			drive(t, m, cmd)

			v := m.View()
			if !v.AltScreen {
				t.Error("view does not request the alternate screen")
			}
			if !strings.Contains(v.Content, tc.heading) {
				t.Errorf("missing %q in:\n%s", tc.heading, v.Content)
			}
			if !strings.Contains(v.Content, m.getViewName(tc.view)) {
				t.Errorf("status bar does not name the view %q", m.getViewName(tc.view))
			}
		})
	}
}

// The edit box was Width(40) in Lip Gloss v1, which excluded the border. v2
// includes it, so the port uses Width(42); the rendered box must still be 42
// columns wide. Every line of it is indented two columns like the rest of
// the field (only the top border used to be).
func TestConfigEditBox(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.navigateTo(ViewConfig)
	drive(t, m, cmd)
	press(t, m, keyEnter)

	var box []string
	for line := range strings.SplitSeq(content(m), "\n") {
		// JoinVertical pads every line to the widest one; drop that.
		plain := strings.TrimRight(sgr.ReplaceAllString(line, ""), " ")
		if trimmed := strings.TrimLeft(plain, " "); strings.HasPrefix(trimmed, "╭") ||
			strings.HasPrefix(trimmed, "│") || strings.HasPrefix(trimmed, "╰") {
			box = append(box, plain)
		}
	}
	if len(box) != 5 {
		t.Fatalf("want a 5-line edit box, found %d lines:\n%s", len(box), content(m))
	}
	for _, line := range box {
		if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			t.Errorf("box line not indented by two columns: %q", line)
		}
		if got := lipgloss.Width(line); got != 2+42 {
			t.Errorf("box line is %d columns, want 2+42: %q", got, line)
		}
	}
}

// The status bar must fit on one line at the terminal width; it used to
// ignore its own padding and wrap "Quit" onto a second line.
func TestStatusBarFitsOneLine(t *testing.T) {
	for _, width := range []int{80, 120, 160} {
		m := newTestModel(t)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 40})

		bar := m.renderStatusBar()
		if h := lipgloss.Height(bar); h != 1 {
			t.Errorf("width %d: status bar is %d lines:\n%s", width, h, bar)
		}
		if w := lipgloss.Width(bar); w != width {
			t.Errorf("width %d: status bar is %d columns", width, w)
		}
	}
}

// Esc while editing a config field cancels the edit and stays in the
// wizard; a second esc returns to the dashboard.
func TestEscCancelsConfigEdit(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.navigateTo(ViewConfig)
	drive(t, m, cmd)

	press(t, m, keyEnter)
	if !strings.Contains(content(m), "◄") {
		t.Fatalf("enter did not open the edit box:\n%s", content(m))
	}

	press(t, m, keyEsc)
	if got := m.currentView.Type(); got != ViewConfig {
		t.Fatalf("esc while editing left the wizard for %v", got)
	}
	if c := content(m); strings.Contains(c, "◄") || !strings.Contains(c, "Enter: Edit") {
		t.Fatalf("esc did not cancel the edit:\n%s", c)
	}

	press(t, m, keyEsc)
	if got := m.currentView.Type(); got != ViewDashboard {
		t.Fatalf("esc outside an edit: view = %v, want dashboard", got)
	}
}

// v toggles the validation view from anywhere; entering it lints the
// migrations. The fixture has hygiene findings only, so it passes with
// warnings.
func TestValidationKey(t *testing.T) {
	m := newTestModel(t)

	press(t, m, keyValid)
	if got := m.currentView.Type(); got != ViewValidation {
		t.Fatalf("after v: view = %v, want validation", got)
	}
	c := content(m)
	for _, want := range []string{"Validation Passed", "001_create_posts.sql:6 [hygiene] CSQ.HYGIENE.PREFER_BIGINT", "v: Validation"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q in:\n%s", want, c)
		}
	}

	for line := range strings.SplitSeq(c, "\n") {
		if w := lipgloss.Width(line); w > 120 {
			t.Errorf("line is %d columns, wider than the 120-column terminal: %q", w, line)
		}
	}

	press(t, m, keyValid)
	if got := m.currentView.Type(); got != ViewDashboard {
		t.Fatalf("after second v: view = %v, want dashboard", got)
	}

	press(t, m, keyHelp)
	if !strings.Contains(content(m), "Toggle migration validation") {
		t.Error("help does not list the v binding")
	}
}

// Breaking and safety findings fail validation, as they fail lint; files
// that do not parse are errors too.
func TestValidationFails(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"001_create.sql": "CREATE TABLE users (id bigint PRIMARY KEY);\n",
		"002_drop.sql":   "DROP TABLE users;\n",
		"003_broken.sql": "CREATE TABLE (;\n",
	}
	for name, sql := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(sql), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(t.TempDir())

	m := NewModel(dir, "capysquash.config.json")
	m.startAt(ViewValidation)
	drive(t, m, m.Init())
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})

	c := content(m)
	for _, want := range []string{"Validation Failed", "002_drop.sql:1 [breaking]", "003_broken.sql: failed to parse SQL"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q in:\n%s", want, c)
		}
	}
}
