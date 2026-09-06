package ui

import (
	"testing"

	"github.com/4fuu/agent-manager/internal/manager"
	"github.com/4fuu/agent-manager/internal/supervisor"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestWorkspaceGeometryAndRouting(t *testing.T) {
	w := newWorkspace()
	w.SetRect(20, 1, 100, 30)
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(120, 33)
	for i := 0; i < 3; i++ {
		term := newTerminal()
		term.preferred = 60
		w.terms = append(w.terms, term)
	}
	w.Draw(s)
	if len(w.visible) != 2 || w.terms[1].cols != 60 {
		t.Fatal("partial column reflowed", w.visible, w.terms[1].cols)
	}
	w.active = 2
	w.Draw(s)
	if w.first != 2 {
		t.Fatal("focused terminal not revealed", w.first)
	}
	w.active = 0
	w.Draw(s)
	if w.first != 0 {
		t.Fatal("left navigation did not scroll back")
	}
	var got supervisor.Input
	w.terms[0].send = func(in supervisor.Input) { got = in }
	w.terms[0].Focus(func(tview.Primitive) {})
	root := tview.NewFlex().AddItem(w, 0, 1, true)
	root.InputHandler()(tcell.NewEventKey(tcell.KeyRune, 'z', 0), func(tview.Primitive) {})
	if got.Key == nil || got.Key.Code != 'z' {
		t.Fatal("root did not route input to nested workspace")
	}
	root.PasteHandler()("paste", func(tview.Primitive) {})
	if got.Paste == nil || *got.Paste != "paste" {
		t.Fatal("workspace paste lost")
	}
}

func TestSwitchSessionRetainsEntireWorkspace(t *testing.T) {
	u := New(nil)
	one := manager.Instance{ID: "first", ProjectID: "p", Status: "running", Panes: []manager.Pane{{ID: "agent", Width: 70}, {ID: "shell", Width: 50}}}
	two := manager.Instance{ID: "second", ProjectID: "p", Status: "running", Panes: []manager.Pane{{ID: "agent", Width: 80}}}
	u.state = manager.State{Instances: []manager.Instance{one, two}}
	u.selectNode("first")
	u.strip.active = 1
	u.strip.first = 1
	saved := u.strip
	u.selectNode("second")
	if u.strip == saved || len(u.strip.terms) != 1 {
		t.Fatal("sessions share strip")
	}
	u.selectNode("first")
	if u.strip != saved || u.strip.active != 1 || u.strip.first != 1 || u.strip.terms[1].preferred != 50 {
		t.Fatal("switch lost layout")
	}
	u.imageForm(manager.ImageProfile{})
	u.syncWorkspace(one)
	if !u.modalView.HasFocus() {
		t.Fatal("refresh stole modal focus")
	}
}

func TestPendingWidthSurvivesOlderSnapshot(t *testing.T) {
	u := New(nil)
	in := manager.Instance{ID: "session", Panes: []manager.Pane{{ID: "agent", Width: 80}}}
	u.syncWorkspace(in)
	term := u.strip.terms[0]
	term.preferred, term.pendingWidth = 30, 30
	u.syncWorkspace(in)
	if term.preferred != 30 || term.pendingWidth != 30 {
		t.Fatal("stale snapshot reset optimistic width")
	}
	in.Panes[0].Width = 30
	u.syncWorkspace(in)
	if term.preferred != 30 || term.pendingWidth != 0 {
		t.Fatal("matching snapshot did not acknowledge width")
	}
}
