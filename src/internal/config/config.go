package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

type Dependency struct {
	Name      string   `yaml:"name" json:"name"`
	Source    string   `yaml:"source" json:"source"`
	Ref       string   `yaml:"ref" json:"ref"`
	Version   string   `yaml:"version,omitempty" json:"-"`
	Subdir    string   `yaml:"subdir,omitempty" json:"subdir"`
	MirrorDir string   `yaml:"mirror_dir,omitempty" json:"mirror_dir"`
	Vendor    *bool    `yaml:"vendor,omitempty" json:"vendor,omitempty"`
	Link      bool     `yaml:"link" json:"link"`
	Labels    []string `yaml:"labels,omitempty" json:"labels"`
}

type Repository struct {
	Remote    string `yaml:"remote"`
	Branch    string `yaml:"branch"`
	LocalPath string `yaml:"local_path"`
}

type Config struct {
	Version      string              `yaml:"version"`
	GlobalCache  string              `yaml:"global_cache"`
	OrbitRepo    *Repository         `yaml:"orbit_repo,omitempty"`
	LinkTargets  map[string][]string `yaml:"link_targets,omitempty"`
	Dependencies []Dependency        `yaml:"dependencies"`
	Path         string              `yaml:"-"`
}

func Find(start, explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		p := filepath.Join(dir, "orbit.yaml")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	home, err := os.UserHomeDir()
	if err == nil {
		p := filepath.Join(home, ".orbit", "orbit.yaml")
		if _, err = os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("orbit.yaml not found in project parents or ~/.orbit; run orbit init or use --config")
}

func Load(path string) (*Config, error) {
	return load(path, false)
}

// LoadCoverage tolerates an omitted link category only for read-only inventory
// comparison/repair. Operational commands and the repaired candidate stay strict.
func LoadCoverage(path string) (*Config, error) { return load(path, true) }

func load(path string, coverage bool) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = c.resolve(coverage); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

func Parse(b []byte) (*Config, error) {
	var c Config
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	if err := d.Decode(&c); err != nil {
		if strings.Contains(err.Error(), "already defined") {
			return nil, fmt.Errorf("duplicate YAML key: %w", err)
		}
		return nil, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("expected exactly one YAML document")
	}
	var node yaml.Node
	if err := yaml.Unmarshal(b, &node); err != nil {
		return nil, err
	}
	if len(node.Content) != 1 {
		return nil, fmt.Errorf("expected a YAML mapping")
	}
	if err := scalarTypes(node.Content[0], reflect.TypeOf(c), "config"); err != nil {
		return nil, err
	}
	for i := range c.Dependencies {
		d := &c.Dependencies[i]
		if d.Ref == "" {
			d.Ref = d.Version
		}
		d.Version = "" // Legacy input is always written back using ref.
	}
	return &c, nil
}

// YAML permits numeric/bool scalars to coerce to Go strings. Paths and refs must
// actually be strings, so a typo cannot silently become a different filesystem path.
func scalarTypes(n *yaml.Node, t reflect.Type, path string) error {
	if n.Kind == yaml.AliasNode {
		return scalarTypes(n.Alias, t, path)
	}
	if t.Kind() == reflect.Pointer {
		if n.Tag == "!!null" && t.Elem().Kind() != reflect.Bool {
			return nil
		}
		return scalarTypes(n, t.Elem(), path)
	}
	bad := func() error { return fmt.Errorf("line %d: %s has an incorrect YAML type", n.Line, path) }
	switch t.Kind() {
	case reflect.String:
		if n.Tag != "!!str" {
			return bad()
		}
	case reflect.Bool:
		if n.Tag != "!!bool" {
			return bad()
		}
	case reflect.Struct:
		if n.Kind != yaml.MappingNode {
			return bad()
		}
		if t == reflect.TypeOf(Dependency{}) {
			// Resolve YAML merges too, so aliases cannot bypass field conflicts or types.
			var fields map[string]yaml.Node
			if err := n.Decode(&fields); err != nil {
				return err
			}
			_, ref := fields["ref"]
			_, version := fields["version"]
			if ref && version {
				return fmt.Errorf("line %d: dependency ref and legacy version are mutually exclusive", n.Line)
			}
			resolved := *n
			resolved.Content = nil
			for key, value := range fields {
				resolved.Content = append(resolved.Content, &yaml.Node{Value: key}, &value)
			}
			n = &resolved
		}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i].Value
			for j := 0; j < t.NumField(); j++ {
				field := t.Field(j)
				tag := strings.Split(field.Tag.Get("yaml"), ",")[0]
				if tag == key && tag != "-" {
					if err := scalarTypes(n.Content[i+1], field.Type, path+"."+key); err != nil {
						return err
					}
				}
			}
		}
	case reflect.Slice:
		if n.Kind != yaml.SequenceNode {
			return bad()
		}
		for _, child := range n.Content {
			if err := scalarTypes(child, t.Elem(), path); err != nil {
				return err
			}
		}
	case reflect.Map:
		if n.Kind != yaml.MappingNode {
			return bad()
		}
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Tag != "!!str" {
				return bad()
			}
			if err := scalarTypes(n.Content[i+1], t.Elem(), path+"."+n.Content[i].Value); err != nil {
				return err
			}
		}
	}
	return nil
}

