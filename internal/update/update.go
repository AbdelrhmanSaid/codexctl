// Package update fetches signed codexctl releases from GitHub and replaces
// the running executable.
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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const (
	Repo        = "AbdelrhmanSaid/codexctl"
	releasesURL = "https://github.com/" + Repo + "/releases"
	// maxDownload bounds every response body; release assets are a few MB.
	maxDownload = 64 << 20
)

// Release is one published version and the checksums of its assets.
type Release struct {
	Version   string            // without the leading "v"
	Checksums map[string]string // asset file name -> hex SHA-256
	baseURL   string            // directory the assets are downloaded from
}

type Client struct {
	HTTP      *http.Client
	UserAgent string
	PublicKey ed25519.PublicKey
	// ReleasesURL is the GitHub releases page; tests point it elsewhere.
	ReleasesURL string
}

func NewClient(currentVersion string) (*Client, error) {
	key, err := PublicKey()
	if err != nil {
		return nil, err
	}
	return &Client{
		HTTP:        &http.Client{Timeout: 2 * time.Minute},
		UserAgent:   "codexctl/" + currentVersion,
		PublicKey:   key,
		ReleasesURL: releasesURL,
	}, nil
}

// Latest resolves the newest non-prerelease. GitHub redirects the
// latest/download path, so no API call or rate limit is involved.
func (c *Client) Latest(ctx context.Context) (*Release, error) {
	return c.release(ctx, c.ReleasesURL+"/latest/download")
}

// Version resolves one specific release.
func (c *Client) Version(ctx context.Context, version string) (*Release, error) {
	return c.release(ctx, c.ReleasesURL+"/download/v"+strings.TrimPrefix(version, "v"))
}

func (c *Client) release(ctx context.Context, baseURL string) (*Release, error) {
	checksums, err := c.get(ctx, baseURL+"/checksums.txt")
	if err != nil {
		return nil, err
	}
	signature, err := c.get(ctx, baseURL+"/checksums.txt.sig")
	if err != nil {
		return nil, err
	}
	if err := Verify(c.PublicKey, checksums, signature); err != nil {
		return nil, fmt.Errorf("refusing release: %w", err)
	}
	release, err := parseChecksums(checksums)
	if err != nil {
		return nil, err
	}
	release.baseURL = baseURL
	return release, nil
}

// Download fetches this platform's archive and verifies its checksum.
func (c *Client) Download(ctx context.Context, release *Release) ([]byte, error) {
	name := AssetName(release.Version)
	want, ok := release.Checksums[name]
	if !ok {
		return nil, fmt.Errorf("release %s has no asset for %s/%s", release.Version, runtime.GOOS, runtime.GOARCH)
	}
	data, err := c.get(ctx, release.baseURL+"/"+name)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("checksum mismatch for %s", name)
	}
	return data, nil
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", path.Base(url), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s was not found; the release may not exist or may predate signed updates", path.Base(url))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: unexpected status %s", path.Base(url), resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", path.Base(url), err)
	}
	if len(data) > maxDownload {
		return nil, fmt.Errorf("download %s: response is larger than %d bytes", path.Base(url), maxDownload)
	}
	return data, nil
}

// parseChecksums reads GoReleaser's checksums.txt. The version is taken from
// the asset names, which GoReleaser formats as codexctl_VERSION_OS_ARCH.EXT.
func parseChecksums(data []byte) (*Release, error) {
	release := &Release{Checksums: map[string]string{}}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		sum, name := fields[0], fields[1]
		if len(sum) != sha256.Size*2 {
			continue
		}
		if _, err := hex.DecodeString(sum); err != nil {
			continue
		}
		release.Checksums[name] = sum
		if release.Version == "" && strings.HasPrefix(name, "codexctl_") {
			if parts := strings.Split(name, "_"); len(parts) >= 3 {
				release.Version = parts[1]
			}
		}
	}
	if release.Version == "" {
		return nil, errors.New("checksums.txt lists no codexctl assets")
	}
	return release, nil
}

