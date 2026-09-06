package ui

import (
	"fmt"
	"strings"

	"github.com/4fuu/agent-manager/internal/manager"
	"github.com/4fuu/agent-manager/internal/supervisor"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type imageManager struct {
	u          *UI
	root, body *tview.Flex
	list       *tview.List
	details    *tview.TextView
	buttons    []*tview.Button
	ids        []string
	selected   string
}

func (u *UI) imagesMenu() {
	if u.images != nil {
		return
	}
	m := &imageManager{u: u, list: tview.NewList(), details: tview.NewTextView().SetWrap(true)}
	u.images = m
	m.list.SetBorder(true).SetTitle(" PROFILES ")
	m.list.SetBorderPadding(1, 1, 1, 1)
	m.list.SetMainTextColor(tcell.ColorDefault).SetSecondaryTextColor(muted).SetSelectedTextColor(tcell.ColorBlack).SetSelectedBackgroundColor(accent)
	m.details.SetBorder(true).SetTitle(" IMAGE DETAILS ").SetBorderPadding(1, 1, 2, 2)
	m.list.SetChangedFunc(func(index int, _, _ string, _ rune) {
		if index >= 0 && index < len(m.ids) {
			m.selected = m.ids[index]
			m.showDetails()
		}
	}).SetSelectedFunc(func(int, string, string, rune) { m.edit() })
	header := tview.NewTextView().SetText("  IMAGES\n  Configure once. Download before creating a session.").SetTextColor(accent)
	m.body = tview.NewFlex().AddItem(m.list, 38, 0, true).AddItem(m.details, 0, 1, false)
	buttons := tview.NewFlex()
	for _, action := range []struct {
		label string
		run   func()
	}{
		{"New", func() { u.imageForm(manager.ImageProfile{Command: "/bin/bash -l"}) }},
		{"Edit", m.edit}, {"Download", func() { m.action("download-image") }},
		{"Cancel", func() { m.action("cancel-image-download") }}, {"Delete", m.delete}, {"Back", m.close},
	} {
		button := tview.NewButton(action.label).SetSelectedFunc(action.run)
		m.buttons = append(m.buttons, button)
		buttons.AddItem(button, 0, 1, false)
	}
	m.root = tview.NewFlex().SetDirection(tview.FlexRow).AddItem(header, 3, 0, false).AddItem(m.body, 0, 1, true).AddItem(buttons, 3, 0, false).AddItem(u.status, 1, 0, false)
	m.update()
	u.status.SetText("n New  e Edit  d Download/retry  c Cancel  Tab Focus  Esc Back")
	u.pages.AddPage("images", m.root, true, true).SwitchToPage("images")
	u.app.SetFocus(m.list)
}

func (m *imageManager) layout(width int) {
	if width < 90 {
		m.body.SetDirection(tview.FlexRow).ResizeItem(m.list, 9, 0)
	} else {
		m.body.SetDirection(tview.FlexColumn).ResizeItem(m.list, 38, 0)
	}
}

func (m *imageManager) profile() manager.ImageProfile {
	for _, p := range m.u.state.Images {
		if p.ID == m.selected {
			return p
		}
	}
	return manager.ImageProfile{}
}

func imageStatus(p manager.ImageProfile) string {
	if p.Download.Status == "" {
		return "Not downloaded here"
	}
	if p.Download.Status == "ready" {
		return "Downloaded"
	}
	return strings.ToUpper(p.Download.Status[:1]) + p.Download.Status[1:]
}

func (m *imageManager) update() {
	images := m.u.state.Images
	selected := m.selected
	same := len(images) == len(m.ids)
	for i, p := range images {
		if i >= len(m.ids) || m.ids[i] != p.ID {
			same = false
			break
		}
	}
	if !same {
		m.list.Clear()
		m.ids = nil
		for _, p := range images {
			m.ids = append(m.ids, p.ID)
			m.list.AddItem(tview.Escape(p.Name), imageStatus(p), 0, nil)
		}
	} else {
		for i, p := range images {
			m.list.SetItemText(i, tview.Escape(p.Name), imageStatus(p))
		}
	}
	index := 0
	for i, p := range images {
		if p.ID == selected {
			index = i
		}
	}
	if len(images) > 0 {
		m.selected = images[index].ID
		m.list.SetCurrentItem(index)
	} else {
		m.selected = ""
	}
	m.showDetails()
}

func (m *imageManager) showDetails() {
	p := m.profile()
	if len(m.buttons) > 0 {
		m.buttons[1].SetDisabled(p.ID == "")
		m.buttons[2].SetDisabled(p.ID == "" || p.Download.Active())
		m.buttons[3].SetDisabled(!p.Download.Active() || p.Download.Status == "cancelling")
		m.buttons[4].SetDisabled(p.ID == "" || p.Download.Active())
	}
	if p.ID == "" {
		m.details.SetText("No image profiles.\n\nChoose New to add a name, registry reference and launch command.")
		return
	}
	source, kind := p.Image, "OCI registry"
	if p.Archive != "" {
		source, kind = p.Archive, "Local archive"
	}
	text := fmt.Sprintf("%s\n\n%s\n%s\n\nLaunch  %s\nStatus  %s\n", p.Name, kind, source, p.Command, imageStatus(p))
	d := p.Download
	if d.Total > 0 {
		percent := min(int(100*d.Completed/d.Total), 100)
		text += fmt.Sprintf("\n[%s%s] %d%%\nArchive: %.1f / %.1f MB\n", strings.Repeat("=", percent/5), strings.Repeat("-", 20-percent/5), percent, float64(d.Completed)/1e6, float64(d.Total)/1e6)
	}
	if d.Status == "importing" {
		text += "\nImporting verified layers into microsandbox...\n"
	}
	if d.Digest != "" {
		text += "\nDigest\n" + d.Digest + "\n"
	}
	if d.Error != "" {
		text += "\n" + d.Error + "\n"
	}
	text += fmt.Sprintf("\n%d profile mappings · %d shared mappings\n\nDownloads continue when this screen or the UI closes.\nDownload again to refresh a tag or retry.\nDeleting a profile does not remove cached layers or session disks.", len(p.Mappings), len(m.u.state.Defaults))
	if text != m.details.GetText(false) {
		m.details.SetText(text)
	}
}

func (m *imageManager) edit() {
	if p := m.profile(); p.ID != "" {
		m.u.imageForm(p)
	}
}
func (m *imageManager) action(action string) {
	if m.selected == "" {
		return
	}
	m.send(supervisor.Request{Action: action, ID: m.selected})
}
func (m *imageManager) send(r supervisor.Request) {
	m.u.enqueue(func() {
		_, err := m.u.client.Call(r)
		m.u.app.QueueUpdateDraw(func() {
			if err != nil {
				m.u.status.SetText(err.Error())
			} else {
				m.u.status.SetText("Request accepted. Downloads are owned by the supervisor.")
			}
		})
	})
}
func (m *imageManager) delete() {
	if p := m.profile(); p.ID != "" {
		m.u.confirm("Delete this profile? Cached images and existing sessions are retained.", "Delete", func() { m.send(supervisor.Request{Action: "delete-image", ID: p.ID}) })
	}
}
func (m *imageManager) close() {
	m.u.images = nil
	m.u.pages.RemovePage("images").SwitchToPage("main")
	m.u.app.SetFocus(m.u.tree)
}
func (m *imageManager) input(e *tcell.EventKey) *tcell.EventKey {
	if e.Key() == tcell.KeyEscape {
		m.close()
		return nil
	}
	if e.Key() == tcell.KeyTab || e.Key() == tcell.KeyBacktab {
		items := []tview.Primitive{m.list, m.details}
		for _, b := range m.buttons {
			if !b.IsDisabled() {
				items = append(items, b)
			}
		}
		index := 0
		for i, p := range items {
			if p.HasFocus() {
				index = i
				break
			}
		}
		delta := 1
		if e.Key() == tcell.KeyBacktab {
			delta = -1
		}
		m.u.app.SetFocus(items[(index+delta+len(items))%len(items)])
		return nil
	}
	if e.Key() == tcell.KeyRune {
		switch e.Rune() {
		case 'n':
			m.u.imageForm(manager.ImageProfile{Command: "/bin/bash -l"})
		case 'e':
			m.edit()
		case 'd':
			m.action("download-image")
		case 'c':
			m.action("cancel-image-download")
		case 'x':
			m.delete()
		default:
			return e
		}
		return nil
	}
	return e
}
