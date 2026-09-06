package ui

import (
	"strings"
	"testing"

	"github.com/4fuu/agent-manager/internal/manager"
	"github.com/gdamore/tcell/v2"
)

func TestImageManagerSelectionProgressAndFocus(t *testing.T) {
	u := New(nil)
	u.state.Images = manager.BuiltinImages()
	u.imagesMenu()
	m := u.images
	m.list.SetCurrentItem(2)
	if m.selected != "omp" {
		t.Fatal("selection not routed")
	}
	u.state.Images[2].Download = manager.ImageDownload{Status: "downloading", Completed: 25, Total: 100}
	m.update()
	if !strings.Contains(m.details.GetText(false), "25%") || !m.buttons[2].IsDisabled() || m.buttons[3].IsDisabled() {
		t.Fatal("download controls/progress", m.details.GetText(false))
	}
	u.state.Images = append(u.state.Images, manager.ImageProfile{ID: "new", Name: "New image"})
	m.update()
	if m.selected != "omp" {
		t.Fatal("list rebuild lost selection")
	}
	m.edit()
	m.update()
	if !u.modalView.HasFocus() {
		t.Fatal("progress stole form focus")
	}
	u.closeModal()
	if !m.list.HasFocus() {
		t.Fatal("form did not return to image manager")
	}
	m.input(tcell.NewEventKey(tcell.KeyTab, 0, 0))
	if !m.details.HasFocus() {
		t.Fatal("details cannot receive scrolling input")
	}
	m.input(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	if u.images != nil || !u.tree.HasFocus() {
		t.Fatal("back did not restore workspace")
	}
}
