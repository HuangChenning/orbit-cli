package updater

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type member struct {
	name string
	kind byte
	data string
}

func bundle(t *testing.T, isZip bool, members ...member) []byte {
	t.Helper()
	var b bytes.Buffer
	if isZip {
		z := zip.NewWriter(&b)
		for _, m := range members {
			h := &zip.FileHeader{Name: m.name, Method: zip.Deflate}
			h.SetMode(0755)
			if m.kind == tar.TypeSymlink {
				h.SetMode(os.ModeSymlink | 0755)
			}
			w, err := z.CreateHeader(h)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.Write([]byte(m.data)); err != nil {
				t.Fatal(err)
			}
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&b)
		tr := tar.NewWriter(gz)
		for _, m := range members {
			h := &tar.Header{Name: m.name, Mode: 0755, Size: int64(len(m.data)), Typeflag: m.kind}
			if m.kind == tar.TypeSymlink {
				h.Size = 0
				h.Linkname = "other"
			}
			if err := tr.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if h.Size > 0 {
				if _, err := tr.Write([]byte(m.data)); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := tr.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return b.Bytes()
}
func TestNativeArchivesAndVersions(t *testing.T) {
	for _, target := range []struct{ os, arch, ext, binary string }{{"darwin", "arm64", "tar.gz", "orbit"}, {"darwin", "amd64", "tar.gz", "orbit"}, {"linux", "amd64", "tar.gz", "orbit"}, {"windows", "amd64", "zip", "orbit.exe"}} {
		name, binary, err := archiveName("v1.2.3", target.os, target.arch)
		if err != nil || name != "orbit_1.2.3_"+target.os+"_"+target.arch+"."+target.ext || binary != target.binary {
			t.Fatalf("native archive: %s %s %v", name, binary, err)
		}
	}
	if _, _, err := archiveName("v1.2.3", "linux", "arm64"); err == nil {
		t.Fatal("unsupported platform accepted")
	}
	for _, tc := range []struct {
		current, latest string
		want            bool
	}{{"v1.9.0", "v1.10.0", true}, {"v1.2.3", "v1.2.3", false}, {"v2.0.0", "v1.99.99", false}, {"dev", "v1.0.0", true}} {
		got, err := available(tc.current, tc.latest)
		if err != nil || got != tc.want {
			t.Fatalf("%s -> %s: %v %v", tc.current, tc.latest, got, err)
		}
	}
	for _, v := range []string{"v1.2.3-rc1", "1.2.3", "v01.2.3", "v1.2", "v18446744073709551616.0.0"} {
		if _, err := version(v); err == nil {
			t.Fatalf("accepted %s", v)
		}
	}
}
func TestExtractionProtectsInstallation(t *testing.T) {
	for _, isZip := range []bool{false, true} {
		binary := "orbit"
		if isZip {
			binary += ".exe"
		}
		for _, tc := range []struct {
			name    string
			members []member
			valid   bool
		}{
			{"valid", []member{{binary, tar.TypeReg, "new bytes"}, {"LICENSE", tar.TypeReg, "license"}}, true},
			{"missing", []member{{"LICENSE", tar.TypeReg, "license"}}, false},
			{"duplicate", []member{{binary, tar.TypeReg, "one"}, {binary, tar.TypeReg, "two"}}, false},
			{"nonregular", []member{{binary, tar.TypeSymlink, ""}}, false},
			{"traversal", []member{{binary, tar.TypeReg, "new"}, {"../escaped", tar.TypeReg, "bad"}}, false},
			{"absolute", []member{{"/escaped", tar.TypeReg, "bad"}, {binary, tar.TypeReg, "new"}}, false},
			{"windows path", []member{{`C:\escaped`, tar.TypeReg, "bad"}, {binary, tar.TypeReg, "new"}}, false},
		} {
			t.Run(fmt.Sprintf("%t/%s", isZip, tc.name), func(t *testing.T) {
				data, err := extract(bundle(t, isZip, tc.members...), binary, isZip)
				if tc.valid {
					if err != nil || string(data) != "new bytes" {
						t.Fatalf("%q %v", data, err)
					}
				} else if err == nil {
					t.Fatal("unsafe archive accepted")
				}
			})
		}
	}
}
func TestChecksumRejectsAmbiguity(t *testing.T) {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("archive")))
	for _, sums := range []string{"", "not-a-checksum  native.tar.gz", digest + "  other.tar.gz", digest + "  native.tar.gz\n" + digest + "  native.tar.gz", digest + "  ../native.tar.gz", digest + "  native.tar.gz extra"} {
		if _, err := checksum([]byte(sums), "native.tar.gz"); err == nil {
			t.Fatalf("accepted %q", sums)
		}
	}
	if _, err := checksum([]byte(digest+"  native.tar.gz\n"), "native.tar.gz"); err != nil {
		t.Fatal(err)
	}
}

func TestStableChecksAndVerifiedUpdates(t *testing.T) {
	name, binary, err := archiveName("v1.2.3", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	archive := bundle(t, runtime.GOOS == "windows", member{binary, tar.TypeReg, "new binary"})
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	for _, tc := range []struct {
		name, current   string
		status          int
		change          func(*release)
		sums            string
		wantErr, update bool
	}{
		{name: "verified replacement", current: "v1.0.0", update: true},
		{name: "dev can update", current: "dev", update: true},
		{name: "equal preserves binary", current: "v1.2.3"},
		{name: "newer never downgrades", current: "v2.0.0"},
		{name: "not published", current: "dev", status: 404, wantErr: true},
		{name: "draft", current: "dev", change: func(r *release) { r.Draft = true }, wantErr: true},
		{name: "prerelease", current: "dev", change: func(r *release) { r.Prerelease = true }, wantErr: true},
		{name: "unstable tag", current: "dev", change: func(r *release) { r.Tag = "v1.2.3-rc1" }, wantErr: true},
		{name: "missing archive", current: "dev", change: func(r *release) { r.Assets = r.Assets[1:] }, wantErr: true},
		{name: "duplicate asset", current: "dev", change: func(r *release) { r.Assets = append(r.Assets, r.Assets[0]) }, wantErr: true},
		{name: "checksum mismatch preserves binary", current: "dev", sums: strings.Repeat("0", 64) + "  " + name, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assetRequests := 0
			var r release
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/latest":
					if tc.status != 0 {
						w.WriteHeader(tc.status)
						return
					}
					json.NewEncoder(w).Encode(r)
				case "/sums":
					assetRequests++
					sums := tc.sums
					if sums == "" {
						sums = digest + "  " + name
					}
					fmt.Fprintln(w, sums)
				case "/archive":
					assetRequests++
					w.Write(archive)
				default:
					http.NotFound(w, req)
				}
			}))
			defer server.Close()
			r = release{Tag: "v1.2.3", Assets: []asset{{name, server.URL + "/archive"}, {"SHA256SUMS", server.URL + "/sums"}}}
			if tc.change != nil {
				tc.change(&r)
			}
			c := client{http: server.Client(), endpoint: server.URL + "/latest", local: true}
			destination := filepath.Join(t.TempDir(), binary)
			if err := os.WriteFile(destination, []byte("old binary"), 0755); err != nil {
				t.Fatal(err)
			}
			result, err := c.update(context.Background(), tc.current, runtime.GOOS, runtime.GOARCH, destination)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v, want error %t", err, tc.wantErr)
			}
			got, readErr := os.ReadFile(destination)
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := "old binary"
			if tc.update {
				want = "new binary"
			}
			if string(got) != want {
				t.Fatalf("installation got %q want %q", got, want)
			}
			if !tc.update && !tc.wantErr && assetRequests != 0 {
				t.Fatal("up-to-date command downloaded assets")
			}
			if tc.name == "not published" && !strings.Contains(err.Error(), "no stable release published") {
				t.Fatal(err)
			}
			if result.Backup != "" {
				t.Logf("backup retained: %s", result.Backup)
			}
		})
	}
}
func TestMetadataAndDownloadFailures(t *testing.T) {
	for _, body := range []string{`{`, `null`, `{ "tag_name": "v1.2.3", "assets": [] }`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		c := client{http: server.Client(), endpoint: server.URL, local: true}
		if _, _, err := c.check(context.Background(), "dev", "linux", "amd64"); err == nil {
			t.Fatalf("accepted metadata %q", body)
		}
		server.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "too large") }))
	defer server.Close()
	c := client{http: server.Client(), endpoint: server.URL, local: true}
	if _, err := c.get(context.Background(), server.URL, 2); err == nil {
		t.Fatal("size bound not enforced")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.get(ctx, server.URL, 100); err == nil {
		t.Fatal("cancellation ignored")
	}
	server.Close()
	if _, err := c.get(context.Background(), server.URL, 100); err == nil {
		t.Fatal("network error ignored")
	}
	for _, raw := range []string{"http://example.com/file", "https://user:secret@example.com/file", "file:///tmp/file"} {
		if err := validateURL(raw, false); err == nil {
			t.Fatalf("production accepted %s", raw)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err := productionClient().http.CheckRedirect(req, nil); err == nil {
		t.Fatal("production allowed HTTPS downgrade redirect")
	}
}
func TestInstallPreservesOldBinaryOnFailure(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "orbit")
	if err := os.WriteFile(destination, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}
	// A missing stage exercises Unix atomic failure and the Windows rollback path.
	if _, err := replaceExecutable(filepath.Join(t.TempDir(), "missing-stage"), destination); err == nil {
		t.Fatal("missing stage succeeded")
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "old binary" {
		t.Fatalf("old installation lost: %q %v", data, err)
	}
	if _, err := install(destination, []byte("new binary")); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(destination)
	if err != nil || string(data) != "new binary" {
		t.Fatalf("replacement failed: %q %v", data, err)
	}
	if _, err := install(filepath.Dir(destination), []byte("bad")); err == nil {
		t.Fatal("directory destination accepted")
	}
}

func TestRunningExecutableHelper(t *testing.T) {
	if os.Getenv("ORBIT_UPDATER_TEST_HELPER") != "1" {
		return
	}
	fmt.Println("ready")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceRunningExecutable(t *testing.T) {
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	name := "orbit-fixture"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	destination := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(destination, payload, 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, destination, "-test.run=^TestRunningExecutableHelper$")
	child.Env = append(os.Environ(), "ORBIT_UPDATER_TEST_HELPER=1")
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	child.Stderr = &stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); child.Process.Kill() }()
	ready := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		ready <- line
		io.Copy(io.Discard, stdout)
	}()
	select {
	case line := <-ready:
		if line != "ready\n" && line != "ready\r\n" {
			t.Fatalf("helper not ready: %q %s", line, stderr.String())
		}
	case <-ctx.Done():
		t.Fatal("helper readiness timeout")
	}
	backup, err := install(destination, []byte("verified replacement"))
	if err != nil {
		t.Fatalf("replace running executable: %v", err)
	}
	installed, err := os.ReadFile(destination)
	if err != nil || string(installed) != "verified replacement" {
		t.Fatalf("installed %q: %v", installed, err)
	}
	if runtime.GOOS == "windows" && backup == "" {
		t.Error("expected running Windows binary backup to be reported")
	}
	if backup != "" {
		old, err := os.ReadFile(backup)
		if err != nil || !bytes.Equal(old, payload) {
			t.Fatalf("backup not preserved: %v", err)
		}
	}
	if _, err := io.WriteString(stdin, "exit\n"); err != nil {
		t.Fatal(err)
	}
	stdin.Close()
	if err := child.Wait(); err != nil {
		t.Fatalf("helper exit: %v: %s", err, stderr.String())
	}
	if backup != "" {
		if err := os.Remove(backup); err != nil {
			t.Fatalf("backup cleanup after exit: %v", err)
		}
	}
}

func TestExtractRejectsOversizedBinary(t *testing.T) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tr := tar.NewWriter(gz)
	if err := tr.WriteHeader(&tar.Header{Name: "orbit", Mode: 0755, Size: maxBinary + 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	// Only the header is needed: extraction must reject before reading a huge payload.
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := extract(b.Bytes(), "orbit", false); err == nil || !strings.Contains(err.Error(), "size invalid") {
		t.Fatalf("oversized binary: %v", err)
	}
}
