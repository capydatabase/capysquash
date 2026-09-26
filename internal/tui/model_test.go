package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	keySpace = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyHelp  = tea.KeyPressMsg{Code: '?', Text: "?"}
	keyQuit  = tea.KeyPressMsg{Code: 'q', Text: "q"}
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
	run(t, m, m.Init())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

// run executes cmd the way the program would and feeds each resulting
// message back into the model until nothing is left to do.
func run(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			run(t, m, c)
		}
	default:
		_, next := m.Update(msg)
		run(t, m, next)
	}
}

func press(t *testing.T, m *Model, key tea.KeyPressMsg) {
	t.Helper()
	_, cmd := m.Update(key)
	run(t, m, cmd)
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
		{ViewValidation, "No validation results yet."},
		{ViewHelp, "Help & Keyboard Shortcuts"},
	}
	for _, tc := range cases {
		t.Run((&Model{}).getViewName(tc.view), func(t *testing.T) {
			m := newTestModel(t)
			_, cmd := m.navigateTo(tc.view)
			run(t, m, cmd)

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
// columns wide.
func TestConfigEditBoxWidth(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.navigateTo(ViewConfig)
	run(t, m, cmd)
	press(t, m, keyEnter)

	for line := range strings.SplitSeq(content(m), "\n") {
		if strings.Contains(line, "◄") {
			// JoinVertical pads every line to the widest one; drop that.
			if got := lipgloss.Width(strings.TrimRight(line, " ")); got != 42 {
				t.Fatalf("edit box line is %d columns, want 42: %q", got, line)
			}
			return
		}
	}
	t.Fatalf("no edit box rendered:\n%s", content(m))
}
