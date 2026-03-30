package connect_test

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cockroachlabs/workload-exporter/pkg/connect"
)

// parseURL is a test helper that parses a URL string and fatals on error.
func parseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", raw, err)
	}
	return u
}

// touch creates an empty file at dir/name and returns its full path.
func touch(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatalf("failed to create file %s: %v", path, err)
	}
	return path
}

// allCerts creates ca.crt, client.<user>.crt, and client.<user>.key in dir.
func allCerts(t *testing.T, dir, user string) {
	t.Helper()
	touch(t, dir, "ca.crt")
	touch(t, dir, "client."+user+".crt")
	touch(t, dir, "client."+user+".key")
}

// insecureCfg returns a minimal config suitable for testing non-TLS logic without
// env var interference — insecure, all *Set flags true.
func insecureCfg(host string, port int, user string) connect.ConnectionConfig {
	return connect.ConnectionConfig{
		Host:        host,
		Port:        port,
		User:        user,
		UserSet:     true,
		Insecure:    true,
		InsecureSet: true,
		CertsDirSet: true,
	}
}

// =====================================================================
// DefaultCertsDir
// =====================================================================

func TestDefaultCertsDir_NonEmpty(t *testing.T) {
	dir := connect.DefaultCertsDir()
	if dir == "" {
		t.Fatal("DefaultCertsDir returned empty string")
	}
}

func TestDefaultCertsDir_EndsWithCockroachCerts(t *testing.T) {
	dir := connect.DefaultCertsDir()
	if !strings.HasSuffix(dir, ".cockroach-certs") {
		t.Errorf("DefaultCertsDir %q does not end with .cockroach-certs", dir)
	}
}

func TestDefaultCertsDir_IsAbsolutePath(t *testing.T) {
	dir := connect.DefaultCertsDir()
	if !filepath.IsAbs(dir) {
		t.Errorf("DefaultCertsDir %q is not an absolute path", dir)
	}
}

// =====================================================================
// ResolveConnectionURL — priority chain
// =====================================================================

