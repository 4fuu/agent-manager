package ui

import (
	"github.com/4fuu/agent-manager/internal/supervisor"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type terminal struct {
	*tview.Box
	id, label    string
	preferred    int
	pendingWidth int
	frame        supervisor.Frame
	send         func(supervisor.Input)
	escape       func()
	resize       func(int, int)
	rows, cols   int
	button       uv.MouseButton
}

func newTerminal() *terminal {
	t := &terminal{Box: tview.NewBox()}
	t.SetBackgroundColor(tcell.ColorDefault).SetBorder(true).SetTitle(" Terminal ")
	return t
}
func (t *terminal) Draw(s tcell.Screen) {
	t.Box.DrawForSubclass(s, t)
	x, y, w, h := t.GetInnerRect()
	if w > 0 && h > 0 && (w != t.cols || h != t.rows) {
		t.cols, t.rows = w, h
		if t.resize != nil {
			t.resize(h, w)
		}
	}
	for row := 0; row < h && row < t.frame.Height; row++ {
		for col := 0; col < w && col < t.frame.Width; col++ {
			i := row*t.frame.Width + col
			if i >= len(t.frame.Cells) {
				continue
			}
			c := t.frame.Cells[i]
			r := []rune(c.Text)
			if len(r) == 0 {
				continue
			}
			st := tcell.StyleDefault
			if c.FG != "" {
				st = st.Foreground(tcell.GetColor(c.FG))
			}
			if c.BG != "" {
				st = st.Background(tcell.GetColor(c.BG))
			}
			st = st.Bold(c.Attr&uv.AttrBold != 0).Dim(c.Attr&uv.AttrFaint != 0).Italic(c.Attr&uv.AttrItalic != 0).Reverse(c.Attr&uv.AttrReverse != 0).Blink(c.Attr&uv.AttrBlink != 0).StrikeThrough(c.Attr&uv.AttrStrikethrough != 0).Underline(c.Underline)
			s.SetContent(x+col, y+row, r[0], r[1:], st)
		}
	}
	if t.HasFocus() && t.frame.Live && t.frame.CursorX < w && t.frame.CursorY < h {
		s.ShowCursor(x+t.frame.CursorX, y+t.frame.CursorY)
	}
}
func key(e *tcell.EventKey) uv.Key {
	k := uv.Key{}
	if e.Modifiers()&tcell.ModAlt != 0 {
		k.Mod |= uv.ModAlt
	}
	if e.Modifiers()&tcell.ModShift != 0 {
		k.Mod |= uv.ModShift
	}
	if e.Modifiers()&tcell.ModCtrl != 0 {
		k.Mod |= uv.ModCtrl
	}
	switch e.Key() {
	case tcell.KeyRune:
		k.Code = e.Rune()
		k.Text = string(e.Rune())
	case tcell.KeyEnter:
		k.Code = uv.KeyEnter
	case tcell.KeyTab:
		k.Code = uv.KeyTab
	case tcell.KeyBacktab:
		k.Code = uv.KeyTab
		k.Mod |= uv.ModShift
	case tcell.KeyEscape:
		k.Code = uv.KeyEscape
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		k.Code = uv.KeyBackspace
	case tcell.KeyUp:
		k.Code = uv.KeyUp
	case tcell.KeyDown:
		k.Code = uv.KeyDown
	case tcell.KeyLeft:
		k.Code = uv.KeyLeft
	case tcell.KeyRight:
		k.Code = uv.KeyRight
	case tcell.KeyHome:
		k.Code = uv.KeyHome
	case tcell.KeyEnd:
		k.Code = uv.KeyEnd
	case tcell.KeyPgUp:
		k.Code = uv.KeyPgUp
	case tcell.KeyPgDn:
		k.Code = uv.KeyPgDown
	case tcell.KeyDelete:
		k.Code = uv.KeyDelete
	case tcell.KeyInsert:
		k.Code = uv.KeyInsert
	default:
		if e.Key() >= tcell.KeyF1 && e.Key() <= tcell.KeyF64 {
			k.Code = uv.KeyF1 + rune(e.Key()-tcell.KeyF1)
		} else if e.Key() >= tcell.KeyCtrlA && e.Key() <= tcell.KeyCtrlZ {
			k.Code = 'a' + rune(e.Key()-tcell.KeyCtrlA)
			k.Mod |= uv.ModCtrl
		} else {
			k.Code = rune(e.Key())
		}
	}
	return k
}
func (t *terminal) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return t.WrapInputHandler(func(e *tcell.EventKey, _ func(tview.Primitive)) {
		if e.Key() == tcell.KeyCtrlRightSq {
			t.escape()
			return
		}
		k := key(e)
		t.send(supervisor.Input{Key: &k})
	})
}
func (t *terminal) PasteHandler() func(string, func(tview.Primitive)) {
	return t.WrapPasteHandler(func(text string, _ func(tview.Primitive)) { t.send(supervisor.Input{Paste: &text}) })
}
func (t *terminal) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return t.WrapMouseHandler(func(a tview.MouseAction, e *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		x, y := e.Position()
		ox, oy, w, h := t.GetInnerRect()
		if (x < ox || y < oy || x >= ox+w || y >= oy+h) && t.button == uv.MouseNone {
			return false, nil
		}
		m := uv.Mouse{X: max(0, min(x-ox, w-1)), Y: max(0, min(y-oy, h-1)), Button: t.button}
		kind := ""
		if e.Modifiers()&tcell.ModAlt != 0 {
			m.Mod |= uv.ModAlt
		}
		if e.Modifiers()&tcell.ModShift != 0 {
			m.Mod |= uv.ModShift
		}
		if e.Modifiers()&tcell.ModCtrl != 0 {
			m.Mod |= uv.ModCtrl
		}
		switch a {
		case tview.MouseLeftDown:
			m.Button = uv.MouseLeft
			kind = "press"
		case tview.MouseMiddleDown:
			m.Button = uv.MouseMiddle
			kind = "press"
		case tview.MouseRightDown:
			m.Button = uv.MouseRight
			kind = "press"
		case tview.MouseLeftUp, tview.MouseMiddleUp, tview.MouseRightUp:
			kind = "release"
		case tview.MouseMove:
			kind = "motion"
		case tview.MouseScrollUp:
			m.Button = uv.MouseWheelUp
			kind = "wheel"
		case tview.MouseScrollDown:
			m.Button = uv.MouseWheelDown
			kind = "wheel"
		}
		if kind == "press" {
			focus(t)
			t.button = m.Button
		}
		if kind != "" {
			t.send(supervisor.Input{Mouse: &m, MouseKind: kind})
		}
		if kind == "release" {
			t.button = uv.MouseNone
		}
		if t.button != uv.MouseNone {
			return true, t
		}
		return true, nil
	})
}
