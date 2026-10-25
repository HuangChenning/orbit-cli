// Package updater installs verified native binaries from stable GitHub releases.
package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const latestURL = "https://api.github.com/repos/HuangChenning/orbit-cli/releases/latest"
const maxArchive = 128 << 20
const maxBinary = 256 << 20

var stableTag = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}
type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Result describes a stable release check or completed installation.
type Result struct {
	Current, Latest string
	Available       bool
	Backup          string
}
type client struct {
	http     *http.Client
	endpoint string
	local    bool
}

func productionClient() client {
	return client{http: &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return validateURL(req.URL.String(), false)
	}}, endpoint: latestURL}
}

func validateURL(raw string, local bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && !(local && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
		return fmt.Errorf("release URL must use HTTPS without credentials")
	}
	return nil
}

func (c client) get(ctx context.Context, raw string, limit int64) ([]byte, error) {
	if err := validateURL(raw, c.local); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "orbit-cli-updater")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound && raw == c.endpoint {
		return nil, fmt.Errorf("no stable release published")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", raw, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("download exceeds %d byte limit", limit)
	}
	return b, nil
}

func version(v string) ([3]uint64, error) {
	var result [3]uint64
	m := stableTag.FindStringSubmatch(v)
	if m == nil {
		return result, fmt.Errorf("invalid stable version %q", v)
	}
	for i := range result {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return result, fmt.Errorf("invalid stable version %q", v)
		}
		result[i] = n
	}
	return result, nil
}
func available(current, latest string) (bool, error) {
	target, err := version(latest)
	if err != nil {
		return false, err
	}
	if current == "dev" {
		return true, nil
	}
	source, err := version(current)
	if err != nil {
		return false, err
	}
	for i := range source {
		if source[i] != target[i] {
			return source[i] < target[i], nil
		}
	}
	return false, nil
}

func archiveName(tag, goos, arch string) (string, string, error) {
	if _, err := version(tag); err != nil {
		return "", "", err
	}
	target := goos + "_" + arch
	switch target {
	case "darwin_arm64", "darwin_amd64", "linux_amd64", "windows_amd64":
	default:
		return "", "", fmt.Errorf("unsupported update platform %s", target)
	}
	extension, binary := "tar.gz", "orbit"
	if goos == "windows" {
		extension, binary = "zip", "orbit.exe"
	}
	return "orbit_" + strings.TrimPrefix(tag, "v") + "_" + target + "." + extension, binary, nil
}
func (c client) check(ctx context.Context, current, goos, arch string) (Result, release, error) {
	result := Result{Current: current}
	b, err := c.get(ctx, c.endpoint, 2<<20)
	if err != nil {
		return result, release{}, err
	}
	var r release
	if err := json.Unmarshal(b, &r); err != nil {
		return result, r, fmt.Errorf("invalid release metadata: %w", err)
	}
	if r.Draft || r.Prerelease {
		return result, r, fmt.Errorf("latest release is not stable")
	}
	result.Latest = r.Tag
	result.Available, err = available(current, r.Tag)
	if err != nil {
		return result, r, err
	}
	name, _, err := archiveName(r.Tag, goos, arch)
	if err != nil {
		return result, r, err
	}
	if _, _, err := c.assets(r, name); err != nil {
		return result, r, err
	}
	return result, r, nil
}
func (c client) assets(r release, name string) (string, string, error) {
	var archive, sums string
	seen := map[string]bool{}
	for _, a := range r.Assets {
		if seen[a.Name] {
			return "", "", fmt.Errorf("duplicate release asset %q", a.Name)
		}
		seen[a.Name] = true
		if a.Name == name || a.Name == "SHA256SUMS" {
			if err := validateURL(a.URL, c.local); err != nil {
				return "", "", err
			}
			if a.Name == name {
				archive = a.URL
			} else {
				sums = a.URL
			}
		}
	}
	if archive == "" || sums == "" {
		return "", "", fmt.Errorf("release requires %s and SHA256SUMS assets", name)
	}
	return archive, sums, nil
}

// Check reports whether a newer stable release is available without loading Orbit configuration.
func Check(ctx context.Context, current string) (Result, error) {
	result, _, err := productionClient().check(ctx, current, runtime.GOOS, runtime.GOARCH)
	return result, err
}

