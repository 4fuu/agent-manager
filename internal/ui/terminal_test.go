package ui

import (
	"testing"

	"github.com/4fuu/agent-manager/internal/supervisor"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestTerminalKeyboardMousePasteAndEscape(t *testing.T) {
	term := newTerminal()
	term.SetRect(34, 2, 90, 30)
	var sent []supervisor.Input
	term.send = func(i supervisor.Input) { sent = append(sent, i) }
	escaped := false
	term.escape = func() { escaped = true }
	focus := func(tview.Primitive) {}
	term.InputHandler()(tcell.NewEventKey(tcell.KeyUp, 0, 0), focus)
	term.InputHandler()(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModCtrl), focus)
	term.PasteHandler()("paste\ntext", focus)
	term.MouseHandler()(tview.MouseLeftDown, tcell.NewEventMouse(40, 8, tcell.Button1, 0), focus)
	term.MouseHandler()(tview.MouseLeftUp, tcell.NewEventMouse(40, 8, 0, 0), focus)
	term.InputHandler()(tcell.NewEventKey(tcell.KeyCtrlRightSq, 0, 0), focus)
	if !escaped || len(sent) != 5 || sent[0].Key.Code != uv.KeyUp || sent[1].Key.Mod&uv.ModCtrl == 0 || *sent[2].Paste != "paste\ntext" || sent[3].Mouse.X != 5 || sent[3].Mouse.Y != 5 || sent[4].MouseKind != "release" {
		t.Fatalf("routing mismatch: %+v", sent)
	}
	used, _ := term.MouseHandler()(tview.MouseLeftDown, tcell.NewEventMouse(5, 1, tcell.Button1, 0), focus)
	if used {
		t.Fatal("manager chrome mouse leaked to guest")
	}
	term.MouseHandler()(tview.MouseLeftDown, tcell.NewEventMouse(40, 8, tcell.Button1, 0), focus)
	_, capture := term.MouseHandler()(tview.MouseLeftUp, tcell.NewEventMouse(5, 1, 0, 0), focus)
	if capture != nil || sent[len(sent)-1].MouseKind != "release" || sent[len(sent)-1].Mouse.X != 0 {
		t.Fatal("drag must release in original pane and return mouse ownership")
	}
}
func TestMappingFormRoundTrip(t *testing.T) {
	ms, e := parseMappings("/host/auth | /guest/auth\n/host/settings | /guest/settings | rw")
	if e != nil || ms[0].Writable || !ms[1].Writable {
		t.Fatal(ms, e)
	}
	again, e := parseMappings(mappingText(ms))
	if e != nil || len(again) != 2 || !again[1].Writable {
		t.Fatal(again, e)
	}
	if _, e := parseMappings("/a | /b | yes"); e == nil {
		t.Fatal("ambiguous writable flag accepted")
	}
}

func TestApplicationRoutesCtrlCToGuest(t *testing.T) {
	u := New(nil)
	var got supervisor.Input
	u.term.send = func(in supervisor.Input) { got = in }
	u.app.SetFocus(u.term)
	remaining := u.app.GetInputCapture()(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModCtrl))
	if remaining != nil || got.Key == nil || got.Key.Code != 'c' || got.Key.Mod&uv.ModCtrl == 0 {
		t.Fatal("Ctrl+C escaped into tview's quit handler")
	}
}
