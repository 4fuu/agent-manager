package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/4fuu/agent-manager/internal/manager"
	"github.com/4fuu/agent-manager/internal/supervisor"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var accent = tcell.NewHexColor(0x85c7c1)
var muted = tcell.NewHexColor(0x82909e)

type UI struct {
	app                                    *tview.Application
	client                                 *supervisor.Client
	pages                                  *tview.Pages
	tree                                   *tview.TreeView
	term                                   *terminal
	strip                                  *workspace
	workspaces                             map[string]*workspace
	logs, status, heading                  *tview.TextView
	right                                  *tview.Pages
	body                                   *tview.Flex
	state                                  manager.State
	selected, project                      string
	modal, showLogs, hideSidebar, dragging bool
	sidebarWidth                           int
	modalView                              tview.Primitive
	modalWidth, modalHeight                int
	refreshMu                              sync.Mutex
	actions                                chan func()
	done                                   chan struct{}
}

func Run(c *supervisor.Client) error {
	if _, e := c.Call(supervisor.Request{Action: "state"}); e != nil {
		return e
	}
	u := New(c)
	go func() {
		for {
			select {
			case <-u.done:
				return
			case f := <-u.actions:
				f()
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-u.done:
				return
			case <-ticker.C:
				u.refresh()
			}
		}
	}()
	defer close(u.done)
	return u.app.Run()
}