// AssetName is the archive GoReleaser publishes for this platform.
func AssetName(version string) string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	return "codexctl_" + version + "_" + runtime.GOOS + "_" + runtime.GOARCH + ext
}

// ExtractBinary returns the codexctl executable stored in a release archive.
func ExtractBinary(archive []byte, name string) ([]byte, error) {
	if strings.HasSuffix(name, ".zip") {
		return extractZip(archive)
	}
	return extractTarGz(archive)
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "codexctl.exe"
	}
	return "codexctl"
}

func extractTarGz(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if header.Typeflag == tar.TypeReg && path.Base(header.Name) == binaryName() {
			return readLimited(tr)
		}
	}
	return nil, fmt.Errorf("archive does not contain %s", binaryName())
}

func extractZip(archive []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	for _, file := range zr.File {
		if file.Mode().IsRegular() && path.Base(file.Name) == binaryName() {
			rc, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return readLimited(rc)
		}
	}
	return nil, fmt.Errorf("archive does not contain %s", binaryName())
}

func readLimited(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxDownload+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDownload {
		return nil, errors.New("extracted binary is unexpectedly large")
	}
	return data, nil
}

// Executable returns the real path of the running binary, following any
// symlink such as the one a manual install into ~/bin might use.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// Apply replaces the executable at exe with binary. The new file is written
// next to the old one and renamed over it, so the swap is atomic on POSIX.
// Windows refuses to overwrite a running executable but allows renaming it,
// so the old file is moved aside first and deleted on a later update.
func Apply(exe string, binary []byte) error {
	dir := filepath.Dir(exe)
	temp, err := os.CreateTemp(dir, ".codexctl-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(binary); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempName, 0o755); err != nil && runtime.GOOS != "windows" {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Rename(tempName, exe)
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("move current executable aside: %w", err)
	}
	if err := os.Rename(tempName, exe); err != nil {
		_ = os.Rename(old, exe)
		return fmt.Errorf("install new executable: %w", err)
	}
	// The running image keeps the old file locked; the next update removes it.
	_ = os.Remove(old)
	return nil
}

// Method describes how the running binary was installed.
type Method int

const (
	MethodRelease Method = iota // a GoReleaser build placed on PATH by hand
	MethodGoInstall
	MethodPackage // dpkg, rpm, apk or pacman
	MethodDev     // go build in a checkout
)

// DetectInstall classifies the running binary. releaseBuild reports whether
// GoReleaser stamped a version into it.
func DetectInstall(releaseBuild bool, exe string) Method {
	if !releaseBuild {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			return MethodGoInstall
		}
		return MethodDev
	}
	if runtime.GOOS == "linux" || runtime.GOOS == "freebsd" {
		for _, dir := range []string{"/usr/bin/", "/usr/lib/", "/usr/sbin/"} {
			if strings.HasPrefix(exe, dir) {
				return MethodPackage
			}
		}
	}
	return MethodRelease
}

// CompareVersions orders two semantic versions, ignoring a leading "v". A
// prerelease sorts before the release it precedes.
func CompareVersions(a, b string) int {
	coreA, preA, _ := strings.Cut(strings.TrimPrefix(a, "v"), "-")
	coreB, preB, _ := strings.Cut(strings.TrimPrefix(b, "v"), "-")
	partsA, partsB := strings.Split(coreA, "."), strings.Split(coreB, ".")
	for i := 0; i < max(len(partsA), len(partsB)); i++ {
		na, nb := 0, 0
		if i < len(partsA) {
			na, _ = strconv.Atoi(partsA[i])
		}
		if i < len(partsB) {
			nb, _ = strconv.Atoi(partsB[i])
		}
		if na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
	}
	switch {
	case preA == preB:
		return 0
	case preA == "":
		return 1
	case preB == "":
		return -1
	}
	return strings.Compare(preA, preB)
}