// Update verifies and installs a newer stable release at the resolved executable path.
func Update(ctx context.Context, current string) (Result, error) {
	executable, err := os.Executable()
	if err != nil {
		return Result{}, err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return Result{}, err
	}
	return productionClient().update(ctx, current, runtime.GOOS, runtime.GOARCH, executable)
}
func (c client) update(ctx context.Context, current, goos, arch, destination string) (Result, error) {
	result, r, err := c.check(ctx, current, goos, arch)
	if err != nil || !result.Available {
		return result, err
	}
	name, binary, _ := archiveName(r.Tag, goos, arch)
	archiveURL, sumsURL, err := c.assets(r, name)
	if err != nil {
		return result, err
	}
	sums, err := c.get(ctx, sumsURL, 1<<20)
	if err != nil {
		return result, err
	}
	expected, err := checksum(sums, name)
	if err != nil {
		return result, err
	}
	archive, err := c.get(ctx, archiveURL, maxArchive)
	if err != nil {
		return result, err
	}
	actual := sha256.Sum256(archive)
	if !bytes.Equal(actual[:], expected) {
		return result, fmt.Errorf("SHA256 checksum mismatch for %s", name)
	}
	payload, err := extract(archive, binary, goos == "windows")
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Backup, err = install(destination, payload)
	return result, err
}

func checksum(data []byte, name string) ([]byte, error) {
	seen := map[string]bool{}
	var selected []byte
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || len(fields[0]) != 64 {
			return nil, fmt.Errorf("malformed SHA256SUMS entry")
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil {
			return nil, fmt.Errorf("malformed SHA256SUMS digest")
		}
		file := strings.TrimPrefix(fields[1], "*")
		if !safeMember(file) || strings.Contains(file, "/") || seen[file] {
			return nil, fmt.Errorf("ambiguous SHA256SUMS entry %q", file)
		}
		seen[file] = true
		if file == name {
			selected = digest
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("SHA256SUMS missing %s", name)
	}
	return selected, nil
}
func safeMember(name string) bool {
	return name != "" && name != "." && !strings.Contains(name, "\\") && !strings.Contains(name, ":") && !path.IsAbs(name) && path.Clean(name) == strings.TrimSuffix(name, "/") && !strings.HasPrefix(name, "../") && name != ".."
}
func extract(archive []byte, binary string, isZip bool) ([]byte, error) {
	var payload []byte
	found := false
	take := func(name string, regular bool, size int64, r io.Reader) error {
		if !safeMember(name) {
			return fmt.Errorf("unsafe archive member %q", name)
		}
		if name != binary {
			return nil
		}
		if found {
			return fmt.Errorf("duplicate archive binary %s", binary)
		}
		found = true
		if !regular {
			return fmt.Errorf("archive binary is not a regular file")
		}
		if size <= 0 || size > maxBinary {
			return fmt.Errorf("archive binary size invalid")
		}
		var err error
		payload, err = io.ReadAll(io.LimitReader(r, maxBinary+1))
		if err != nil {
			return err
		}
		if int64(len(payload)) != size || len(payload) > maxBinary {
			return fmt.Errorf("archive binary size mismatch")
		}
		return nil
	}
	if isZip {
		z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range z.File {
			if !safeMember(f.Name) {
				return nil, fmt.Errorf("unsafe archive member %q", f.Name)
			}
			if f.Name != binary {
				continue
			}
			if f.UncompressedSize64 > maxBinary {
				return nil, fmt.Errorf("archive binary size invalid")
			}
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			err = take(f.Name, f.Mode().IsRegular(), int64(f.UncompressedSize64), r)
			closeErr := r.Close()
			if err != nil {
				return nil, err
			}
			if closeErr != nil {
				return nil, closeErr
			}
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		tr := tar.NewReader(io.LimitReader(gz, maxBinary+(4<<20)))
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if err := take(h.Name, h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA, h.Size, tr); err != nil {
				return nil, err
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("archive missing %s", binary)
	}
	return payload, nil
}

func install(destination string, payload []byte) (string, error) {
	info, err := os.Lstat(destination)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("destination is not a regular file")
	}
	staged, err := os.CreateTemp(filepath.Dir(destination), ".orbit-update-*")
	if err != nil {
		return "", err
	}
	stage := staged.Name()
	defer os.Remove(stage)
	if _, err = staged.Write(payload); err == nil {
		err = staged.Chmod(0755)
	}
	if err == nil {
		err = staged.Sync()
	}
	closeErr := staged.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	return replaceExecutable(stage, destination)
}
