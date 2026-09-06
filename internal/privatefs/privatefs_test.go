package privatefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateDirectoryAndReplacement(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state with spaces 配置")
	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := Check(dir); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "state.json")
	for _, data := range []string{"first", "replacement"} {
		f, err := os.CreateTemp(dir, ".state-*")
		if err != nil {
			t.Fatal(err)
		}
		if err := Protect(f.Name()); err != nil {
			f.Close()
			t.Fatal(err)
		}
		_, err = f.WriteString(data)
		if err == nil {
			err = f.Sync()
		}
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := Replace(f.Name(), target); err != nil {
			t.Fatal(err)
		}
		if err := Check(target); err != nil {
			t.Fatal("replacement lost native permissions", err)
		}
		got, err := os.ReadFile(target)
		if err != nil || string(got) != data {
			t.Fatal("replacement lost contents", err)
		}
	}
	if err := Replace(filepath.Join(dir, "missing"), target); err == nil {
		t.Fatal("missing source accepted")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "replacement" {
		t.Fatal("failed replacement changed original", err)
	}
}
