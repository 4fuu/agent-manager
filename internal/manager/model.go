package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Built and imported locally using images/Containerfile; no unpublished registry default.
const DefaultImage = "uri-agent-manager:2026.904.3"

type Mapping struct {
	Host     string
	Guest    string
	Writable bool
}
type Project struct {
	ID       string
	Name     string
	URL      string
	Image    string
	Ref      string
	Command  string
	Mappings []Mapping
	// AuthFile is an explicit, dedicated GitHub token file, never a token value.
	AuthFile string
}
type Instance struct {
	ID        string
	ProjectID string
	Config    Project
	Status    string
	SetupDone bool
	Cloned    bool
	RuntimeID string
	Error     string
}
type State struct {
	Version   int
	Projects  []Project
	Defaults  []Mapping
	Instances []Instance
}

func MergeMappings(global, project []Mapping) []Mapping {
	out := append([]Mapping(nil), global...)
	for _, m := range project {
		found := false
		for i := range out {
			if out[i].Guest == m.Guest {
				out[i] = m
				found = true
				break
			}
		}
		if !found {
			out = append(out, m)
		}
	}
	return out
}

func ValidateMappings(ms []Mapping) error {
	seen := map[string]bool{}
	home, _ := os.UserHomeDir()
	for _, m := range ms {
		if !filepath.IsAbs(m.Host) || !path.IsAbs(m.Guest) || path.Clean(m.Guest) != m.Guest {
			return errors.New("mappings require clean absolute host and guest paths")
		}
		resolved, err := filepath.EvalSymlinks(m.Host)
		if err != nil {
			return fmt.Errorf("mapping source unavailable: %w", err)
		}
		st, err := os.Stat(resolved)
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() || resolved == home {
			return errors.New("map individual regular config files, not directories or devices")
		}
		if m.Guest == "/" || strings.HasPrefix(m.Guest, "/workspace/") || m.Guest == "/workspace" || strings.HasPrefix(m.Guest, "/run/manager/") {
			return errors.New("mapping target conflicts with workspace or manager state")
		}
		if seen[m.Guest] {
			return errors.New("duplicate guest mapping")
		}
		seen[m.Guest] = true
	}
	return nil
}
func ValidateProject(p Project) error {
	u, err := url.Parse(p.URL)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("repository must be https://github.com/owner/repo without credentials")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("repository must identify an owner and repository")
	}
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Image) == "" || strings.TrimSpace(p.Command) == "" {
		return errors.New("name, image and launch command are required")
	}
	if strings.HasPrefix(p.Ref, "-") || strings.ContainsAny(p.Ref, "\r\n\x00") {
		return errors.New("invalid Git ref")
	}
	if p.AuthFile != "" {
		if !filepath.IsAbs(p.AuthFile) {
			return errors.New("GitHub token file must be absolute")
		}
		st, err := os.Stat(p.AuthFile)
		if err != nil {
			return errors.New("GitHub token file is unavailable")
		}
		if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
			return errors.New("GitHub token file must be a regular file with mode 0600 or stricter")
		}
	}
	return ValidateMappings(p.Mappings)
}

func Load(dir string) (State, error) {
	s := State{Version: 1}
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	if s.Version != 1 {
		return s, errors.New("unsupported state version")
	}
	return s, nil
}
func Save(dir string, s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), filepath.Join(dir, "state.json")); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
