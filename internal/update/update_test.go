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

	publicEncoded, privateEncoded, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	publicKey, err := DecodePublicKey(publicEncoded)
	if err != nil {
		t.Fatal(err)
	}

	privateKey, err := DecodePrivateKey(privateEncoded)
	if err != nil {
		t.Fatal(err)
	}

	return publicKey, privateKey
}

func TestSignVerify(t *testing.T) {
	publicKey, privateKey := testKeys(t)
	data := []byte("hello")
	signature := Sign(privateKey, data)

	if err := Verify(publicKey, data, signature); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	if err := Verify(publicKey, []byte("tampered"), signature); err == nil {
		t.Fatal("tampered data accepted")
	}

	if err := Verify(publicKey, data, []byte("not base64!")); err == nil {
		t.Fatal("malformed signature accepted")
	}

	otherKey, _ := testKeys(t)
	if err := Verify(otherKey, data, signature); err == nil {
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

	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func makeTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzipWriter)

	for _, entry := range []struct {
		name string
		data []byte
	}{{"README.md", []byte("docs")}, {name, content}} {
		if err := tarWriter.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o755, Size: int64(len(entry.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}

		if _, err := tarWriter.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}

	tarWriter.Close()
	gzipWriter.Close()

	return buf.Bytes()
}

func makeZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	for _, entry := range []struct {
		name string
		data []byte
	}{{"LICENSE", []byte("mit")}, {name, content}} {
		fileWriter, err := zipWriter.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}

		fileWriter.Write(entry.data)
	}

	zipWriter.Close()

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
	for _, entry := range entries {
		if entry.Name() != "codexctl" && entry.Name() != "codexctl.old" {
			t.Fatalf("unexpected leftover %s", entry.Name())
		}
	}

	if runtime.GOOS != "windows" {
		info, _ := os.Stat(exe)
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("mode = %o", info.Mode().Perm())
		}
	}
}

func TestClientEndToEnd(t *testing.T) {
	publicKey, privateKey := testKeys(t)
	binary := []byte("#!/bin/sh\necho new\n")

	var archive []byte
	if runtime.GOOS == "windows" {
		archive = makeZip(t, "codexctl.exe", binary)
	} else {
		archive = makeTarGz(t, "codexctl", binary)
	}

	assetName := AssetName("2.0.0")
	sum := sha256.Sum256(archive)
	checksums := []byte(hex.EncodeToString(sum[:]) + "  " + assetName + "\n")

	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest/download/checksums.txt", func(w http.ResponseWriter, _ *http.Request) { w.Write(checksums) })
	mux.HandleFunc("/releases/latest/download/checksums.txt.sig", func(w http.ResponseWriter, _ *http.Request) { w.Write(Sign(privateKey, checksums)) })
	mux.HandleFunc("/releases/latest/download/"+assetName, func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) })
	mux.HandleFunc("/releases/download/v1.0.0/checksums.txt", func(w http.ResponseWriter, _ *http.Request) { w.Write(checksums) })
	mux.HandleFunc("/releases/download/v1.0.0/checksums.txt.sig", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("AAAA")) })

	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{HTTP: server.Client(), UserAgent: "test", PublicKey: publicKey, ReleasesURL: server.URL + "/releases"}

	release, err := client.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if release.Version != "2.0.0" {
		t.Fatalf("version = %q", release.Version)
	}

	var done, total int64
	client.Progress = func(doneBytes, totalBytes int64) { done, total = doneBytes, totalBytes }

	data, err := client.Download(context.Background(), release)
	if err != nil {
		t.Fatal(err)
	}

	if done != int64(len(archive)) || total != int64(len(archive)) {
		t.Fatalf("progress reported %d of %d bytes, want %d", done, total, len(archive))
	}

	got, err := ExtractBinary(data, assetName)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, binary) {
		t.Fatal("extracted binary differs")
	}

	if _, err := client.Version(context.Background(), "1.0.0"); err == nil {
		t.Fatal("bad signature accepted")
	}

	if _, err := client.Version(context.Background(), "3.0.0"); err == nil {
		t.Fatal("missing release accepted")
	}
}