func New(c *supervisor.Client) *UI {
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = tcell.NewHexColor(0x263541)
	tview.Styles.MoreContrastBackgroundColor = tcell.NewHexColor(0x38535b)
	tview.Styles.BorderColor = muted
	tview.Styles.TitleColor = accent
	tview.Styles.PrimaryTextColor = tcell.ColorDefault
	u := &UI{app: tview.NewApplication(), client: c, pages: tview.NewPages(), tree: tview.NewTreeView(), term: newTerminal(), logs: tview.NewTextView(), status: tview.NewTextView(), heading: tview.NewTextView(), right: tview.NewPages(), workspaces: map[string]*workspace{}, sidebarWidth: 28, actions: make(chan func(), 256), done: make(chan struct{})}
	u.app.EnableMouse(true).EnablePaste(true)
	u.tree.SetBorderPadding(1, 0, 1, 1)
	u.tree.SetGraphics(false).SetTopLevel(1)
	u.logs.SetBorderPadding(1, 1, 2, 2).SetTitle(" SETUP LOGS ").SetBorder(true)
	u.logs.SetScrollable(true).SetWrap(true)
	u.heading.SetDynamicColors(true)
	u.status.SetTextColor(muted)
	u.strip = newWorkspace()
	u.right.AddPage("workspace", u.strip, true, true).AddPage("logs", u.logs, true, false)
	u.body = tview.NewFlex().AddItem(u.tree, u.sidebarWidth, 0, true).AddItem(u.right, 0, 1, false)
	button := func(label string, f func()) *tview.Button { return tview.NewButton(label).SetSelectedFunc(f) }
	header := tview.NewFlex().AddItem(u.heading, 0, 1, false).AddItem(button("+ Session", u.instanceForm), 12, 0, false).AddItem(button("Menu", u.menu), 8, 0, false)
	footer := tview.NewFlex().AddItem(u.status, 0, 1, false).AddItem(button("Quit", u.app.Stop), 6, 0, false)
	root := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(header, 1, 0, false).AddItem(u.body, 0, 1, true).AddItem(footer, 1, 0, false)
	u.pages.AddPage("main", root, true, true)
	u.tree.SetChangedFunc(func(n *tview.TreeNode) {
		if ref, ok := n.GetReference().(string); ok {
			u.selectNode(ref)
		}
	}).SetSelectedFunc(func(n *tview.TreeNode) {
		if ref, ok := n.GetReference().(string); ok {
			u.selectNode(ref)
			if u.selected != "" {
				u.focusTerminal()
			} else {
				n.SetExpanded(!n.IsExpanded())
			}
		}
	})
	u.term.escape = func() { u.app.SetFocus(u.tree) }
	u.app.SetBeforeDrawFunc(func(s tcell.Screen) bool {
		w, h := s.Size()
		side := u.sidebarWidth
		if u.hideSidebar || w < 72 {
			side = 0
		}
		u.body.ResizeItem(u.tree, side, 0)
		// Flex size zero means flexible. Hide the rail altogether instead.
		if side == 0 {
			u.body.RemoveItem(u.tree)
		} else if u.body.GetItemCount() == 1 {
			u.body.Clear().AddItem(u.tree, side, 0, false).AddItem(u.right, 0, 1, false)
		}
		if u.modalView != nil {
			mw, mh := min(u.modalWidth, w), min(u.modalHeight, h-2)
			u.modalView.SetRect((w-mw)/2, (h-mh)/2, mw, mh)
		}
		if u.selected == "" {
			u.heading.SetText(" [::b]AGENT MANAGER[::-]  [#82909e]Projects / Sessions")
		}
		if !u.modal {
			u.hints()
		}
		return false
	})
	u.app.SetMouseCapture(func(e *tcell.EventMouse, a tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		x, y := e.Position()
		if u.modal && u.modalView != nil {
			mx, my, mw, mh := u.modalView.GetRect()
			if x < mx || y < my || x >= mx+mw || y >= my+mh {
				return nil, a
			}
		}
		if !u.modal && !u.hideSidebar && u.body.GetItemCount() > 1 {
			if a == tview.MouseLeftDown && x == u.sidebarWidth-1 {
				u.dragging = true
			}
			if u.dragging {
				if a == tview.MouseMove {
					u.sidebarWidth = max(20, min(x+1, 50))
				}
				if a == tview.MouseLeftUp {
					u.dragging = false
				}
				return nil, a
			}
		}
		return e, a
	})
	u.app.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if !u.modal && u.focusedTerminal() != nil {
			if e.Key() == tcell.KeyCtrlC {
				u.focusedTerminal().InputHandler()(e, func(p tview.Primitive) { u.app.SetFocus(p) })
				return nil
			}
			return e
		}
		if e.Key() == tcell.KeyCtrlQ {
			u.app.Stop()
			return nil
		}
		if u.modal {
			if e.Key() == tcell.KeyEscape {
				u.closeModal()
				return nil
			}
			return e
		}
		if e.Key() == tcell.KeyTab {
			u.focusTerminal()
			return nil
		}
		if e.Key() == tcell.KeyLeft {
			u.moveColumn(-1)
			return nil
		}
		if e.Key() == tcell.KeyRight {
			u.moveColumn(1)
			return nil
		}
		if e.Key() == tcell.KeyRune {
			switch e.Rune() {
			case ' ':
				u.menu()
			case '?':
				u.menu()
			case 'p':
				u.projectForm(manager.Project{})
			case 'e':
				if u.project != "" {
					u.projectForm(u.selectedProject())
				}
			case 'n':
				u.instanceForm()
			case 'w':
				u.sessionsMenu()
			case 'i':
				u.imagesMenu()
			case 'g':
				u.defaultsForm()
			case 'r':
				u.renameForm()
			case 't':
				u.addPane("/bin/bash -l")
			case 'y':
				u.addPane("yazi")
			case '+', '=':
				u.resizeColumn(10)
			case '-':
				u.resizeColumn(-10)
			case 'x':
				u.closePane()
			case 's':
				u.sessionAction("start")
			case 'o':
				u.sessionAction("stop")
			case 'l':
				u.showLogs = !u.showLogs
				u.switchPane()
			case 'b':
				u.hideSidebar = !u.hideSidebar
			default:
				return e
			}
			return nil
		}
		return e
	})
	u.app.SetRoot(u.pages, true).SetFocus(u.tree)
	return u
}