// URL flag is returned verbatim, regardless of content.
func TestResolveConnectionURL_URLFlag(t *testing.T) {
	want := "postgresql://user:secret@host:26257/db?sslmode=verify-full"
	got, err := connect.ResolveConnectionURL(connect.ConnectionConfig{
		URL:    want,
		URLSet: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// URL flag beats COCKROACH_URL env var.
func TestResolveConnectionURL_URLFlagBeatsEnvVar(t *testing.T) {
	t.Setenv("COCKROACH_URL", "postgresql://env@envhost:26257/envdb")
	want := "postgresql://flag@flaghost:26257/flagdb"
	got, err := connect.ResolveConnectionURL(connect.ConnectionConfig{
		URL:    want,
		URLSet: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// URL field populated but URLSet=false — the value must be ignored.
func TestResolveConnectionURL_URLFieldIgnoredWhenNotSet(t *testing.T) {
	cfg := insecureCfg("myhost", 26257, "root")
	cfg.URL = "postgresql://should-not-appear@ignored:26257/ignored"
	cfg.URLSet = false // not set

	got, err := connect.ResolveConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "should-not-appear") {
		t.Errorf("URL field value leaked into result when URLSet=false: %q", got)
	}
}

// Legacy URL flag (--connection-url / -c) is returned verbatim.
func TestResolveConnectionURL_LegacyURLFlag(t *testing.T) {
	want := "postgresql://legacy@legacyhost:26257/legacydb"
	got, err := connect.ResolveConnectionURL(connect.ConnectionConfig{
		LegacyURL:    want,
		LegacyURLSet: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Legacy URL flag beats COCKROACH_URL env var.
func TestResolveConnectionURL_LegacyURLFlagBeatsEnvVar(t *testing.T) {
	t.Setenv("COCKROACH_URL", "postgresql://env@envhost:26257/envdb")
	want := "postgresql://legacy@legacyhost:26257/legacydb"
	got, err := connect.ResolveConnectionURL(connect.ConnectionConfig{
		LegacyURL:    want,
		LegacyURLSet: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// LegacyURL field populated but LegacyURLSet=false — the value must be ignored.
func TestResolveConnectionURL_LegacyURLFieldIgnoredWhenNotSet(t *testing.T) {
	cfg := insecureCfg("myhost", 26257, "root")
	cfg.LegacyURL = "postgresql://should-not-appear@ignored:26257/ignored"
	cfg.LegacyURLSet = false // not set

	got, err := connect.ResolveConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "should-not-appear") {
		t.Errorf("LegacyURL field value leaked into result when LegacyURLSet=false: %q", got)
	}
}

// Setting both --url and --connection-url is an error.
func TestResolveConnectionURL_BothURLFlagsIsError(t *testing.T) {
	_, err := connect.ResolveConnectionURL(connect.ConnectionConfig{
		URL:          "postgresql://a@host:26257/db",
		URLSet:       true,
		LegacyURL:    "postgresql://b@host:26257/db",
		LegacyURLSet: true,
	})
	if err == nil {
		t.Fatal("expected error when both --url and --connection-url are set, got nil")
	}
}

// COCKROACH_URL env var is used when no URL flags are set.
func TestResolveConnectionURL_CockroachURLEnvVar(t *testing.T) {
	want := "postgresql://envuser@envhost:26257/envdb?sslmode=verify-full"
	t.Setenv("COCKROACH_URL", want)

	got, err := connect.ResolveConnectionURL(connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root",
		InsecureSet: true,
		CertsDirSet: true,
		CertsDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Empty COCKROACH_URL must not short-circuit; should fall through to discrete flags.
func TestResolveConnectionURL_EmptyCockroachURLFallsThrough(t *testing.T) {
	t.Setenv("COCKROACH_URL", "")

	cfg := insecureCfg("discretehost", 26257, "root")
	got, err := connect.ResolveConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Hostname() != "discretehost" {
		t.Errorf("expected host from discrete flags, got %q", u.Hostname())
	}
}

// With no URL flags and no COCKROACH_URL, discrete flags are used.
func TestResolveConnectionURL_FallsBackToDiscreteFlags(t *testing.T) {
	cfg := insecureCfg("myhost", 26257, "myuser")
	got, err := connect.ResolveConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Hostname() != "myhost" {
		t.Errorf("host: got %q, want %q", u.Hostname(), "myhost")
	}
	if u.User.Username() != "myuser" {
		t.Errorf("user: got %q, want %q", u.User.Username(), "myuser")
	}
}

// =====================================================================
// BuildConnectionURL — URL structure
// =====================================================================

func TestBuildConnectionURL_Scheme(t *testing.T) {
	cfg := insecureCfg("localhost", 26257, "root")
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Scheme != "postgresql" {
		t.Errorf("scheme: got %q, want %q", u.Scheme, "postgresql")
	}
}

func TestBuildConnectionURL_HostAndPort(t *testing.T) {
	tests := []struct {
		host     string
		port     int
		wantHost string
		wantPort string
	}{
		{"localhost", 26257, "localhost", "26257"},
		{"mycluster.example.com", 26257, "mycluster.example.com", "26257"},
		{"10.0.0.1", 5432, "10.0.0.1", "5432"},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			cfg := insecureCfg(tt.host, tt.port, "root")
			got, err := connect.BuildConnectionURL(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			u := parseURL(t, got)
			if u.Hostname() != tt.wantHost {
				t.Errorf("host: got %q, want %q", u.Hostname(), tt.wantHost)
			}
			if u.Port() != tt.wantPort {
				t.Errorf("port: got %q, want %q", u.Port(), tt.wantPort)
			}
		})
	}
}

func TestBuildConnectionURL_UserInURL(t *testing.T) {
	cfg := insecureCfg("localhost", 26257, "myuser")
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.User.Username() != "myuser" {
		t.Errorf("user: got %q, want %q", u.User.Username(), "myuser")
	}
}

func TestBuildConnectionURL_DatabaseInPath(t *testing.T) {
	cfg := insecureCfg("localhost", 26257, "root")
	cfg.Database = "mydb"
	cfg.DatabaseSet = true

	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	// net/url may or may not include the leading slash depending on the host
	if u.Path != "mydb" && u.Path != "/mydb" {
		t.Errorf("path: got %q, want mydb or /mydb", u.Path)
	}
}

func TestBuildConnectionURL_EmptyDatabaseOmitsPath(t *testing.T) {
	cfg := insecureCfg("localhost", 26257, "root")
	cfg.Database = ""
	cfg.DatabaseSet = true

	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Path != "" {
		t.Errorf("expected empty path for empty database, got %q", u.Path)
	}
}

// =====================================================================
// BuildConnectionURL — insecure / TLS mode
// =====================================================================

func TestBuildConnectionURL_InsecureFlagDisablesTLS(t *testing.T) {
	cfg := insecureCfg("localhost", 26257, "root")
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Query().Get("sslmode") != "disable" {
		t.Errorf("sslmode: got %q, want disable", u.Query().Get("sslmode"))
	}
}

// Insecure=true must suppress all cert params even when cert files are present.
func TestBuildConnectionURL_InsecureIgnoresCertFiles(t *testing.T) {
	certsDir := t.TempDir()
	allCerts(t, certsDir, "root")

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root",
		UserSet:     true,
		Insecure:    true,
		InsecureSet: true,
		CertsDir:    certsDir,
		CertsDirSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Query().Get("sslmode") != "disable" {
		t.Errorf("sslmode: got %q, want disable", u.Query().Get("sslmode"))
	}
	for _, param := range []string{"sslrootcert", "sslcert", "sslkey"} {
		if v := u.Query().Get(param); v != "" {
			t.Errorf("expected no %s when insecure=true, got %q", param, v)
		}
	}
}

// COCKROACH_INSECURE env var is applied when InsecureSet=false.
func TestBuildConnectionURL_EnvInsecureTrue(t *testing.T) {
	t.Setenv("COCKROACH_INSECURE", "true")

	cfg := connect.ConnectionConfig{
		Host:    "localhost",
		Port:    26257,
		User:    "root",
		UserSet: true,
		// InsecureSet: false → env var applies
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parseURL(t, got).Query().Get("sslmode") != "disable" {
		t.Errorf("expected sslmode=disable from COCKROACH_INSECURE=true")
	}
}

// COCKROACH_INSECURE=false must not enable insecure mode.
func TestBuildConnectionURL_EnvInsecureFalseDoesNotDisableTLS(t *testing.T) {
	t.Setenv("COCKROACH_INSECURE", "false")

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root",
		UserSet:     true,
		CertsDir:    t.TempDir(), // no certs → sslmode=require
		CertsDirSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parseURL(t, got).Query().Get("sslmode") == "disable" {
		t.Error("sslmode should not be disable when COCKROACH_INSECURE=false")
	}
}

// strconv.ParseBool accepts several forms; all should work.
func TestBuildConnectionURL_EnvInsecureVariants(t *testing.T) {
	tests := []struct {
		value        string
		wantInsecure bool
	}{
		{"1", true},
		{"t", true},
		{"T", true},
		{"TRUE", true},
		{"true", true},
		{"True", true},
		{"0", false},
		{"f", false},
		{"F", false},
		{"FALSE", false},
		{"false", false},
		{"False", false},
	}
	for _, tt := range tests {
		t.Run("COCKROACH_INSECURE="+tt.value, func(t *testing.T) {
			t.Setenv("COCKROACH_INSECURE", tt.value)
			cfg := connect.ConnectionConfig{
				Host:        "localhost",
				Port:        26257,
				User:        "root",
				UserSet:     true,
				CertsDir:    t.TempDir(),
				CertsDirSet: true,
			}
			got, err := connect.BuildConnectionURL(cfg)
			if err != nil {
				t.Fatalf("unexpected error for COCKROACH_INSECURE=%q: %v", tt.value, err)
			}
			sslmode := parseURL(t, got).Query().Get("sslmode")
			if tt.wantInsecure && sslmode != "disable" {
				t.Errorf("COCKROACH_INSECURE=%q: sslmode got %q, want disable", tt.value, sslmode)
			}
			if !tt.wantInsecure && sslmode == "disable" {
				t.Errorf("COCKROACH_INSECURE=%q: sslmode got disable, want non-disable", tt.value)
			}
		})
	}
}

// An invalid COCKROACH_INSECURE value must return an error.
func TestBuildConnectionURL_EnvInsecureInvalidValue(t *testing.T) {
	t.Setenv("COCKROACH_INSECURE", "notabool")

	cfg := connect.ConnectionConfig{
		Host:    "localhost",
		Port:    26257,
		User:    "root",
		UserSet: true,
	}
	_, err := connect.BuildConnectionURL(cfg)
	if err == nil {
		t.Fatal("expected error for invalid COCKROACH_INSECURE, got nil")
	}
}

// COCKROACH_INSECURE is ignored when InsecureSet=true.
func TestBuildConnectionURL_EnvInsecureIgnoredWhenFlagSet(t *testing.T) {
	t.Setenv("COCKROACH_INSECURE", "true")

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root",
		UserSet:     true,
		Insecure:    false,
		InsecureSet: true, // flag was explicitly set to false
		CertsDir:    t.TempDir(),
		CertsDirSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parseURL(t, got).Query().Get("sslmode") == "disable" {
		t.Error("sslmode should not be disable: env var must be ignored when InsecureSet=true")
	}
}

// =====================================================================
// BuildConnectionURL — environment variable overrides
// =====================================================================

func TestBuildConnectionURL_EnvUserApplied(t *testing.T) {
	t.Setenv("COCKROACH_USER", "envuser")

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root", // default, not explicitly set
		InsecureSet: true,
		CertsDirSet: true,
		CertsDir:    t.TempDir(),
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parseURL(t, got).User.Username() != "envuser" {
		t.Errorf("expected user from COCKROACH_USER env var")
	}
}

func TestBuildConnectionURL_EnvUserIgnoredWhenFlagSet(t *testing.T) {
	t.Setenv("COCKROACH_USER", "envuser")

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "flaguser",
		UserSet:     true,
		InsecureSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parseURL(t, got).User.Username() != "flaguser" {
		t.Errorf("expected flaguser, COCKROACH_USER must be ignored when UserSet=true")
	}
}

func TestBuildConnectionURL_EnvDatabaseApplied(t *testing.T) {
	t.Setenv("COCKROACH_DATABASE", "envdb")

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root",
		UserSet:     true,
		InsecureSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Path != "envdb" && u.Path != "/envdb" {
		t.Errorf("path: got %q, want envdb or /envdb", u.Path)
	}
}

func TestBuildConnectionURL_EnvDatabaseIgnoredWhenFlagSet(t *testing.T) {
	t.Setenv("COCKROACH_DATABASE", "envdb")

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root",
		UserSet:     true,
		Database:    "flagdb",
		DatabaseSet: true,
		InsecureSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Path != "flagdb" && u.Path != "/flagdb" {
		t.Errorf("path: got %q, want flagdb or /flagdb; COCKROACH_DATABASE must be ignored when DatabaseSet=true", u.Path)
	}
}

func TestBuildConnectionURL_EnvCertsDirApplied(t *testing.T) {
	certsDir := t.TempDir()
	t.Setenv("COCKROACH_CERTS_DIR", certsDir)
	touch(t, certsDir, "ca.crt")

	cfg := connect.ConnectionConfig{
		Host:    "localhost",
		Port:    26257,
		User:    "root",
		UserSet: true,
		// CertsDirSet: false → env var applies
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Query().Get("sslmode") != "verify-full" {
		t.Errorf("sslmode: got %q, want verify-full", u.Query().Get("sslmode"))
	}
	if u.Query().Get("sslrootcert") != filepath.Join(certsDir, "ca.crt") {
		t.Errorf("sslrootcert: got %q, want %q", u.Query().Get("sslrootcert"), filepath.Join(certsDir, "ca.crt"))
	}
}

func TestBuildConnectionURL_EnvCertsDirIgnoredWhenFlagSet(t *testing.T) {
	envCertsDir := t.TempDir()
	flagCertsDir := t.TempDir()
	t.Setenv("COCKROACH_CERTS_DIR", envCertsDir)
	touch(t, envCertsDir, "ca.crt") // only env dir has certs

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root",
		UserSet:     true,
		CertsDir:    flagCertsDir, // empty, explicitly set
		CertsDirSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// flag dir has no certs → require, env dir certs must not be used
	if parseURL(t, got).Query().Get("sslmode") != "require" {
		t.Errorf("sslmode: COCKROACH_CERTS_DIR must be ignored when CertsDirSet=true")
	}
}

// All env vars applied simultaneously when no *Set flags are true.
func TestBuildConnectionURL_AllEnvVarsApplied(t *testing.T) {
	certsDir := t.TempDir()
	allCerts(t, certsDir, "envuser")
	t.Setenv("COCKROACH_USER", "envuser")
	t.Setenv("COCKROACH_DATABASE", "envdb")
	t.Setenv("COCKROACH_CERTS_DIR", certsDir)

	cfg := connect.ConnectionConfig{
		Host: "localhost",
		Port: 26257,
		// No *Set flags — all values come from env vars
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.User.Username() != "envuser" {
		t.Errorf("user: got %q, want envuser", u.User.Username())
	}
	if u.Path != "envdb" && u.Path != "/envdb" {
		t.Errorf("database: got path %q, want envdb", u.Path)
	}
	if u.Query().Get("sslmode") != "verify-full" {
		t.Errorf("sslmode: got %q, want verify-full", u.Query().Get("sslmode"))
	}
	if u.Query().Get("sslcert") != filepath.Join(certsDir, "client.envuser.crt") {
		t.Errorf("sslcert: got %q, want client.envuser.crt", u.Query().Get("sslcert"))
	}
}

// =====================================================================
// BuildConnectionURL — TLS certificate selection
// =====================================================================

// No cert files → sslmode=require.
func TestBuildConnectionURL_TLS_NoCerts(t *testing.T) {
	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root",
		UserSet:     true,
		CertsDir:    t.TempDir(), // empty
		CertsDirSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Query().Get("sslmode") != "require" {
		t.Errorf("sslmode: got %q, want require", u.Query().Get("sslmode"))
	}
	for _, param := range []string{"sslrootcert", "sslcert", "sslkey"} {
		if v := u.Query().Get(param); v != "" {
			t.Errorf("expected no %s with no certs, got %q", param, v)
		}
	}
}

// ca.crt only → sslmode=verify-full with only sslrootcert.
func TestBuildConnectionURL_TLS_CACertOnly(t *testing.T) {
	certsDir := t.TempDir()
	touch(t, certsDir, "ca.crt")

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root",
		UserSet:     true,
		CertsDir:    certsDir,
		CertsDirSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Query().Get("sslmode") != "verify-full" {
		t.Errorf("sslmode: got %q, want verify-full", u.Query().Get("sslmode"))
	}
	if u.Query().Get("sslrootcert") != filepath.Join(certsDir, "ca.crt") {
		t.Errorf("sslrootcert: got %q, want %q", u.Query().Get("sslrootcert"), filepath.Join(certsDir, "ca.crt"))
	}
	if u.Query().Get("sslcert") != "" || u.Query().Get("sslkey") != "" {
		t.Errorf("expected no sslcert/sslkey with CA only, got sslcert=%q sslkey=%q",
			u.Query().Get("sslcert"), u.Query().Get("sslkey"))
	}
}

// ca.crt + client cert + client key → sslmode=verify-full with all three params.
func TestBuildConnectionURL_TLS_AllCerts(t *testing.T) {
	certsDir := t.TempDir()
	allCerts(t, certsDir, "root")

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root",
		UserSet:     true,
		CertsDir:    certsDir,
		CertsDirSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Query().Get("sslmode") != "verify-full" {
		t.Errorf("sslmode: got %q, want verify-full", u.Query().Get("sslmode"))
	}
	if u.Query().Get("sslrootcert") != filepath.Join(certsDir, "ca.crt") {
		t.Errorf("sslrootcert: got %q", u.Query().Get("sslrootcert"))
	}
	if u.Query().Get("sslcert") != filepath.Join(certsDir, "client.root.crt") {
		t.Errorf("sslcert: got %q", u.Query().Get("sslcert"))
	}
	if u.Query().Get("sslkey") != filepath.Join(certsDir, "client.root.key") {
		t.Errorf("sslkey: got %q", u.Query().Get("sslkey"))
	}
}

// Cert filenames are based on the username — non-root user gets client.<user>.* files.
func TestBuildConnectionURL_TLS_UserSpecificCertNames(t *testing.T) {
	certsDir := t.TempDir()
	allCerts(t, certsDir, "alice")

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "alice",
		UserSet:     true,
		CertsDir:    certsDir,
		CertsDirSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Query().Get("sslcert") != filepath.Join(certsDir, "client.alice.crt") {
		t.Errorf("sslcert: got %q, want client.alice.crt", u.Query().Get("sslcert"))
	}
	if u.Query().Get("sslkey") != filepath.Join(certsDir, "client.alice.key") {
		t.Errorf("sslkey: got %q, want client.alice.key", u.Query().Get("sslkey"))
	}
}

// Env-sourced user (COCKROACH_USER) must also drive the cert filename lookup.
func TestBuildConnectionURL_TLS_EnvUserDrivesCertNames(t *testing.T) {
	t.Setenv("COCKROACH_USER", "bob")
	certsDir := t.TempDir()
	allCerts(t, certsDir, "bob")

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root", // not set — env var overrides
		CertsDir:    certsDir,
		CertsDirSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.User.Username() != "bob" {
		t.Errorf("user: got %q, want bob", u.User.Username())
	}
	if u.Query().Get("sslcert") != filepath.Join(certsDir, "client.bob.crt") {
		t.Errorf("sslcert: got %q, want client.bob.crt", u.Query().Get("sslcert"))
	}
}

// ca.crt + client cert present but key missing → treat as CA-only.
func TestBuildConnectionURL_TLS_MissingKeyFallsBackToCAOnly(t *testing.T) {
	certsDir := t.TempDir()
	touch(t, certsDir, "ca.crt")
	touch(t, certsDir, "client.root.crt")
	// no client.root.key

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root",
		UserSet:     true,
		CertsDir:    certsDir,
		CertsDirSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Query().Get("sslmode") != "verify-full" {
		t.Errorf("sslmode: got %q, want verify-full", u.Query().Get("sslmode"))
	}
	if u.Query().Get("sslcert") != "" {
		t.Errorf("expected no sslcert with missing key, got %q", u.Query().Get("sslcert"))
	}
}

// ca.crt + key present but client cert missing → treat as CA-only.
func TestBuildConnectionURL_TLS_MissingCertFallsBackToCAOnly(t *testing.T) {
	certsDir := t.TempDir()
	touch(t, certsDir, "ca.crt")
	touch(t, certsDir, "client.root.key")
	// no client.root.crt

	cfg := connect.ConnectionConfig{
		Host:        "localhost",
		Port:        26257,
		User:        "root",
		UserSet:     true,
		CertsDir:    certsDir,
		CertsDirSet: true,
	}
	got, err := connect.BuildConnectionURL(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	u := parseURL(t, got)
	if u.Query().Get("sslmode") != "verify-full" {
		t.Errorf("sslmode: got %q, want verify-full", u.Query().Get("sslmode"))
	}
	if u.Query().Get("sslcert") != "" {
		t.Errorf("expected no sslcert with missing cert file, got %q", u.Query().Get("sslcert"))
	}
}
