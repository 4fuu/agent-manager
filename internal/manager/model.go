package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/4fuu/agent-manager/internal/buildinfo"
	"github.com/4fuu/agent-manager/internal/privatefs"
)

var DefaultImage = "ghcr.io/4fuu/agent-manager/uri:" + buildinfo.Version

const StateVersion = 3

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
	Archive  string
	Ref      string
	Command  string
	Mappings []Mapping
	// Preset supplies defaults only; instances retain the resolved configuration.
	Preset string
	// Environment is passed to guest setup and terminals and stored in private state.
	Environment map[string]string
	// AuthFile is an explicit, dedicated GitHub token file, never a token value.
	AuthFile string
}
type ImageProfile struct {
	ID          string
	Name        string
	Image       string
	Archive     string
	Command     string
	Mappings    []Mapping
	Environment map[string]string
	Download    ImageDownload
}

// Download records the last explicit transfer, not a guarantee against external cache removal.
type ImageDownload struct {
	Status, Error, Digest string
	Completed, Total      int64
}

func (d ImageDownload) Active() bool {
	return d.Status == "resolving" || d.Status == "downloading" || d.Status == "importing" || d.Status == "cancelling"
}

type Pane struct {
	ID      string
	Command string
	Width   int
}
type Instance struct {
	ID        string
	Name      string
	Title     string
	Panes     []Pane
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
	Images    []ImageProfile
	Projects  []Project
	Defaults  []Mapping
	Instances []Instance
}

var builtinImages = []ImageProfile{
	{ID: "uri", Name: "URI Agent", Image: DefaultImage, Command: "uri-agent", Environment: map[string]string{"URI_AGENT_CONFIG_DIR": "/root/.config/uri-agent"}},
	{ID: "pi", Name: "Pi", Image: "ghcr.io/4fuu/agent-manager/pi:" + buildinfo.Version, Command: "pi"},
	{ID: "omp", Name: "Oh My Pi", Image: "ghcr.io/4fuu/agent-manager/omp:" + buildinfo.Version, Command: "omp"},
	{ID: "claude", Name: "Claude Code", Image: "ghcr.io/4fuu/agent-manager/claude:" + buildinfo.Version, Command: "claude"},
	{ID: "codex", Name: "Codex", Image: "ghcr.io/4fuu/agent-manager/codex:" + buildinfo.Version, Command: "codex"},
}

// BuiltinImages returns independent profiles for images built from images/Containerfile.
func BuiltinImages() []ImageProfile {
	out := make([]ImageProfile, len(builtinImages))
	for i, image := range builtinImages {
		out[i] = image
		out[i].Mappings = append([]Mapping(nil), image.Mappings...)
		out[i].Environment = maps.Clone(image.Environment)
	}
	return out
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
	for _, m := range ms {
		if !filepath.IsAbs(m.Host) || !path.IsAbs(m.Guest) || path.Clean(m.Guest) != m.Guest || strings.ContainsAny(m.Guest, "\\\x00\r\n") {
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
		if !st.Mode().IsRegular() && !st.IsDir() {
			return errors.New("mapping source must be a regular file or directory")
		}
		if m.Guest == "/" || strings.HasPrefix(m.Guest, "/workspace/") || m.Guest == "/workspace" || m.Guest == "/run" || m.Guest == "/run/manager" || strings.HasPrefix(m.Guest, "/run/manager/") {
			return errors.New("mapping target conflicts with workspace or manager state")
		}
		for guest := range seen {
			if guest == m.Guest || strings.HasPrefix(guest, m.Guest+"/") || strings.HasPrefix(m.Guest, guest+"/") {
				return errors.New("overlapping guest mappings are ambiguous")
			}
		}
		seen[m.Guest] = true
	}
	return nil
}

func ValidateImage(image ImageProfile) error {
	if strings.TrimSpace(image.ID) == "" || strings.TrimSpace(image.Name) == "" || strings.TrimSpace(image.Command) == "" {
		return errors.New("image ID, name and launch command are required")
	}
	if (strings.TrimSpace(image.Image) == "") == (strings.TrimSpace(image.Archive) == "") {
		return errors.New("image requires exactly one OCI reference or archive")
	}
	if image.Archive != "" {
		if !filepath.IsAbs(image.Archive) {
			return errors.New("image archive must be absolute")
		}
		st, err := os.Stat(image.Archive)
		if err != nil || !st.Mode().IsRegular() {
			return errors.New("image archive must be an available regular file")
		}
	}
	if err := validateEnvironment(image.Environment); err != nil {
		return err
	}
	return ValidateMappings(image.Mappings)
}

func ValidateRepository(p Project) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("project name is required")
	}
	u, err := url.Parse(p.URL)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("repository must be https://github.com/owner/repo without credentials")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("repository must identify an owner and repository")
	}
	if p.AuthFile != "" {
		if !filepath.IsAbs(p.AuthFile) {
			return errors.New("GitHub token file must be absolute")
		}
		st, err := os.Stat(p.AuthFile)
		if err != nil || !st.Mode().IsRegular() {
			return errors.New("GitHub token file must be an available regular file")
		}
		if err := privatefs.Check(p.AuthFile); err != nil {
			return fmt.Errorf("GitHub token file is not private: %w", err)
		}
	}
	return nil
}

