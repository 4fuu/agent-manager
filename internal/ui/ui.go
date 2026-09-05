package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/4fuu/agent-manager/internal/manager"
	"github.com/4fuu/agent-manager/internal/supervisor"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type UI struct {
	app               *tview.Application
	client            *supervisor.Client
	pages             *tview.Pages
	tree              *tview.TreeView
	term              *terminal
	logs              *tview.TextView
	status            *tview.TextView
	right             *tview.Pages
	state             manager.State
	selected, project string
	modal, showLogs   bool
	actions           chan func()
	done              chan struct{}
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
	u := &UI{app: tview.NewApplication(), client: c, pages: tview.NewPages(), tree: tview.NewTreeView(), term: newTerminal(), logs: tview.NewTextView(), status: tview.NewTextView(), right: tview.NewPages(), actions: make(chan func(), 256), done: make(chan struct{})}
	u.app.EnableMouse(true).EnablePaste(true)
	u.tree.SetBorder(true).SetTitle(" Projects / instances ")
	u.logs.SetBorder(true).SetTitle(" Setup logs · arrows / wheel to scroll ")
	u.logs.SetScrollable(true).SetWrap(true)
	u.status.SetText("Add a Project to begin. Tab: switch pane · Ctrl+Q: detach UI")
	u.term.send = func(in supervisor.Input) {
		in.ID = u.selected
		u.enqueue(func() {
			_, e := c.Call(supervisor.Request{Action: "input", Input: in})
			if e != nil {
				u.message(e.Error())
			}
		})
	}
	u.term.escape = func() { u.app.SetFocus(u.tree) }
	u.term.resize = func(rows, cols int) {
		if u.selected != "" && u.term.frame.Live {
			u.term.send(supervisor.Input{Rows: rows, Cols: cols})
		}
	}
	u.right.AddPage("terminal", u.term, true, true).AddPage("logs", u.logs, true, false)
	body := tview.NewFlex().AddItem(u.tree, 34, 0, true).AddItem(u.right, 0, 1, false)
	bar1, bar2 := tview.NewFlex(), tview.NewFlex()
	labels := []string{"F1 Add", "F2 Edit", "F3 New instance", "F4 Start/resume", "F5 Attach", "F6 Detach", "F7 Stop", "F8 Retry setup", "F9 Shell", "F10 Delete", "F11 Defaults", "F12 Logs"}
	for i, label := range labels {
		n := i + 1
		b := tview.NewButton(label).SetSelectedFunc(func() { u.action(n) })
		if i < 6 {
			bar1.AddItem(b, 0, 1, false)
		} else {
			bar2.AddItem(b, 0, 1, false)
		}
	}
	quit := tview.NewButton("Quit UI").SetSelectedFunc(func() { u.app.Stop() })
	footer := tview.NewFlex().AddItem(u.status, 0, 1, false).AddItem(quit, 11, 0, false)
	root := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(bar1, 1, 0, false).AddItem(bar2, 1, 0, false).AddItem(body, 0, 1, true).AddItem(footer, 1, 0, false)
	u.pages.AddPage("main", root, true, true)
	u.tree.SetChangedFunc(func(n *tview.TreeNode) {
		if ref, ok := n.GetReference().(string); ok {
			u.selectNode(ref)
		}
	}).SetSelectedFunc(func(n *tview.TreeNode) {
		if ref, ok := n.GetReference().(string); ok {
			u.selectNode(ref)
		}
	})
	u.app.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if u.term.HasFocus() && !u.modal {
			// tview reserves an unchanged Ctrl+C event for quitting the whole UI.
			if e.Key() == tcell.KeyCtrlC {
				u.term.InputHandler()(e, func(p tview.Primitive) { u.app.SetFocus(p) })
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
		if e.Key() >= tcell.KeyF1 && e.Key() <= tcell.KeyF12 {
			u.action(int(e.Key()-tcell.KeyF1) + 1)
			return nil
		}
		if e.Key() == tcell.KeyTab {
			if u.tree.HasFocus() {
				if u.showLogs {
					u.app.SetFocus(u.logs)
				} else {
					u.app.SetFocus(u.term)
				}
			} else {
				u.app.SetFocus(u.tree)
			}
			return nil
		}
		return e
	})
	u.app.SetRoot(u.pages, true).SetFocus(u.tree)
	return u
}
func (u *UI) enqueue(f func()) {
	select {
	case u.actions <- f:
	default:
		u.status.SetText("Input queue full; wait for supervisor")
	}
}
func (u *UI) message(text string) { u.app.QueueUpdateDraw(func() { u.status.SetText(text) }) }
func (u *UI) selectNode(ref string) {
	if strings.HasPrefix(ref, "p:") {
		u.project = strings.TrimPrefix(ref, "p:")
		u.selected = ""
		u.term.frame = supervisor.Frame{}
		u.logs.SetText("Select an instance or create one with F3.")
		return
	}
	if ref != u.selected {
		u.selected = ref
		u.term.frame = supervisor.Frame{}
		u.term.cols = 0
		for _, in := range u.state.Instances {
			if in.ID == ref {
				u.project = in.ProjectID
				u.showLogs = in.Status != "running" && in.Status != "shell"
				u.switchPane()
				break
			}
		}
	}
}
func (u *UI) switchPane() {
	if u.showLogs {
		u.right.SwitchToPage("logs")
	} else {
		u.right.SwitchToPage("terminal")
	}
}
func (u *UI) refresh() {
	out, e := u.client.Call(supervisor.Request{Action: "state"})
	if e != nil {
		u.message(e.Error())
		return
	}
	var selected string
	u.app.QueueUpdate(func() { selected = u.selected })
	var frame supervisor.Frame
	missing := false
	if selected != "" {
		r, e := u.client.Call(supervisor.Request{Action: "frame", ID: selected})
		if e == nil {
			frame = r.Frame
		} else if e.Error() == "instance not found" {
			missing = true
		}
	}
	u.app.QueueUpdateDraw(func() {
		if missing && selected == u.selected {
			u.selected = ""
		}
		u.state = out.State
		ref := "p:" + u.project
		if u.selected != "" {
			ref = u.selected
		}
		root := tview.NewTreeNode("GitHub Projects").SetColor(tcell.ColorAqua).SetSelectable(false)
		var current *tview.TreeNode
		for _, p := range u.state.Projects {
			pn := tview.NewTreeNode(tview.Escape(p.Name)).SetReference("p:" + p.ID).SetColor(tcell.ColorLightSkyBlue).SetSelectable(true).SetExpanded(true)
			root.AddChild(pn)
			if ref == "p:"+p.ID {
				current = pn
			}
			for _, in := range u.state.Instances {
				if in.ProjectID != p.ID {
					continue
				}
				text := fmt.Sprintf("%s [%s]", in.ID[:8], in.Status)
				n := tview.NewTreeNode(tview.Escape(text)).SetReference(in.ID).SetSelectable(true)
				if in.Status == "failed" {
					n.SetColor(tcell.ColorOrangeRed)
				} else if in.Status == "running" {
					n.SetColor(tcell.ColorGreen)
				}
				pn.AddChild(n)
				if ref == in.ID {
					current = n
				}
			}
		}
		u.tree.SetRoot(root)
		if current == nil && u.selected == "" && len(root.GetChildren()) > 0 {
			current = root.GetChildren()[0]
		}
		u.tree.SetCurrentNode(current)
		if selected == u.selected && selected != "" {
			wasLive := u.term.frame.Live
			u.term.frame = frame
			if frame.Logs != u.logs.GetText(false) {
				u.logs.SetText(frame.Logs)
				u.logs.ScrollToEnd()
			}
			if frame.Live && !wasLive {
				u.term.cols = 0
				u.showLogs = false
				u.switchPane()
			}
			for _, in := range u.state.Instances {
				if in.ID == selected && !u.modal {
					u.status.SetText(fmt.Sprintf("%s · %s · %s", in.Config.Ref, in.Status, in.Error))
					break
				}
			}
		}
	})
}
func (u *UI) selectedProject() manager.Project {
	for _, p := range u.state.Projects {
		if p.ID == u.project {
			return p
		}
	}
	return manager.Project{Image: manager.DefaultImage, Command: "uri-agent", Ref: "main"}
}
func (u *UI) action(n int) {
	if u.modal {
		return
	}
	switch n {
	case 1:
		u.projectForm(manager.Project{Image: manager.DefaultImage, Command: "uri-agent", Ref: "main"})
	case 2:
		if u.project != "" {
			u.projectForm(u.selectedProject())
		}
	case 3:
		if u.project != "" {
			u.instanceForm()
		}
	case 5:
		u.showLogs = false
		u.switchPane()
		u.app.SetFocus(u.term)
	case 6:
		u.app.SetFocus(u.tree)
	case 11:
		u.defaultsForm()
	case 12:
		u.showLogs = !u.showLogs
		u.switchPane()
		if u.showLogs {
			u.app.SetFocus(u.logs)
		}
	case 10:
		if u.selected != "" {
			id := u.selected
			u.confirm("Delete instance "+id[:8]+" and its entire guest disk? Config files on the host are retained.", func() { u.rpc(supervisor.Request{Action: "delete", ID: id}) })
		} else if u.project != "" {
			id := u.project
			u.confirm("Delete this Project? All its instances must be deleted first.", func() { u.rpc(supervisor.Request{Action: "delete-project", ID: id}) })
		}
	default:
		if u.selected != "" {
			a := map[int]string{4: "start", 7: "stop", 8: "retry", 9: "shell"}[n]
			if a != "" {
				u.rpc(supervisor.Request{Action: a, ID: u.selected})
			}
		}
	}
}
func (u *UI) rpc(r supervisor.Request) {
	u.enqueue(func() {
		_, e := u.client.Call(r)
		if e != nil {
			u.message(e.Error())
		}
	})
}
func (u *UI) popup(p tview.Primitive, width, height int) {
	u.modal = true
	center := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(nil, 0, 1, false).AddItem(tview.NewFlex().AddItem(nil, 0, 1, false).AddItem(p, width, 0, true).AddItem(nil, 0, 1, false), height, 0, true).AddItem(nil, 0, 1, false)
	u.pages.AddPage("modal", center, true, true)
	u.app.SetFocus(p)
}
func (u *UI) closeModal() { u.modal = false; u.pages.RemovePage("modal"); u.app.SetFocus(u.tree) }
func (u *UI) confirm(text string, f func()) {
	m := tview.NewModal().SetText(text).AddButtons([]string{"Cancel", "Delete"}).SetDoneFunc(func(i int, _ string) {
		u.closeModal()
		if i == 1 {
			f()
		}
	})
	u.popup(m, 72, 9)
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
			return nil, fmt.Errorf("mapping format: /host/file | /guest/file | ro or rw")
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
func (u *UI) projectForm(p manager.Project) {
	f := tview.NewForm()
	f.SetBorder(true).SetTitle(" Project · Tab next · Esc cancel ")
	f.AddInputField("Name", p.Name, 52, nil, func(v string) { p.Name = v })
	f.AddInputField("GitHub URL", p.URL, 52, nil, func(v string) { p.URL = v })
	f.AddInputField("OCI image", p.Image, 52, nil, func(v string) { p.Image = v })
	f.AddInputField("Default ref", p.Ref, 52, nil, func(v string) { p.Ref = v })
	f.AddInputField("Guest command", p.Command, 52, nil, func(v string) { p.Command = v })
	f.AddInputField("GitHub token file", p.AuthFile, 52, nil, func(v string) { p.AuthFile = v })
	mapping := mappingText(p.Mappings)
	f.AddTextView("Mappings format", "/host/file | /guest/file | ro (or explicit rw)\nOne per line. Guest path overrides global default.", 58, 2, false, false)
	f.AddTextArea("Config mappings", mapping, 58, 4, 0, func(v string) { mapping = v })
	f.AddButton("Save", func() {
		ms, e := parseMappings(mapping)
		if e != nil {
			u.status.SetText(e.Error())
			return
		}
		p.Mappings = ms
		u.enqueue(func() {
			_, e := u.client.Call(supervisor.Request{Action: "project", Project: p})
			u.app.QueueUpdateDraw(func() {
				if e != nil {
					u.status.SetText(e.Error())
				} else {
					u.closeModal()
				}
			})
		})
	}).AddButton("Cancel", u.closeModal)
	u.popup(f, 82, 29)
}
func (u *UI) defaultsForm() {
	text := mappingText(u.state.Defaults)
	f := tview.NewForm()
	f.SetBorder(true).SetTitle(" Global config mappings · applied to NEW instances ")
	f.AddTextView("Format", "/host/file | /guest/file | ro (or explicit rw)\nOne per line; only individual regular files.", 62, 2, false, false)
	f.AddTextArea("Mappings", text, 62, 8, 0, func(v string) { text = v })
	f.AddButton("Save", func() {
		ms, e := parseMappings(text)
		if e != nil {
			u.status.SetText(e.Error())
			return
		}
		u.enqueue(func() {
			_, e := u.client.Call(supervisor.Request{Action: "defaults", Mappings: ms})
			u.app.QueueUpdateDraw(func() {
				if e != nil {
					u.status.SetText(e.Error())
				} else {
					u.closeModal()
				}
			})
		})
	}).AddButton("Cancel", u.closeModal)
	u.popup(f, 82, 20)
}
func (u *UI) instanceForm() {
	p := u.selectedProject()
	ref, image := p.Ref, p.Image
	f := tview.NewForm()
	f.SetBorder(true).SetTitle(" New isolated instance · " + tview.Escape(p.Name) + " ")
	f.AddInputField("Branch / ref", ref, 54, nil, func(v string) { ref = v }).AddInputField("OCI image", image, 54, nil, func(v string) { image = v })
	f.AddTextView("Lifecycle", "Boot → clone → .agents/setup → URI Agent PTY\nEach instance has its own persistent guest disk.", 60, 2, false, false)
	f.AddButton("Create & start", func() {
		u.enqueue(func() {
			r, e := u.client.Call(supervisor.Request{Action: "create", ID: p.ID, Ref: ref, Image: image})
			if e == nil {
				_, e = u.client.Call(supervisor.Request{Action: "start", ID: r.ID})
			}
			u.app.QueueUpdateDraw(func() {
				if e != nil {
					u.status.SetText(e.Error())
				} else {
					u.selected = r.ID
					u.closeModal()
					u.showLogs = true
					u.switchPane()
				}
			})
		})
	}).AddButton("Cancel", u.closeModal)
	u.popup(f, 82, 15)
}
