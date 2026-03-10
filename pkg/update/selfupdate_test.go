package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDeps returns an UpdateDeps with all fields set for a successful update.
func fakeDeps(t *testing.T, binaryPath string) UpdateDeps {
	t.Helper()
	return UpdateDeps{
		CheckLatest: func(_ context.Context) (*ReleaseInfo, error) {
			return &ReleaseInfo{TagName: "v2.0.0", HTMLURL: "https://github.com/" + repo + "/releases/tag/v2.0.0"}, nil
		},
		Download: func(_ context.Context, version string) ([]byte, error) {
			return []byte("new-binary"), nil
		},
		RunVersion: func(_ context.Context, _ string) (string, error) {
			return "workload-exporter version v2.0.0\nCommit: abc1234\nBuilt:  2025-07-02T08:30:00Z", nil
		},
		CurrentVersion: "v1.7.1",
		BinaryPath:     binaryPath,
	}
}

// setupBinary creates a temp dir with a fake "workload-exporter" binary and
// returns the path to the binary.
func setupBinary(t *testing.T) (dir, binaryPath string) {
	t.Helper()
	dir = t.TempDir()
	binaryPath = filepath.Join(dir, "workload-exporter")
	if err := os.WriteFile(binaryPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, binaryPath
}

func TestPerformUpdate_HappyPath(t *testing.T) {
	_, binaryPath := setupBinary(t)
	deps := fakeDeps(t, binaryPath)

	var buf bytes.Buffer
	err := performUpdate(context.Background(), &buf, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		"downloading",
		"verified: workload-exporter version v2.0.0",
		"update complete",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, output)
		}
	}

	// Verify the binary was replaced.
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new-binary" {
		t.Errorf("binary contents = %q, want %q", data, "new-binary")
	}
}

func TestPerformUpdate_AlreadyUpToDate(t *testing.T) {
	_, binaryPath := setupBinary(t)
	deps := fakeDeps(t, binaryPath)
	deps.CheckLatest = func(_ context.Context) (*ReleaseInfo, error) {
		return &ReleaseInfo{TagName: "v1.7.1"}, nil
	}

	var buf bytes.Buffer
	err := performUpdate(context.Background(), &buf, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "already up to date") {
		t.Errorf("expected 'already up to date' in output:\n%s", output)
	}

	// Verify the binary was NOT replaced.
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old-binary" {
		t.Errorf("binary should not have been replaced, got %q", data)
	}
}

func TestPerformUpdate_DownloadFails(t *testing.T) {
	_, binaryPath := setupBinary(t)
	deps := fakeDeps(t, binaryPath)
	deps.Download = func(_ context.Context, _ string) ([]byte, error) {
		return nil, fmt.Errorf("network error")
	}

	var buf bytes.Buffer
	err := performUpdate(context.Background(), &buf, deps)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "network error") {
		t.Errorf("error = %q, want it to contain 'network error'", err)
	}

	// Binary should be untouched.
	data, _ := os.ReadFile(binaryPath)
	if string(data) != "old-binary" {
		t.Errorf("binary should not have been replaced after download failure")
	}
}

func TestPerformUpdate_SanityCheckFails(t *testing.T) {
	_, binaryPath := setupBinary(t)
	deps := fakeDeps(t, binaryPath)
	deps.RunVersion = func(_ context.Context, _ string) (string, error) {
		return "", fmt.Errorf("exit status 1")
	}

	var buf bytes.Buffer
	err := performUpdate(context.Background(), &buf, deps)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sanity check failed") {
		t.Errorf("error = %q, want it to contain 'sanity check failed'", err)
	}

	// Binary should be untouched.
	data, _ := os.ReadFile(binaryPath)
	if string(data) != "old-binary" {
		t.Errorf("binary should not have been replaced after sanity check failure")
	}
}

