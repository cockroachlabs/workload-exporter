package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

var fixedTime = time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC)

const testVersion = "v1.7.1"

func testChecker(t *testing.T, handler http.Handler, opts ...func(*Deps)) *Checker {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	deps := Deps{
		HTTPClient: srv.Client(),
		CacheDir:   t.TempDir(),
		Now:        func() time.Time { return fixedTime },
		Version:    testVersion,
		BaseURL:    srv.URL,
	}
	for _, opt := range opts {
		opt(&deps)
	}
	return NewChecker(deps)
}

func releaseJSON(tag string) []byte {
	resp := ReleaseInfo{
		TagName: tag,
		HTMLURL: "https://github.com/" + repo + "/releases/tag/" + tag,
	}
	data, _ := json.Marshal(resp)
	return data
}

// --- Check tests ---

func TestCheck_DevBuild(t *testing.T) {
	called := false
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}), func(d *Deps) { d.Version = "dev" })

	result, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for dev build, got %+v", result)
	}
	if called {
		t.Error("HTTP request was made for dev build")
	}
}

func TestCheck_EmptyVersion(t *testing.T) {
	called := false
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}), func(d *Deps) { d.Version = "" })

	result, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for empty version, got %+v", result)
	}
	if called {
		t.Error("HTTP request was made for empty version")
	}
}

func TestCheck_UpToDate(t *testing.T) {
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(releaseJSON(testVersion))
	}))

	result, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TagName != testVersion {
		t.Errorf("TagName = %q, want %q", result.TagName, testVersion)
	}
}

func TestCheck_NewerRelease(t *testing.T) {
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(releaseJSON("v2.0.0"))
	}))

	result, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TagName != "v2.0.0" {
		t.Errorf("TagName = %q, want %q", result.TagName, "v2.0.0")
	}
}

func TestCheck_APIError(t *testing.T) {
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	_, err := c.Check(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if want := "500"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err, want)
	}
}

func TestCheck_MalformedJSON(t *testing.T) {
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))

	_, err := c.Check(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestCheck_WritesCache(t *testing.T) {
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(releaseJSON("v2.0.0"))
	}))

	_, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(c.cachePath())
	if err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	var cached cache
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatalf("cache unmarshal error: %v", err)
	}
	if cached.LatestTag != "v2.0.0" {
		t.Errorf("cached LatestTag = %q, want %q", cached.LatestTag, "v2.0.0")
	}
	if cached.BuildVersion != testVersion {
		t.Errorf("cached BuildVersion = %q, want %q", cached.BuildVersion, testVersion)
	}
}

// --- NotifyIfNeeded tests ---

func TestNotify_DevBuild(t *testing.T) {
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("HTTP request should not be made for dev build")
	}), func(d *Deps) { d.Version = "dev" })

	if msg := c.NotifyIfNeeded(context.Background()); msg != "" {
		t.Errorf("got %q, want empty", msg)
	}
}

func TestNotify_FreshCacheUpToDate(t *testing.T) {
	called := false
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write(releaseJSON(testVersion))
	}))

	// Seed cache: up to date, recent check, matching version.
	seedCache(t, c, testVersion, fixedTime.Add(-1*time.Hour))

	if msg := c.NotifyIfNeeded(context.Background()); msg != "" {
		t.Errorf("got %q, want empty", msg)
	}
	if called {
		t.Error("HTTP request was made despite fresh cache")
	}
}

func TestNotify_FreshCacheUpdateAvailable(t *testing.T) {
	called := false
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	seedCache(t, c, "v2.0.0", fixedTime.Add(-1*time.Hour))

	msg := c.NotifyIfNeeded(context.Background())
	if msg == "" {
		t.Error("expected notification, got empty")
	}
	if want := "v2.0.0"; !strings.Contains(msg, want) {
		t.Errorf("msg %q does not contain %q", msg, want)
	}
	if called {
		t.Error("HTTP request was made despite fresh cache")
	}
}

func TestNotify_StaleCache(t *testing.T) {
	called := false
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write(releaseJSON("v2.0.0"))
	}))

	// Seed cache with LastCheck >24h ago.
	seedCache(t, c, testVersion, fixedTime.Add(-25*time.Hour))

	msg := c.NotifyIfNeeded(context.Background())
	if !called {
		t.Error("HTTP request should be made for stale cache")
	}
	if want := "v2.0.0"; !strings.Contains(msg, want) {
		t.Errorf("msg %q does not contain %q", msg, want)
	}
}

func TestNotify_DifferentBuildVersion(t *testing.T) {
	called := false
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write(releaseJSON(testVersion))
	}))

	// Seed cache with a different BuildVersion.
	data, _ := json.Marshal(cache{
		LastCheck:    fixedTime.Add(-1 * time.Hour).UTC().Format(time.RFC3339),
		BuildVersion: "v1.0.0",
		LatestTag:    testVersion,
	})
	_ = os.WriteFile(c.cachePath(), data, 0o644)

	c.NotifyIfNeeded(context.Background())
	if !called {
		t.Error("HTTP request should be made when build version differs")
	}
}

func TestNotify_NoCache(t *testing.T) {
	called := false
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write(releaseJSON(testVersion))
	}))

	c.NotifyIfNeeded(context.Background())
	if !called {
		t.Error("HTTP request should be made when no cache exists")
	}
}

func TestNotify_APIErrorSwallowed(t *testing.T) {
	c := testChecker(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	msg := c.NotifyIfNeeded(context.Background())
	if msg != "" {
		t.Errorf("expected empty string on API error, got %q", msg)
	}
}

// --- semver tests ---

func TestSemverGreater(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		// Major version bumps.
		{"v2.0.0", "v1.7.1", true},
		{"v1.7.1", "v2.0.0", false},
		// Minor version bumps.
		{"v1.8.0", "v1.7.1", true},
		{"v1.7.1", "v1.8.0", false},
		// Patch version bumps.
		{"v1.7.2", "v1.7.1", true},
		{"v1.7.1", "v1.7.2", false},
		// Equality.
		{"v1.7.1", "v1.7.1", false},
		// Pre-release: release > pre-release of same version.
		{"v2.0.0", "v2.0.0-rc1", true},
		{"v2.0.0-rc1", "v2.0.0", false},
		// Pre-release: newer pre-release still beats older release.
		{"v2.0.0-rc1", "v1.7.1", true},
		// Downgrade: older is not greater.
		{"v1.0.0", "v2.0.0", false},
		// Invalid versions: always false.
		{"dev", "v1.7.1", false},
		{"v1.7.1", "dev", false},
		{"", "", false},
		{"not-a-version", "also-not", false},
	}
	for _, tt := range tests {
		got := semverGreater(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("semverGreater(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// --- helpers ---

func seedCache(t *testing.T, c *Checker, latestTag string, lastCheck time.Time) {
	t.Helper()
	data, err := json.Marshal(cache{
		LastCheck:    lastCheck.UTC().Format(time.RFC3339),
		BuildVersion: c.deps.Version,
		LatestTag:    latestTag,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.deps.CacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.cachePath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