func (u *UI) focusedTerminal() *terminal {
	if u.term.HasFocus() {
		return u.term
	}
	for _, t := range u.strip.terms {
		if t.HasFocus() {
			return t
		}
	}
	return nil
}
func (u *UI) hints() {
	text := " p Project   n Session   i Images   Space Menu   Tab Terminal"
	if u.selected != "" {
		text = " t Shell   y Files   ←/→ Column   +/− Width   r Rename   Space Menu"
	}
	if u.focusedTerminal() != nil {
		text = " TERMINAL  ·  typing → guest   Ctrl+] navigation   tmux: Ctrl+B [ scroll/copy"
	}
	if u.showLogs {
		text = " SETUP LOGS  ·  arrows / wheel scroll   l Workspace   Space Recovery actions"
	}
	u.status.SetText(text)
}
func (u *UI) enqueue(f func()) {
	select {
	case u.actions <- f:
	default:
		u.status.SetText("Input queue full; wait for supervisor")
	}
}
func (u *UI) message(text string) { u.app.QueueUpdateDraw(func() { u.notice(text) }) }
func (u *UI) notice(text string) {
	m := tview.NewModal().SetText(text).AddButtons([]string{"OK"}).SetDoneFunc(func(int, string) { u.closeModal() })
	u.popup(m, 72, 12)
}
func (u *UI) rpc(r supervisor.Request) {
	u.enqueue(func() {
		_, e := u.client.Call(r)
		if e != nil {
			u.message(e.Error())
		}
	})
}
func sessionName(in manager.Instance) string {
	if in.Name != "" {
		return in.Name
	}
	if in.Title != "" {
		return in.Title
	}
	return "Session " + in.ID[:min(6, len(in.ID))]
}
func (u *UI) selectedProject() manager.Project {
	for _, p := range u.state.Projects {
		if p.ID == u.project {
			return p
		}
	}
	return manager.Project{}
}
func (u *UI) selectedInstance() manager.Instance {
	for _, in := range u.state.Instances {
		if in.ID == u.selected {
			return in
		}
	}
	return manager.Instance{}
}
func (u *UI) selectNode(ref string) {
	if strings.HasPrefix(ref, "p:") {
		u.project = strings.TrimPrefix(ref, "p:")
		u.selected = ""
		u.strip = newWorkspace()
		u.right.AddPage("workspace", u.strip, true, true)
		u.showLogs = false
		u.switchPane()
		return
	}
	if ref == u.selected {
		return
	}
	u.selected = ref
	for _, in := range u.state.Instances {
		if in.ID == ref {
			u.project = in.ProjectID
			u.activate(in)
			break
		}
	}
}
func (u *UI) activate(in manager.Instance) {
	w := u.workspaces[in.ID]
	if w == nil {
		w = newWorkspace()
		u.workspaces[in.ID] = w
	}
	u.strip = w
	u.syncWorkspace(in)
	u.right.AddPage("workspace", w, true, true)
	u.showLogs = in.Status == "setup" || in.Status == "working" || in.Status == "failed"
	u.switchPane()
}
func (u *UI) syncWorkspace(in manager.Instance) {
	w := u.strip
	wasFocused := u.focusedTerminal() != nil
	old := map[string]*terminal{}
	for _, t := range w.terms {
		old[t.id] = t
	}
	active := ""
	if len(w.terms) > 0 {
		active = w.terms[min(w.active, len(w.terms)-1)].id
	}
	w.terms = nil
	for _, p := range in.Panes {
		t := old[p.ID]
		if t == nil {
			t = newTerminal()
			t.id = p.ID
			session, pane := in.ID, p.ID
			t.send = func(input supervisor.Input) {
				input.ID = session
				input.PaneID = pane
				u.enqueue(func() {
					_, e := u.client.Call(supervisor.Request{Action: "input", Input: input})
					if e != nil {
						u.message(e.Error())
					}
				})
			}
			t.escape = func() { u.app.SetFocus(u.tree) }
			t.resize = func(rows, cols int) {
				if t.frame.Live {
					t.send(supervisor.Input{Rows: rows, Cols: cols})
				}
			}
		}
		if p.Width == t.pendingWidth {
			t.pendingWidth = 0
		}
		if t.pendingWidth == 0 {
			t.preferred = p.Width
		}
		t.label = p.Command
		w.terms = append(w.terms, t)
		if p.ID == active {
			w.active = len(w.terms) - 1
		}
	}
	w.active = max(0, min(w.active, len(w.terms)-1))
	w.first = min(w.first, w.active)
	w.onFocus = func(index int) { w.active = index; u.term = w.terms[index]; u.app.SetFocus(u.term) }
	if len(w.terms) > 0 {
		u.term = w.terms[w.active]
		if wasFocused && !u.modal {
			u.app.SetFocus(u.term)
		}
	} else if wasFocused {
		u.app.SetFocus(u.tree)
	}
}
func (u *UI) switchPane() {
	if u.showLogs {
		u.right.SwitchToPage("logs")
		if !u.modal {
			u.app.SetFocus(u.logs)
		}
	} else {
		u.right.SwitchToPage("workspace")
		if u.logs.HasFocus() && !u.modal {
			u.app.SetFocus(u.tree)
		}
	}
}
func (u *UI) focusTerminal() {
	if len(u.strip.terms) == 0 {
		return
	}
	u.showLogs = false
	u.switchPane()
	u.term = u.strip.terms[u.strip.active]
	u.app.SetFocus(u.term)
}
func (u *UI) moveColumn(delta int) {
	w := u.strip
	if len(w.terms) == 0 {
		return
	}
	w.active = max(0, min(w.active+delta, len(w.terms)-1))
	u.term = w.terms[w.active]
}
func (u *UI) resizeColumn(delta int) {
	w := u.strip
	if len(w.terms) == 0 {
		return
	}
	t := w.terms[w.active]
	t.preferred = max(30, min(240, t.preferred+delta))
	t.pendingWidth = t.preferred
	r := supervisor.Request{Action: "width", ID: u.selected, PaneID: t.id, Width: t.preferred}
	u.enqueue(func() {
		_, err := u.client.Call(r)
		if err != nil {
			u.app.QueueUpdateDraw(func() {
				if t.pendingWidth == r.Width {
					t.pendingWidth = 0
				}
				u.notice(err.Error())
			})
		}
	})
}
func (u *UI) sessionAction(action string) {
	if u.selected != "" {
		u.rpc(supervisor.Request{Action: action, ID: u.selected})
	}
}
func (u *UI) addPane(command string) {
	if u.selected == "" {
		return
	}
	id := u.selected
	u.enqueue(func() {
		r, e := u.client.Call(supervisor.Request{Action: "add-pane", ID: id, Command: command})
		if e != nil {
			u.message(e.Error())
			return
		}
		u.refresh()
		u.app.QueueUpdateDraw(func() {
			if u.selected == id {
				for i, t := range u.strip.terms {
					if t.id == r.ID {
						u.strip.active = i
						u.focusTerminal()
					}
				}
			}
		})
	})
}
func (u *UI) closePane() {
	if len(u.strip.terms) == 0 {
		return
	}
	id, pane := u.selected, u.strip.terms[u.strip.active].id
	u.confirm("Close this terminal and its guest program? Other terminals and the session disk remain.", "Close terminal", func() { u.rpc(supervisor.Request{Action: "close-pane", ID: id, PaneID: pane}) })
}

