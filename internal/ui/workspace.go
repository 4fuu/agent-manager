package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// A session owns a strip, not a split grid. Offscreen columns keep their size.
type workspace struct {
	*tview.Box
	terms         []*terminal
	active, first int
	onFocus       func(int)
	visible       []int
}

func newWorkspace() *workspace { return &workspace{Box: tview.NewBox()} }
func (w *workspace) HasFocus() bool {
	for _, t := range w.terms {
		if t.HasFocus() {
			return true
		}
	}
	return w.Box.HasFocus()
}
func (w *workspace) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return func(e *tcell.EventKey, focus func(tview.Primitive)) {
		for _, t := range w.terms {
			if t.HasFocus() {
				t.InputHandler()(e, focus)
				return
			}
		}
	}
}
func (w *workspace) PasteHandler() func(string, func(tview.Primitive)) {
	return func(text string, focus func(tview.Primitive)) {
		for _, t := range w.terms {
			if t.HasFocus() {
				t.PasteHandler()(text, focus)
				return
			}
		}
	}
}
func (w *workspace) Draw(s tcell.Screen) {
	w.Box.DrawForSubclass(s, w)
	x, y, width, height := w.GetInnerRect()
	if width < 1 || height < 2 {
		return
	}
	if len(w.terms) == 0 {
		tview.Print(s, "A workspace for your agents", x+3, y+height/3, max(0, width-6), tview.AlignLeft, accent)
		tview.Print(s, "Create a session to begin. Each session has its own guest disk.", x+3, y+height/3+2, max(0, width-6), tview.AlignLeft, muted)
		tview.Print(s, "n  New session     p  New project     Space  Actions", x+3, y+height/3+4, max(0, width-6), tview.AlignLeft, muted)
		return
	}
	w.active = max(0, min(w.active, len(w.terms)-1))
	w.first = min(w.first, w.active)
	columnWidth := func(i int) int { return min(max(30, w.terms[i].preferred)+2, width) }
	// Reveal the whole focused column, keeping partial neighbours as overflow cues.
	used := 0
	for i := w.first; i <= w.active; i++ {
		used += columnWidth(i)
	}
	for used > width && w.first < w.active {
		used -= columnWidth(w.first)
		w.first++
	}
	left, right := " ", " "
	if w.first > 0 {
		left = "‹"
	}
	used = 0
	for i := w.first; i < len(w.terms); i++ {
		used += columnWidth(i)
	}
	if used > width {
		right = "›"
	}
	tview.Print(s, fmt.Sprintf(" %s  TERMINALS   %02d / %02d", left, w.active+1, len(w.terms)), x, y, width, tview.AlignLeft, muted)
	tview.Print(s, right, x+width-2, y, 1, tview.AlignLeft, accent)
	w.visible = nil
	offset := 0
	for i := w.first; i < len(w.terms) && offset < width; i++ {
		t := w.terms[i]
		cw := columnWidth(i)
		t.SetRect(x+offset, y+1, cw, height-1)
		title := t.frame.Title
		if title == "" {
			title = t.label
		}
		color := muted
		marker := "○"
		if t.frame.Live {
			marker = "●"
		}
		if i == w.active {
			color = accent
		}
		if t.HasFocus() {
			marker = "▸"
		}
		t.SetBorderColor(color).SetTitleColor(color).SetTitle(fmt.Sprintf(" %s %02d · %s ", marker, i+1, tview.Escape(title))).SetTitleAlign(tview.AlignLeft)
		t.Draw(clippedScreen{Screen: s, x: x + offset, y: y + 1, w: min(cw, width-offset), h: height - 1})
		w.visible = append(w.visible, i)
		offset += cw
	}
}

func (w *workspace) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return w.WrapMouseHandler(func(a tview.MouseAction, e *tcell.EventMouse, focus func(tview.Primitive)) (bool, tview.Primitive) {
		x, y := e.Position()
		ox, oy, width, height := w.GetInnerRect()
		if x < ox || y < oy || x >= ox+width || y >= oy+height {
			return false, nil
		}
		if y == oy && len(w.terms) > 0 {
			delta := 0
			if a == tview.MouseScrollDown {
				delta = 1
			}
			if a == tview.MouseScrollUp {
				delta = -1
			}
			if a == tview.MouseLeftDown {
				if x > ox+width/2 {
					delta = 1
				} else {
					delta = -1
				}
			}
			if delta != 0 {
				w.active = max(0, min(w.active+delta, len(w.terms)-1))
				if w.onFocus != nil {
					w.onFocus(w.active)
				}
			}
			return true, nil
		}
		for _, i := range w.visible {
			t := w.terms[i]
			tx, ty, tw, th := t.GetRect()
			if x < tx || y < ty || x >= tx+tw || y >= ty+th {
				continue
			}
			if a == tview.MouseLeftDown && w.onFocus != nil {
				w.onFocus(i)
			}
			if y == ty {
				return true, nil
			}
			return t.MouseHandler()(a, e, func(p tview.Primitive) {
				if w.onFocus != nil {
					w.onFocus(i)
				} else {
					focus(p)
				}
			})
		}
		return true, nil
	})
}

// Clip painting, not the PTY dimensions: a partial trailing column must not reflow.
type clippedScreen struct {
	tcell.Screen
	x, y, w, h int
}

func (s clippedScreen) SetContent(x, y int, r rune, comb []rune, style tcell.Style) {
	if x >= s.x && y >= s.y && x < s.x+s.w && y < s.y+s.h {
		s.Screen.SetContent(x, y, r, comb, style)
	}
}
func (s clippedScreen) ShowCursor(x, y int) {
	if x >= s.x && y >= s.y && x < s.x+s.w && y < s.y+s.h {
		s.Screen.ShowCursor(x, y)
	}
}
