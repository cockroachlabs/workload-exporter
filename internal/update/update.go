package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	repo               = "cockroachlabs/workload-exporter"
	cacheFile          = "update-check.json"
	checkInterval      = 24 * time.Hour
	maxAPIResponseSize = 1 << 20 // 1 MB, well above any real GitHub API response
)

// ReleaseInfo holds information about the latest GitHub release.
type ReleaseInfo struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// cache is the on-disk update check cache.
type cache struct {
	LastCheck    string `json:"last_check"`
	BuildVersion string `json:"build_version"`
	LatestTag    string `json:"latest_tag"`
}

// HTTPDoer is the interface for making HTTP requests.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Deps holds the external dependencies for update checking.
type Deps struct {
	HTTPClient HTTPDoer
	CacheDir   string // directory for update-check.json
	Now        func() time.Time
	Version    string // current build version (e.g. "v1.7.1", or "dev")
	BaseURL    string // GitHub API base, default "https://api.github.com"
}

// Checker performs update checks using injected dependencies.
type Checker struct {
	deps Deps
}

// NewChecker creates a Checker with the given dependencies.
func NewChecker(deps Deps) *Checker {
	return &Checker{deps: deps}
}

func defaultChecker(version string) *Checker {
	home, _ := os.UserHomeDir()
	return NewChecker(Deps{
		HTTPClient: http.DefaultClient,
		CacheDir:   filepath.Join(home, ".workload-exporter"),
		Now:        time.Now,
		Version:    version,
		BaseURL:    "https://api.github.com",
	})
}

// Check calls the GitHub releases API to determine if a newer version is
// available. Returns nil, nil for dev builds.
func Check(ctx context.Context, version string) (*ReleaseInfo, error) {
	return defaultChecker(version).Check(ctx)
}

// NotifyIfNeeded is a rate-limited wrapper around Check. It returns a
// notification string if a newer release is available and the last check was
// more than 24 hours ago (or the build version changed). Returns "" if up to
// date or if the check was performed recently. Swallows all errors.
func NotifyIfNeeded(ctx context.Context, version string) string {
	return defaultChecker(version).NotifyIfNeeded(ctx)
}

func (c *Checker) cachePath() string {
	return filepath.Join(c.deps.CacheDir, cacheFile)
}

func (c *Checker) readCache() (*cache, error) {
	data, err := os.ReadFile(c.cachePath())
	if err != nil {
		return nil, err
	}
	var cached cache
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, err
	}
	return &cached, nil
}

func (c *Checker) writeCache(cached *cache) error {
	if err := os.MkdirAll(c.deps.CacheDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.cachePath(), data, 0o644)
}

// Check calls the GitHub releases API to determine if a newer version is
// available. Returns nil, nil for dev builds.
func (c *Checker) Check(ctx context.Context) (*ReleaseInfo, error) {
	if c.deps.Version == "" || c.deps.Version == "dev" {
		return nil, nil
	}

	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.deps.BaseURL, repo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.deps.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release ReleaseInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIResponseSize)).Decode(&release); err != nil {
		return nil, fmt.Errorf("decoding GitHub response: %w", err)
	}

	// Update cache (best-effort).
	_ = c.writeCache(&cache{
		LastCheck:    c.deps.Now().UTC().Format(time.RFC3339),
		BuildVersion: c.deps.Version,
		LatestTag:    release.TagName,
	})

	return &release, nil
}

// NotifyIfNeeded is a rate-limited wrapper around Check.
func (c *Checker) NotifyIfNeeded(ctx context.Context) string {
	if c.deps.Version == "" || c.deps.Version == "dev" {
		return ""
	}

	// Check cache to see if we need to make an API call.
	if cached, err := c.readCache(); err == nil {
		// Cache is valid if build version matches and check is recent.
		if cached.BuildVersion == c.deps.Version {
			lastCheck, err := time.Parse(time.RFC3339, cached.LastCheck)
			if err == nil && c.deps.Now().Sub(lastCheck) < checkInterval {
				// Return cached result without making an API call.
				if cached.LatestTag != "" && semverGreater(cached.LatestTag, c.deps.Version) {
					return fmt.Sprintf("workload-exporter: update available (%s -> %s). Run 'workload-exporter update' to install.", c.deps.Version, cached.LatestTag)
				}
				return ""
			}
		}
	}

	result, err := c.Check(ctx)
	if err != nil || result == nil {
		return ""
	}
	if semverGreater(result.TagName, c.deps.Version) {
		return fmt.Sprintf("workload-exporter: update available (%s -> %s). Run 'workload-exporter update' to install.", c.deps.Version, result.TagName)
	}
	return ""
}

// IsNewer reports whether version a is strictly newer than version b using
// semver ordering (vMAJOR.MINOR.PATCH with optional pre-release suffix).
// Returns false if either value is not a valid semver string.
func IsNewer(a, b string) bool {
	return semverGreater(a, b)
}

// semverGreater reports whether a is strictly greater than b.
func semverGreater(a, b string) bool {
	sa, oka := parseSemver(a)
	sb, okb := parseSemver(b)
	if !oka || !okb {
		return false
	}
	for i := range sa.core {
		if sa.core[i] != sb.core[i] {
			return sa.core[i] > sb.core[i]
		}
	}
	// Equal core versions: release > pre-release.
	return sb.pre != "" && sa.pre == ""
}

type semver struct {
	core [3]int
	pre  string // empty for release versions
}

func parseSemver(v string) (semver, bool) {
	v = strings.TrimPrefix(v, "v")
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return semver{}, false
	}
	var core [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, false
		}
		core[i] = n
	}
	return semver{core: core, pre: pre}, true
}
