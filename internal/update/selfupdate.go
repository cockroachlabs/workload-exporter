package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	maxBinarySize   = 100 * 1024 * 1024 // 100 MB guard against runaway downloads
	maxChecksumSize = 1 << 20           // 1 MB, well above any real checksums.txt
)

// UpdateDeps holds the external dependencies for PerformUpdate.
// Tests inject fakes; production uses defaultUpdateDeps().
type UpdateDeps struct {
	// CheckLatest returns the latest release info.
	CheckLatest func(ctx context.Context) (*ReleaseInfo, error)

	// Download fetches the release asset and returns the binary contents.
	Download func(ctx context.Context, version string) ([]byte, error)

	// RunVersion runs "<binary> version" and returns the output.
	RunVersion func(ctx context.Context, binary string) (string, error)

	// CurrentVersion is the version string of the running binary (e.g. "v1.7.1").
	CurrentVersion string

	// BinaryPath is the resolved path to the current binary.
	// If empty, it is auto-detected via os.Executable + EvalSymlinks.
	BinaryPath string
}

func defaultUpdateDeps(version string) UpdateDeps {
	return UpdateDeps{
		CheckLatest: func(ctx context.Context) (*ReleaseInfo, error) {
			return Check(ctx, version)
		},
		Download: func(ctx context.Context, v string) ([]byte, error) {
			return defaultDownload(ctx, http.DefaultClient, v)
		},
		RunVersion:     defaultRunVersion,
		CurrentVersion: version,
	}
}

// assetName returns the expected release asset filename for the current platform.
func assetName(version string) string {
	goos := runtime.GOOS
	arch := runtime.GOARCH
	if goos == "windows" {
		return fmt.Sprintf("workload-exporter-%s-%s-%s.zip", version, goos, arch)
	}
	return fmt.Sprintf("workload-exporter-%s-%s-%s.tar.gz", version, goos, arch)
}

// binaryNameInArchive returns the binary name inside the release archive.
func binaryNameInArchive(version string) string {
	goos := runtime.GOOS
	arch := runtime.GOARCH
	name := fmt.Sprintf("workload-exporter-%s-%s-%s", version, goos, arch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// assetDownloadURL returns the public download URL for a release asset.
func assetDownloadURL(version string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, version, assetName(version))
}

func defaultDownload(ctx context.Context, client HTTPDoer, version string) ([]byte, error) {
	// Download the release archive.
	archiveData, err := fetchURL(ctx, client, assetDownloadURL(version), maxBinarySize)
	if err != nil {
		return nil, fmt.Errorf("downloading asset: %w", err)
	}

	// Download checksums.txt and verify the archive before extracting.
	checksumURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/checksums.txt", repo, version)
	checksumData, err := fetchURL(ctx, client, checksumURL, maxChecksumSize)
	if err != nil {
		return nil, fmt.Errorf("downloading checksums: %w", err)
	}
	if err := verifyChecksum(archiveData, assetName(version), checksumData); err != nil {
		return nil, err
	}

	// Extract binary from archive.
	return extractBinary(archiveData, version)
}

// fetchURL performs a GET request and returns the response body, bounded by limit bytes.
func fetchURL(ctx context.Context, client HTTPDoer, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// verifyChecksum checks the SHA256 of data against the entry for filename in
// a checksums.txt file (sha256sum format: "<hash>  <filename>").
func verifyChecksum(data []byte, filename string, checksumFile []byte) error {
	for _, line := range strings.Split(string(checksumFile), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 || parts[1] != filename {
			continue
		}
		sum := sha256.Sum256(data)
		actual := hex.EncodeToString(sum[:])
		if actual != parts[0] {
			return fmt.Errorf("checksum mismatch for %s: got %s, want %s", filename, actual, parts[0])
		}
		return nil
	}
	return fmt.Errorf("no checksum entry found for %s in checksums.txt", filename)
}

// extractBinary extracts the workload-exporter binary from a .tar.gz or .zip archive.
func extractBinary(data []byte, version string) ([]byte, error) {
	wantName := binaryNameInArchive(version)

	if runtime.GOOS == "windows" {
		return extractFromZip(data, wantName)
	}
	return extractFromTarGz(data, wantName)
}

func extractFromTarGz(data []byte, wantName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("opening gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar: %w", err)
		}
		if hdr.Name == wantName || filepath.Base(hdr.Name) == wantName {
			return io.ReadAll(io.LimitReader(tr, maxBinarySize))
		}
	}
	return nil, fmt.Errorf("binary %s not found in archive", wantName)
}