func (u *UI) refresh() {
	u.refreshMu.Lock()
	defer u.refreshMu.Unlock()
	var selected string
	visible := map[string]bool{}
	u.app.QueueUpdate(func() {
		selected = u.selected
		for _, i := range u.strip.visible {
			if i < len(u.strip.terms) {
				visible[u.strip.terms[i].id] = true
			}
		}
		if len(u.strip.terms) > 0 {
			visible[u.strip.terms[u.strip.active].id] = true
		}
	})
	out, e := u.client.Call(supervisor.Request{Action: "state"})
	if e != nil {
		u.message(e.Error())
		return
	}
	frames := map[string]supervisor.Frame{}
	logs := ""
	for _, in := range out.State.Instances {
		if in.ID == selected {
			if len(in.Panes) == 0 {
				r, e := u.client.Call(supervisor.Request{Action: "frame", ID: selected})
				if e == nil {
					logs = r.Frame.Logs
				}
			}
			for index, p := range in.Panes {
				if index != 0 && !visible[p.ID] && !(in.Status == "shell" && p.ID == "recovery") {
					continue
				}
				r, e := u.client.Call(supervisor.Request{Action: "frame", ID: selected, PaneID: p.ID})
				if e == nil {
					frames[p.ID] = r.Frame
					logs = r.Frame.Logs
				}
			}
		}
	}
	u.app.QueueUpdateDraw(func() {
		if selected != u.selected {
			return
		}
		previousStatus := u.selectedInstance().Status
		u.state = out.State
		present := map[string]bool{}
		for _, session := range u.state.Instances {
			present[session.ID] = true
		}
		for id := range u.workspaces {
			if !present[id] {
				delete(u.workspaces, id)
			}
		}
		in := u.selectedInstance()
		if u.selected != "" && in.ID == "" {
			u.selected = ""
			u.strip = newWorkspace()
			u.right.AddPage("workspace", u.strip, true, true)
			if !u.modal {
				u.app.SetFocus(u.tree)
			}
		}
		ref := "p:" + u.project
		if u.selected != "" {
			ref = u.selected
		}
		collapsed := map[string]bool{}
		if root := u.tree.GetRoot(); root != nil {
			for _, n := range root.GetChildren() {
				if r, ok := n.GetReference().(string); ok {
					collapsed[r] = !n.IsExpanded()
				}
			}
		}
		root := tview.NewTreeNode("")
		var current *tview.TreeNode
		for _, p := range u.state.Projects {
			pn := tview.NewTreeNode("▾ " + tview.Escape(p.Name)).SetReference("p:" + p.ID).SetColor(accent).SetSelectable(true).SetExpanded(!collapsed["p:"+p.ID])
			pn.SetSelectedTextStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(accent).Bold(true))
			root.AddChild(pn)
			if !pn.IsExpanded() {
				pn.SetText("▸ " + tview.Escape(p.Name))
			}
			if ref == "p:"+p.ID {
				current = pn
			}
			for _, in := range u.state.Instances {
				if in.ProjectID != p.ID {
					continue
				}
				glyph, color := "○", muted
				switch in.Status {
				case "running", "shell":
					glyph, color = "●", accent
				case "working", "setup":
					glyph, color = "◐", tcell.ColorYellow
				case "failed", "missing", "unavailable":
					glyph, color = "×", tcell.ColorIndianRed
				}
				n := tview.NewTreeNode("  " + glyph + " " + tview.Escape(sessionName(in))).SetReference(in.ID).SetColor(color).SetSelectable(true)
				n.SetSelectedTextStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(accent))
				pn.AddChild(n)
				if ref == in.ID {
					current = n
					pn.SetExpanded(true)
				}
			}
		}
		u.tree.SetRoot(root)
		if current == nil && len(root.GetChildren()) > 0 {
			current = root.GetChildren()[0]
		}
		u.tree.SetCurrentNode(current)
		if selected == u.selected && selected != "" {
			in = u.selectedInstance()
			u.syncWorkspace(in)
			if in.Status == "shell" && previousStatus != "shell" {
				for index, p := range in.Panes {
					if p.ID == "recovery" {
						u.strip.active = index
						u.term = u.strip.terms[index]
					}
				}
				u.showLogs = false
				u.switchPane()
			}
			for _, t := range u.strip.terms {
				frame, ok := frames[t.id]
				if !ok {
					continue
				}
				wasLive := t.frame.Live
				t.frame = frame
				if t.frame.Live && !wasLive {
					t.cols = 0
					u.showLogs = false
					u.switchPane()
				}
			}
			if logs != u.logs.GetText(false) {
				u.logs.SetText(logs)
				u.logs.ScrollToEnd()
			}
			u.heading.SetText(" [::b]" + tview.Escape(u.selectedProject().Name) + "[::-]  [#82909e]/[-]  " + tview.Escape(sessionName(in)) + "  [#85c7c1]" + in.Status)
		}
	})
}

