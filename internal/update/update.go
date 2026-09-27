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
	// Bounds every response body.
	maxDownload = 64 << 20
)

type Release struct {
	Version   string            // without the leading "v"
	Checksums map[string]string // asset file name -> hex SHA-256
	baseURL   string
}

type Client struct {
	HTTP      *http.Client
	UserAgent string
	PublicKey ed25519.PublicKey
	// ReleasesURL is pointed elsewhere by tests.
	ReleasesURL string
	// Progress gets a total of -1 when it is unknown.
	Progress func(done, total int64)
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

// Latest follows GitHub's latest/download redirect, so no API call is
// involved.
func (c *Client) Latest(ctx context.Context) (*Release, error) {
	return c.release(ctx, c.ReleasesURL+"/latest/download")
}

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

func (c *Client) Download(ctx context.Context, release *Release) ([]byte, error) {
	assetName := AssetName(release.Version)

	wantSum, ok := release.Checksums[assetName]
	if !ok {
		return nil, fmt.Errorf("release %s has no asset for %s/%s", release.Version, runtime.GOOS, runtime.GOARCH)
	}

	data, err := c.get(ctx, release.baseURL+"/"+assetName)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(data)
	if gotSum := hex.EncodeToString(sum[:]); gotSum != wantSum {
		return nil, fmt.Errorf("checksum mismatch for %s", assetName)
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

	var body io.Reader = resp.Body
	if c.Progress != nil {
		body = &progressReader{r: body, total: resp.ContentLength, report: c.Progress}
	}

	data, err := io.ReadAll(io.LimitReader(body, maxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", path.Base(url), err)
	}

	if len(data) > maxDownload {
		return nil, fmt.Errorf("download %s: response is larger than %d bytes", path.Base(url), maxDownload)
	}

	return data, nil
}

type progressReader struct {
	r      io.Reader
	done   int64
	total  int64
	report func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	p.report(p.done, p.total)

	return n, err
}

// The version comes from asset names: codexctl_VERSION_OS_ARCH.EXT.
func parseChecksums(data []byte) (*Release, error) {
	release := &Release{Checksums: map[string]string{}}

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		sum, assetName := fields[0], fields[1]
		if len(sum) != sha256.Size*2 {
			continue
		}

		if _, err := hex.DecodeString(sum); err != nil {
			continue
		}

		release.Checksums[assetName] = sum

		if release.Version == "" && strings.HasPrefix(assetName, "codexctl_") {
			if parts := strings.Split(assetName, "_"); len(parts) >= 3 {
				release.Version = parts[1]
			}
		}
	}

	if release.Version == "" {
		return nil, errors.New("checksums.txt lists no codexctl assets")
	}

	return release, nil
}

func AssetName(version string) string {
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}

	return "codexctl_" + version + "_" + runtime.GOOS + "_" + runtime.GOARCH + ext
}

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
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}

	tarReader := tar.NewReader(gzipReader)

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}

		if header.Typeflag == tar.TypeReg && path.Base(header.Name) == binaryName() {
			return readLimited(tarReader)
		}
	}

	return nil, fmt.Errorf("archive does not contain %s", binaryName())
}

func extractZip(archive []byte) ([]byte, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}

	for _, file := range zipReader.File {
		if file.Mode().IsRegular() && path.Base(file.Name) == binaryName() {
			entry, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer entry.Close()

			return readLimited(entry)
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

// Executable follows symlinks, such as one from a manual install into ~/bin.
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

// Apply renames the new file into place, which is atomic on POSIX. On Windows
// the old file is moved aside first.
func Apply(exe string, binary []byte) error {
	dir := filepath.Dir(exe)

	tempFile, err := os.CreateTemp(dir, ".codexctl-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}

	tempName := tempFile.Name()
	defer os.Remove(tempName)

	if _, err := tempFile.Write(binary); err != nil {
		tempFile.Close()
		return err
	}

	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return err
	}

	if err := tempFile.Close(); err != nil {
		return err
	}

	if err := os.Chmod(tempName, 0o755); err != nil && runtime.GOOS != "windows" {
		return err
	}

	if runtime.GOOS != "windows" {
		return os.Rename(tempName, exe)
	}

	oldPath := exe + ".old"
	_ = os.Remove(oldPath)

	if err := os.Rename(exe, oldPath); err != nil {
		return fmt.Errorf("move current executable aside: %w", err)
	}

	if err := os.Rename(tempName, exe); err != nil {
		_ = os.Rename(oldPath, exe)
		return fmt.Errorf("install new executable: %w", err)
	}

	// The running image keeps the old file locked; the next update removes it.
	_ = os.Remove(oldPath)

	return nil
}

type Method int

const (
	MethodRelease Method = iota // a GoReleaser build placed on PATH by hand
	MethodGoInstall
	MethodPackage // dpkg, rpm, apk or pacman
	MethodDev     // go build in a checkout
)

// DetectInstall takes whether GoReleaser stamped a version into the binary.
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

// CompareVersions sorts a prerelease before its release.
func CompareVersions(a, b string) int {
	coreA, preA, _ := strings.Cut(strings.TrimPrefix(a, "v"), "-")
	coreB, preB, _ := strings.Cut(strings.TrimPrefix(b, "v"), "-")
	partsA, partsB := strings.Split(coreA, "."), strings.Split(coreB, ".")

	for i := 0; i < max(len(partsA), len(partsB)); i++ {
		numA, numB := 0, 0

		if i < len(partsA) {
			numA, _ = strconv.Atoi(partsA[i])
		}

		if i < len(partsB) {
			numB, _ = strconv.Atoi(partsB[i])
		}

		if numA != numB {
			if numA < numB {
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