func extractFromZip(data []byte, wantName string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening zip: %w", err)
	}
	for _, f := range r.File {
		if f.Name == wantName || filepath.Base(f.Name) == wantName {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer func() { _ = rc.Close() }()
			return io.ReadAll(io.LimitReader(rc, maxBinarySize))
		}
	}
	return nil, fmt.Errorf("binary %s not found in archive", wantName)
}

func defaultRunVersion(ctx context.Context, binary string) (string, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(checkCtx, binary, "version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// PerformUpdate downloads the latest release from GitHub, sanity checks the
// new binary, and atomically swaps it into place.
func PerformUpdate(ctx context.Context, w io.Writer, currentVersion string) error {
	return performUpdate(ctx, w, defaultUpdateDeps(currentVersion))
}

func performUpdate(ctx context.Context, w io.Writer, deps UpdateDeps) error {
	// Step 1: Check for latest release.
	_, _ = fmt.Fprintln(w, "checking for latest release...")

	release, err := deps.CheckLatest(ctx)
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}
	if release == nil {
		_, _ = fmt.Fprintln(w, "dev build: cannot auto-update")
		return nil
	}

	if !semverGreater(release.TagName, deps.CurrentVersion) {
		_, _ = fmt.Fprintf(w, "already up to date (%s)\n", deps.CurrentVersion)
		return nil
	}

	_, _ = fmt.Fprintf(w, "new version available: %s -> %s\n", deps.CurrentVersion, release.TagName)

	// Step 2: Find the target binary path.
	binaryPath := deps.BinaryPath
	if binaryPath == "" {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding executable: %w", err)
		}
		binaryPath, err = filepath.EvalSymlinks(exePath)
		if err != nil {
			return fmt.Errorf("resolving symlinks: %w", err)
		}
	}
	targetDir := filepath.Dir(binaryPath)
	stagedPath := filepath.Join(targetDir, "workload-exporter.staged")

	// Step 3: Download the release asset.
	_, _ = fmt.Fprintf(w, "downloading %s...\n", assetName(release.TagName))

	binaryData, err := deps.Download(ctx, release.TagName)
	if err != nil {
		return fmt.Errorf("downloading release: %w", err)
	}

	// Step 4: Write to temp file and sanity check.
	tmpDir, err := os.MkdirTemp("", "workload-exporter-update-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	newBinary := filepath.Join(tmpDir, "workload-exporter")
	if runtime.GOOS == "windows" {
		newBinary += ".exe"
	}
	if err := os.WriteFile(newBinary, binaryData, 0o755); err != nil {
		return fmt.Errorf("writing binary: %w", err)
	}

	newVersion, err := deps.RunVersion(ctx, newBinary)
	if err != nil {
		return fmt.Errorf("sanity check failed: %w", err)
	}
	if !strings.Contains(newVersion, "workload-exporter") {
		return fmt.Errorf("sanity check failed: unexpected version output: %s", newVersion)
	}
	if !strings.Contains(newVersion, release.TagName) {
		return fmt.Errorf("sanity check failed: version output %q does not contain expected %s", newVersion, release.TagName)
	}

	_, _ = fmt.Fprintf(w, "verified: %s\n", newVersion)

	// Step 5: Stage the new binary to the target filesystem for atomic rename.
	if err := copyFile(newBinary, stagedPath); err != nil {
		return fmt.Errorf("staging binary: %w", err)
	}
	defer func() {
		// Clean up staged file on failure (after swap it no longer exists).
		_ = os.Remove(stagedPath)
	}()

	// Step 6: Atomic swap.
	if err := os.Rename(stagedPath, binaryPath); err != nil {
		return fmt.Errorf("swapping binary: %w", err)
	}

	_, _ = fmt.Fprintln(w, "update complete")
	return nil
}

// copyFile copies src to dst, preserving the executable permission bit.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