func (u *UI) popup(p tview.Primitive, width, height int) {
	u.modal = true
	u.modalView = p
	u.modalWidth = width
	u.modalHeight = height
	u.pages.AddPage("modal", p, false, true)
	u.app.SetFocus(p)
}
func (u *UI) closeModal() {
	u.modal = false
	u.modalView = nil
	u.pages.RemovePage("modal")
	u.app.SetFocus(u.tree)
}
func (u *UI) confirm(text, label string, f func()) {
	m := tview.NewModal().SetText(text).AddButtons([]string{"Cancel", label}).SetDoneFunc(func(i int, _ string) {
		u.closeModal()
		if i == 1 {
			f()
		}
	})
	u.popup(m, 72, 10)
}
func (u *UI) menu() {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetBorder(true).SetTitle(" ACTIONS · Esc back ")
	add := func(name string, key rune, f func()) { list.AddItem(name, "", key, func() { u.closeModal(); f() }) }
	add("New project", 'p', func() { u.projectForm(manager.Project{}) })
	add("New session", 'n', u.instanceForm)
	add("Switch project / session", 'w', u.sessionsMenu)
	add("Images & config mappings", 'i', u.imagesMenu)
	add("Shared config mappings", 'g', u.defaultsForm)
	if u.project != "" {
		add("Edit project", 'e', func() { u.projectForm(u.selectedProject()) })
	}
	if u.selected != "" {
		add("Focus terminal", 'f', u.focusTerminal)
		add("Add shell column", 't', func() { u.addPane("/bin/bash -l") })
		add("Open Yazi file manager", 'y', func() { u.addPane("yazi") })
		add("Previous column", '[', func() { u.moveColumn(-1) })
		add("Next column", ']', func() { u.moveColumn(1) })
		add("Wider column", '+', func() { u.resizeColumn(10) })
		add("Narrower column", '-', func() { u.resizeColumn(-10) })
		add("Close terminal column", 'x', u.closePane)
		add("Rename session", 'r', u.renameForm)
		add("Start / reconnect", 's', func() { u.sessionAction("start") })
		add("Stop session (retain disk)", 'o', func() { u.sessionAction("stop") })
		add("Setup logs / workspace", 'l', func() { u.showLogs = !u.showLogs; u.switchPane() })
		add("Retry setup", 'a', func() { u.sessionAction("retry") })
		add("Recovery shell", 'h', func() { u.sessionAction("shell") })
		add("Delete session & disk", 'd', func() {
			id := u.selected
			u.confirm("Permanently delete this session and its entire guest disk? Host config files are retained.", "Delete disk", func() { u.rpc(supervisor.Request{Action: "delete", ID: id}) })
		})
	} else if u.project != "" {
		add("Delete project", 'd', func() {
			id := u.project
			u.confirm("Delete project? Its sessions must be deleted first.", "Delete", func() { u.rpc(supervisor.Request{Action: "delete-project", ID: id}) })
		})
	}
	add("Toggle sidebar", 'b', func() { u.hideSidebar = !u.hideSidebar })
	u.popup(list, 56, min(28, list.GetItemCount()+2))
}
func (u *UI) sessionsMenu() {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetBorder(true).SetTitle(" PROJECTS / SESSIONS ")
	for _, p := range u.state.Projects {
		project := p.ID
		list.AddItem(p.Name, "", 0, func() { u.closeModal(); u.selectNode("p:" + project) })
		for _, in := range u.state.Instances {
			if in.ProjectID == project {
				session := in.ID
				list.AddItem("  "+sessionName(in), "", 0, func() { u.closeModal(); u.selectNode(session) })
			}
		}
	}
	u.popup(list, 64, min(24, list.GetItemCount()+2))
}
func newForm(title string) *tview.Form {
	f := tview.NewForm()
	f.SetBorder(true).SetTitle(" " + title + " · Esc cancel ")
	f.SetItemPadding(1)
	return f
}
func (u *UI) saveForm(r supervisor.Request) {
	u.enqueue(func() {
		_, e := u.client.Call(r)
		u.app.QueueUpdateDraw(func() {
			if e != nil {
				u.status.SetText("Cannot save: " + e.Error())
			} else {
				u.closeModal()
			}
		})
	})
}
func (u *UI) projectForm(p manager.Project) {
	f := newForm("PROJECT")
	f.AddInputField("Name", p.Name, 0, nil, func(v string) { p.Name = v }).AddInputField("GitHub URL", p.URL, 0, nil, func(v string) { p.URL = v }).AddInputField("Token file (optional)", p.AuthFile, 0, nil, func(v string) { p.AuthFile = v })
	f.AddTextView("Sessions", "Each session clones the repository's default branch, runs .agents/setup, then starts your chosen Agent.", 0, 3, false, false)
	f.AddButton("Save project", func() { u.saveForm(supervisor.Request{Action: "project", Project: p}) }).AddButton("Cancel", u.closeModal)
	u.popup(f, 78, 17)
}
func (u *UI) instanceForm() {
	if len(u.state.Projects) == 0 {
		u.projectForm(manager.Project{})
		return
	}
	if len(u.state.Images) == 0 {
		u.imagesMenu()
		return
	}
	projectIndex := 0
	projects, profiles := append([]manager.Project(nil), u.state.Projects...), append([]manager.ImageProfile(nil), u.state.Images...)
	names := []string{}
	for i, p := range projects {
		names = append(names, p.Name)
		if p.ID == u.project {
			projectIndex = i
		}
	}
	images := []string{}
	for _, p := range profiles {
		images = append(images, p.Name)
	}
	imageIndex := 0
	f := newForm("NEW SESSION")
	f.AddDropDown("Project", names, projectIndex, func(_ string, i int) { projectIndex = i }).AddDropDown("Image", images, 0, func(_ string, i int) { imageIndex = i })
	f.AddTextView("Automatic", "Default branch → .agents/setup → Agent\nIndependent guest disk. Image + shared mappings are applied automatically.", 0, 3, false, false)
	f.AddButton("Create & start", func() {
		project, image := projects[projectIndex].ID, profiles[imageIndex].ID
		u.enqueue(func() {
			r, e := u.client.Call(supervisor.Request{Action: "create", ID: project, Image: image})
			if e == nil {
				_, e = u.client.Call(supervisor.Request{Action: "start", ID: r.ID})
			}
			u.app.QueueUpdateDraw(func() {
				if e != nil {
					u.notice(e.Error())
				} else {
					u.closeModal()
					u.selected = r.ID
					u.project = project
					u.strip = newWorkspace()
					u.workspaces[r.ID] = u.strip
					u.right.AddPage("workspace", u.strip, true, true)
					u.showLogs = true
					u.switchPane()
				}
			})
		})
	}).AddButton("Cancel", u.closeModal)
	u.popup(f, 74, 15)
}
func (u *UI) renameForm() {
	if u.selected == "" {
		return
	}
	id, name := u.selected, sessionName(u.selectedInstance())
	f := newForm("RENAME SESSION")
	f.AddInputField("Fixed name", name, 0, nil, func(v string) { name = v })
	f.AddTextView("Title", "A custom name stays fixed. Otherwise only the leftmost terminal's OSC title names this session.", 0, 3, false, false)
	f.AddButton("Rename", func() { u.saveForm(supervisor.Request{Action: "rename", ID: id, Name: name}) }).AddButton("Cancel", u.closeModal)
	u.popup(f, 72, 13)
}
func mappingText(ms []manager.Mapping) string {
	var lines []string
	for _, m := range ms {
		mode := "ro"
		if m.Writable {
			mode = "rw"
		}
		lines = append(lines, m.Host+" | "+m.Guest+" | "+mode)
	}
	return strings.Join(lines, "\n")
}
func parseMappings(text string) ([]manager.Mapping, error) {
	var out []manager.Mapping
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		p := strings.Split(line, "|")
		if len(p) < 2 || len(p) > 3 {
			return nil, fmt.Errorf("mapping format: absolute host path | /guest/path | ro or rw")
		}
		m := manager.Mapping{Host: strings.TrimSpace(p[0]), Guest: strings.TrimSpace(p[1])}
		if len(p) == 3 {
			mode := strings.TrimSpace(p[2])
			if mode != "ro" && mode != "rw" {
				return nil, fmt.Errorf("mapping mode must be ro or explicit rw")
			}
			m.Writable = mode == "rw"
		}
		out = append(out, m)
	}
	return out, nil
}