var drive = regexp.MustCompile(`^[A-Za-z]:`)

func Relative(s string, root bool) (string, error) {
	s = strings.ReplaceAll(s, `\`, "/")
	if s == "" || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") || drive.MatchString(s) {
		return "", fmt.Errorf("unsafe relative path %q", s)
	}
	for _, p := range strings.Split(s, "/") {
		if p == ".." || p == ".git" || p == ".orbit-manifest.yaml" {
			return "", fmt.Errorf("reserved or escaping path %q", s)
		}
	}
	s = filepath.ToSlash(filepath.Clean(s))
	if s == "." && !root {
		return "", fmt.Errorf("root path is not allowed")
	}
	return s, nil
}

func Overlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}

func (c *Config) Expand(s string) (string, error) {
	if s == "~" || strings.HasPrefix(s, "~/") {
		h, e := os.UserHomeDir()
		if e != nil {
			return "", e
		}
		s = filepath.Join(h, strings.TrimPrefix(s, "~/"))
	} else if strings.HasPrefix(s, "~") {
		return "", fmt.Errorf("unsupported home path %q", s)
	}
	if !filepath.IsAbs(s) {
		s = filepath.Join(filepath.Dir(c.Path), s)
	}
	// macOS exposes system temporary directories through these fixed aliases.
	if runtime.GOOS == "darwin" {
		for _, alias := range []string{"/var/", "/tmp/"} {
			if strings.HasPrefix(s, alias) {
				s = "/private" + s
				break
			}
		}
	}
	return filepath.Abs(s)
}

// NoSymlinks checks each existing ancestor before any write or deletion.
func NoSymlinks(path string) error {
	path = filepath.Clean(path)
	for {
		if runtime.GOOS == "darwin" && (path == "/var" || path == "/tmp") {
			break
		}
		info, err := os.Lstat(path)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in managed path: %s", path)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return nil
}

func (c *Config) Resolve() error {
	return c.resolve(false)
}

func (c *Config) resolve(coverage bool) error {
	if c.Version != "1.0" {
		return fmt.Errorf("version must be the string 1.0")
	}
	if strings.TrimSpace(c.GlobalCache) == "" {
		return fmt.Errorf("global_cache is required")
	}
	var err error
	c.GlobalCache, err = c.Expand(c.GlobalCache)
	if err != nil {
		return err
	}
	if err = protected(c.GlobalCache); err != nil {
		return err
	}
	if c.OrbitRepo != nil {
		r := c.OrbitRepo
		if r.Remote == "" || r.Branch == "" {
			return fmt.Errorf("orbit_repo requires remote and branch")
		}
		if r.LocalPath == "" {
			r.LocalPath = "~/.orbit/repo"
		}
		if strings.HasPrefix(r.Remote, "-") || strings.HasPrefix(r.Branch, "-") || strings.ContainsAny(r.Branch, " ~^:?*[\\") {
			return fmt.Errorf("invalid mirror remote or branch")
		}
		r.LocalPath, err = c.Expand(r.LocalPath)
		if err != nil {
			return err
		}
		if err = protected(r.LocalPath); err != nil {
			return err
		}
		if Overlap(r.LocalPath, c.GlobalCache) {
			return fmt.Errorf("mirror and cache paths overlap")
		}
		if err = NoSymlinks(r.LocalPath); err != nil {
			return err
		}
	}
	if err = NoSymlinks(c.GlobalCache); err != nil {
		return err
	}
	for group, targets := range c.LinkTargets {
		if s, e := Relative(group, false); e != nil || strings.Contains(s, "/") {
			return fmt.Errorf("invalid link_targets category %q", group)
		}
		seen := map[string]bool{}
		resolved := []string{}
		for _, t := range targets {
			if strings.TrimSpace(t) == "" {
				return fmt.Errorf("empty link target")
			}
			p, e := c.Expand(t)
			if e != nil {
				return e
			}
			if seen[p] {
				continue // Coverage check reports and can repair identical declarations.
			}
			seen[p] = true
			resolved = append(resolved, p)
		}
		c.LinkTargets[group] = resolved
	}
	names := map[string]bool{}
	var mirrors, links []string
	for i := range c.Dependencies {
		d := &c.Dependencies[i]
		if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Source) == "" || strings.TrimSpace(d.Ref) == "" {
			return fmt.Errorf("dependency %d: name, source and ref are required", i+1)
		}
		if names[d.Name] {
			return fmt.Errorf("duplicate dependency name %q", d.Name)
		}
		names[d.Name] = true
		if strings.HasPrefix(d.Source, "-") || strings.HasPrefix(d.Ref, "-") || strings.ContainsAny(d.Ref, "\n\r\x00") {
			return fmt.Errorf("invalid source or ref for %s", d.Name)
		}
		if !strings.Contains(d.Source, ":") {
			d.Source, err = c.Expand(d.Source)
			if err != nil {
				return err
			}
		}
		if d.Subdir == "" {
			d.Subdir = "."
		}
		d.Subdir, err = Relative(d.Subdir, true)
		if err != nil {
			return fmt.Errorf("%s subdir: %w", d.Name, err)
		}
		if d.MirrorDir != "" {
			d.MirrorDir, err = Relative(d.MirrorDir, false)
			if err != nil {
				return fmt.Errorf("%s mirror_dir: %w", d.Name, err)
			}
			for _, p := range mirrors {
				if Overlap(filepath.FromSlash(p), filepath.FromSlash(d.MirrorDir)) {
					return fmt.Errorf("overlapping mirror_dir %s and %s", p, d.MirrorDir)
				}
			}
			mirrors = append(mirrors, d.MirrorDir)
		}
		for _, label := range d.Labels {
			if strings.TrimSpace(label) == "" {
				return fmt.Errorf("empty label for %s", d.Name)
			}
		}
		if d.Link {
			ts := c.Targets(*d)
			if len(ts) == 0 {
				if coverage && d.MirrorDir != "" {
					continue
				}
				return fmt.Errorf("%s: link requires mirror_dir and global link_targets for its category", d.Name)
			}
			for _, p := range ts {
				if err = protected(p); err != nil {
					return err
				}
				if Overlap(p, c.GlobalCache) || (c.OrbitRepo != nil && Overlap(p, c.OrbitRepo.LocalPath)) {
					return fmt.Errorf("link target overlaps managed storage: %s", p)
				}
				for _, other := range links {
					if Overlap(p, other) {
						return fmt.Errorf("overlapping link targets %s and %s", p, other)
					}
				}
				links = append(links, p)
			}
		}
	}
	return nil
}

func protected(p string) error {
	h, _ := os.UserHomeDir()
	if p == filepath.Dir(p) || p == h {
		return fmt.Errorf("refusing protected directory %s", p)
	}
	return nil
}

func (c *Config) Targets(d Dependency) []string {
	if !d.Link || d.MirrorDir == "" {
		return nil
	}
	group, suffix, _ := strings.Cut(d.MirrorDir, "/")
	var targets []string
	for _, root := range c.LinkTargets[group] {
		targets = append(targets, filepath.Join(root, filepath.FromSlash(suffix)))
	}
	return targets
}

// Vendoring defaults to true so existing configurations keep downloading assets.
func (d Dependency) Vendoring() bool { return d.Vendor == nil || *d.Vendor }

func selectionValues(values []string, flag string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, value := range values {
		if strings.Contains(value, "，") {
			return nil, fmt.Errorf("--%s requires an English comma (,), not a Chinese comma (，)", flag)
		}
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				return nil, fmt.Errorf("--%s contains an empty selection", flag)
			}
			if !seen[item] {
				out = append(out, item)
				seen[item] = true
			}
		}
	}
	return out, nil
}

func (c *Config) Select(labels, names []string) ([]Dependency, error) {
	if len(labels) > 0 && len(names) > 0 {
		return nil, fmt.Errorf("--name and --label are mutually exclusive")
	}
	labels, err := selectionValues(labels, "label")
	if err != nil {
		return nil, err
	}
	names, err = selectionValues(names, "name")
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, name := range names {
		found := false
		for _, d := range c.Dependencies {
			if d.Name == name {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown dependency name %q", name)
		}
		wanted[name] = true
	}
	var out []Dependency
	for _, d := range c.Dependencies {
		match := len(labels) == 0 && len(names) == 0 || wanted[d.Name]
		for _, want := range labels {
			for _, got := range d.Labels {
				if want == got {
					match = true
				}
			}
		}
		if match {
			out = append(out, d)
		}
	}
	if len(out) == 0 && len(labels) > 0 {
		return nil, fmt.Errorf("no dependencies match the requested labels")
	}
	return out, nil
}
