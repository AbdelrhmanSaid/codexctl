package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func testKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pubEnc, privEnc, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := DecodePublicKey(pubEnc)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := DecodePrivateKey(privEnc)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestSignVerify(t *testing.T) {
	pub, priv := testKeys(t)
	data := []byte("hello")
	sig := Sign(priv, data)
	if err := Verify(pub, data, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := Verify(pub, []byte("tampered"), sig); err == nil {
		t.Fatal("tampered data accepted")
	}
	if err := Verify(pub, data, []byte("not base64!")); err == nil {
		t.Fatal("malformed signature accepted")
	}
	other, _ := testKeys(t)
	if err := Verify(other, data, sig); err == nil {
		t.Fatal("signature accepted with the wrong key")
	}
}

func TestEmbeddedPublicKeyIsValid(t *testing.T) {
	if _, err := PublicKey(); err != nil {
		t.Fatal(err)
	}
}

func TestParseChecksums(t *testing.T) {
	sum := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	data := "# comment\n" + sum + "  codexctl_1.2.3_linux_amd64.tar.gz\n" +
		sum + "  codexctl-1.2.3-1-x86_64.pkg.tar.zst\nshort  codexctl_9.9.9_linux_arm64.tar.gz\n"
	release, err := parseChecksums([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "1.2.3" {
		t.Fatalf("version = %q", release.Version)
	}
	if len(release.Checksums) != 2 {
		t.Fatalf("checksums = %v", release.Checksums)
	}
	if _, err := parseChecksums([]byte("nothing here\n")); err == nil {
		t.Fatal("expected error for empty checksums")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.10.0", "1.9.0", 1},
		{"0.9.0", "1.0.0", -1},
		{"1.0.0-rc1", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc1", 1},
		{"1.0.0-rc1", "1.0.0-rc2", -1},
		{"1.0", "1.0.0", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func makeTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		data []byte
	}{{"README.md", []byte("docs")}, {name, content}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func makeZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct {
		name string
		data []byte
	}{{"LICENSE", []byte("mit")}, {name, content}} {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(f.data)
	}
	zw.Close()
	return buf.Bytes()
}

func TestExtractBinary(t *testing.T) {
	want := []byte("binary bytes")
	got, err := ExtractBinary(makeTarGz(t, "codexctl", want), "x.tar.gz")
	if runtime.GOOS == "windows" {
		got, err = ExtractBinary(makeZip(t, "codexctl.exe", want), "x.zip")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("extracted %q", got)
	}
	if _, err := ExtractBinary(makeTarGz(t, "other", want), "x.tar.gz"); err == nil && runtime.GOOS != "windows" {
		t.Fatal("expected missing binary error")
	}
}

func TestApply(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "codexctl")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Apply(exe, []byte("new")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("executable = %q", data)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "codexctl" && e.Name() != "codexctl.old" {
			t.Fatalf("unexpected leftover %s", e.Name())
		}
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(exe)
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("mode = %o", info.Mode().Perm())
		}
	}
}

// TestClientEndToEnd serves a fake release and walks the full path: signed
// checksums, version discovery, download, checksum verification, extraction.
func TestClientEndToEnd(t *testing.T) {
	pub, priv := testKeys(t)
	binary := []byte("#!/bin/sh\necho new\n")
	var archive []byte
	if runtime.GOOS == "windows" {
		archive = makeZip(t, "codexctl.exe", binary)
	} else {
		archive = makeTarGz(t, "codexctl", binary)
	}
	name := AssetName("2.0.0")
	sum := sha256.Sum256(archive)
	checksums := []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest/download/checksums.txt", func(w http.ResponseWriter, _ *http.Request) { w.Write(checksums) })
	mux.HandleFunc("/releases/latest/download/checksums.txt.sig", func(w http.ResponseWriter, _ *http.Request) { w.Write(Sign(priv, checksums)) })
	mux.HandleFunc("/releases/latest/download/"+name, func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) })
	mux.HandleFunc("/releases/download/v1.0.0/checksums.txt", func(w http.ResponseWriter, _ *http.Request) { w.Write(checksums) })
	mux.HandleFunc("/releases/download/v1.0.0/checksums.txt.sig", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("AAAA")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), UserAgent: "test", PublicKey: pub, ReleasesURL: srv.URL + "/releases"}
	release, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "2.0.0" {
		t.Fatalf("version = %q", release.Version)
	}
	data, err := c.Download(context.Background(), release)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExtractBinary(data, name)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatal("extracted binary differs")
	}
	if _, err := c.Version(context.Background(), "1.0.0"); err == nil {
		t.Fatal("bad signature accepted")
	}
	if _, err := c.Version(context.Background(), "3.0.0"); err == nil {
		t.Fatal("missing release accepted")
	}
}