const mappingWarning = "File or directory: absolute host path | /guest/path | ro (or rw). Credentials and config hooks are available to setup and Agents. rw allows host changes. Shared mappings apply automatically; this profile overrides matching guest paths."

func (u *UI) defaultsForm() {
	text := mappingText(u.state.Defaults)
	f := newForm("SHARED CONFIG · new sessions only")
	f.AddTextView("Access", mappingWarning, 0, 4, false, false).AddTextArea("Mappings", text, 0, 7, 0, func(v string) { text = v })
	f.AddButton("Save", func() {
		ms, e := parseMappings(text)
		if e != nil {
			u.status.SetText(e.Error())
			return
		}
		u.saveForm(supervisor.Request{Action: "defaults", Mappings: ms})
	}).AddButton("Cancel", u.closeModal)
	u.popup(f, 86, 20)
}
func (u *UI) imagesMenu() {
	list := tview.NewList()
	list.SetBorder(true).SetTitle(" IMAGES · configured before session creation ")
	for _, p := range u.state.Images {
		profile := p
		source := p.Image
		if p.Archive != "" {
			source = p.Archive
		}
		list.AddItem(p.Name, source, 0, func() { u.closeModal(); u.imageForm(profile) })
	}
	list.AddItem("+ Custom image", "OCI reference or local archive", 'n', func() { u.closeModal(); u.imageForm(manager.ImageProfile{}) })
	u.popup(list, 84, min(26, 2*list.GetItemCount()+2))
}
func (u *UI) imageForm(p manager.ImageProfile) {
	f := newForm("IMAGE PROFILE")
	mappings := mappingText(p.Mappings)
	env, _ := json.Marshal(p.Environment)
	envText := string(env)
	if envText == "null" {
		envText = "{}"
	}
	f.AddInputField("Name", p.Name, 0, nil, func(v string) { p.Name = v }).AddInputField("OCI reference", p.Image, 0, nil, func(v string) { p.Image = v }).AddInputField("OR archive path", p.Archive, 0, nil, func(v string) { p.Archive = v }).AddInputField("Agent command", p.Command, 0, nil, func(v string) { p.Command = v })
	f.AddInputField("Environment JSON", envText, 0, nil, func(v string) { envText = v }).AddTextView("Access", mappingWarning, 0, 4, false, false).AddTextArea("Mappings", mappings, 0, 5, 0, func(v string) { mappings = v })
	f.AddButton("Save", func() {
		ms, e := parseMappings(mappings)
		if e != nil {
			u.status.SetText(e.Error())
			return
		}
		p.Mappings = ms
		if e = json.Unmarshal([]byte(envText), &p.Environment); e != nil {
			u.status.SetText("Environment must be a JSON object of string values.")
			return
		}
		u.saveForm(supervisor.Request{Action: "image", Profile: p})
	}).AddButton("Cancel", u.closeModal)
	if p.ID != "" {
		f.AddButton("Delete profile", func() {
			u.confirm("Delete this image profile? Existing sessions retain their configuration.", "Delete", func() { u.saveForm(supervisor.Request{Action: "delete-image", ID: p.ID}) })
		})
	}
	u.popup(f, 90, 29)
}