func validateEnvironment(environment map[string]string) error {
	for key, value := range environment {
		if key == "" || strings.ContainsRune(value, '\x00') {
			return errors.New("invalid guest environment")
		}
		for i, c := range key {
			if c != '_' && !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') && !(i > 0 && c >= '0' && c <= '9') {
				return errors.New("invalid guest environment variable name")
			}
		}
	}
	return nil
}
func ValidateProject(p Project) error {
	if p.Preset != "" && p.Preset != "custom" && p.Preset != "uri" && p.Preset != "pi" && p.Preset != "omp" && p.Preset != "claude" && p.Preset != "codex" {
		return errors.New("unknown Agent preset")
	}
	if err := ValidateRepository(p); err != nil {
		return err
	}
	if err := validateEnvironment(p.Environment); err != nil {
		return err
	}
	if strings.TrimSpace(p.Command) == "" || ((strings.TrimSpace(p.Image) == "") == (strings.TrimSpace(p.Archive) == "")) {
		return errors.New("launch command and exactly one image or archive are required")
	}
	if strings.HasPrefix(p.Ref, "-") || strings.ContainsAny(p.Ref, "\r\n\x00") {
		return errors.New("invalid Git ref")
	}
	if p.Archive != "" {
		if err := ValidateImage(ImageProfile{ID: "project", Name: p.Name, Archive: p.Archive, Command: p.Command}); err != nil {
			return err
		}
	}
	return ValidateMappings(p.Mappings)
}

func Load(dir string) (State, error) {
	s := State{}
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if os.IsNotExist(err) {
		return State{Version: StateVersion, Images: BuiltinImages()}, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	if s.Version != StateVersion {
		return s, errors.New("unsupported state version; use a new state directory")
	}
	return s, nil
}
func Save(dir string, s State) error {
	if s.Version != StateVersion {
		return errors.New("unsupported state version")
	}
	if err := privatefs.EnsureDir(dir); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = privatefs.Protect(f.Name()); err == nil {
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
	return privatefs.Replace(f.Name(), filepath.Join(dir, "state.json"))
}

// ResolveProject fills only missing values and takes ownership of mutable data.
func ResolveProject(p Project) (Project, error) {
	p.Mappings = append([]Mapping(nil), p.Mappings...)
	env := map[string]string{"TERM": "xterm-256color"}
	var profile *ImageProfile
	switch p.Preset {
	case "", "custom":
		p.Preset = "custom"
	default:
		for i := range builtinImages {
			if builtinImages[i].ID == p.Preset {
				profile = &builtinImages[i]
				break
			}
		}
		if profile == nil {
			return Project{}, errors.New("unknown Agent preset")
		}
	}
	if profile != nil {
		if p.Command == "" {
			p.Command = profile.Command
		}
		if p.Image == "" && p.Archive == "" {
			p.Image = profile.Image
			p.Archive = profile.Archive
		}
		p.Mappings = MergeMappings(profile.Mappings, p.Mappings)
		maps.Copy(env, profile.Environment)
	}
	maps.Copy(env, p.Environment)
	p.Environment = env
	return p, ValidateProject(p)
}