func TestPerformUpdate_BadVersionOutput(t *testing.T) {
	_, binaryPath := setupBinary(t)
	deps := fakeDeps(t, binaryPath)
	deps.RunVersion = func(_ context.Context, _ string) (string, error) {
		return "some garbage output", nil
	}

	var buf bytes.Buffer
	err := performUpdate(context.Background(), &buf, deps)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected version output") {
		t.Errorf("error = %q, want it to contain 'unexpected version output'", err)
	}
}

func TestPerformUpdate_VersionMismatch(t *testing.T) {
	_, binaryPath := setupBinary(t)
	deps := fakeDeps(t, binaryPath)
	// RunVersion returns the wrong version tag.
	deps.RunVersion = func(_ context.Context, _ string) (string, error) {
		return "workload-exporter version v1.0.0", nil
	}

	var buf bytes.Buffer
	err := performUpdate(context.Background(), &buf, deps)
	if err == nil {
		t.Fatal("expected error for version mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "sanity check failed") {
		t.Errorf("error = %q, want it to contain 'sanity check failed'", err)
	}
}

func TestPerformUpdate_Downgrade(t *testing.T) {
	_, binaryPath := setupBinary(t)
	deps := fakeDeps(t, binaryPath)
	// Simulate a case where the installed binary is already newer than "latest".
	deps.CurrentVersion = "v3.0.0"
	deps.CheckLatest = func(_ context.Context) (*ReleaseInfo, error) {
		return &ReleaseInfo{TagName: "v2.0.0"}, nil
	}

	var buf bytes.Buffer
	err := performUpdate(context.Background(), &buf, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "already up to date") {
		t.Errorf("expected 'already up to date' for downgrade scenario:\n%s", buf.String())
	}

	// Binary should be untouched.
	data, _ := os.ReadFile(binaryPath)
	if string(data) != "old-binary" {
		t.Error("binary should not have been replaced in downgrade scenario")
	}
}

// --- verifyChecksum tests ---

func checksumLine(data []byte, filename string) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) + "  " + filename + "\n"
}

func TestVerifyChecksum_Valid(t *testing.T) {
	data := []byte("binary content")
	checksumFile := checksumLine(data, "myfile.tar.gz")

	if err := verifyChecksum(data, "myfile.tar.gz", []byte(checksumFile)); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifyChecksum_ValidAmongMultipleEntries(t *testing.T) {
	data := []byte("binary content")
	checksumFile := "aaaa  other-file.zip\n" + checksumLine(data, "myfile.tar.gz") + "bbbb  another.tar.gz\n"

	if err := verifyChecksum(data, "myfile.tar.gz", []byte(checksumFile)); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	data := []byte("binary content")
	wrong := strings.Repeat("a", 64) + "  myfile.tar.gz\n"

	err := verifyChecksum(data, "myfile.tar.gz", []byte(wrong))
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error %q should mention 'checksum mismatch'", err)
	}
}

func TestVerifyChecksum_NotFound(t *testing.T) {
	checksumFile := "abc123  other-file.tar.gz\n"

	err := verifyChecksum([]byte("data"), "myfile.tar.gz", []byte(checksumFile))
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), "no checksum entry") {
		t.Errorf("error %q should mention 'no checksum entry'", err)
	}
}

func TestVerifyChecksum_EmptyFile(t *testing.T) {
	err := verifyChecksum([]byte("data"), "myfile.tar.gz", []byte(""))
	if err == nil {
		t.Fatal("expected error for empty checksums file")
	}
}

func TestPerformUpdate_StagedFileCleanedUp(t *testing.T) {
	_, binaryPath := setupBinary(t)
	deps := fakeDeps(t, binaryPath)

	var buf bytes.Buffer
	err := performUpdate(context.Background(), &buf, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After successful update, workload-exporter.staged should not exist.
	stagedPath := filepath.Join(filepath.Dir(binaryPath), "workload-exporter.staged")
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Errorf("staged file should not exist after successful update")
	}
}
