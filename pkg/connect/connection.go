// Package connect provides connection URL resolution for CockroachDB clusters,
// with flag and environment variable semantics compatible with the cockroach sql CLI.
package connect

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

// ConnectionConfig holds all connection parameters and tracks which were
// explicitly set (vs left as defaults) to enable correct env var fallback behavior.
//
// The *Set fields mirror cobra's Flags().Changed() — they should be true only
// when the caller explicitly provided that flag, not when it is a default value.
type ConnectionConfig struct {
	// URL is the full PostgreSQL connection URL (--url flag).
	URL string
	// LegacyURL is the deprecated --connection-url / -c flag value.
	LegacyURL string
	// Host is the database host (--host flag).
	Host string
	// Port is the database port (--port flag).
	Port int
	// User is the database user (--user / -u flag).
	User string
	// Database is the database name (--database / -d flag).
	Database string
	// Insecure disables TLS when true (--insecure flag).
	Insecure bool
	// CertsDir is the path to the certificate directory (--certs-dir flag).
	CertsDir string

	// URLSet indicates --url was explicitly provided.
	URLSet bool
	// LegacyURLSet indicates --connection-url / -c was explicitly provided.
	LegacyURLSet bool
	// UserSet indicates --user was explicitly provided.
	UserSet bool
	// DatabaseSet indicates --database was explicitly provided.
	DatabaseSet bool
	// InsecureSet indicates --insecure was explicitly provided.
	InsecureSet bool
	// CertsDirSet indicates --certs-dir was explicitly provided.
	CertsDirSet bool
}

// DefaultCertsDir returns the default certificate directory (~/.cockroach-certs),
// matching the default used by cockroach sql.
func DefaultCertsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cockroach-certs"
	}
	return filepath.Join(home, ".cockroach-certs")
}

// ResolveConnectionURL determines the final PostgreSQL connection URL from cfg.
//
// Priority order:
//  1. --url flag (cfg.URL when cfg.URLSet is true)
//  2. --connection-url / -c flag (cfg.LegacyURL when cfg.LegacyURLSet) — deprecated
//  3. COCKROACH_URL environment variable
//  4. Discrete flags (--host, --port, --user, --database, --insecure, --certs-dir)
//     with COCKROACH_* environment variable fallbacks for each
func ResolveConnectionURL(cfg ConnectionConfig) (string, error) {
	if cfg.URLSet && cfg.LegacyURLSet {
		return "", fmt.Errorf("cannot specify both --url and --connection-url")
	}
	if cfg.URLSet {
		return cfg.URL, nil
	}
	if cfg.LegacyURLSet {
		return cfg.LegacyURL, nil
	}
	if envURL := os.Getenv("COCKROACH_URL"); envURL != "" {
		return envURL, nil
	}
	return BuildConnectionURL(cfg)
}

// BuildConnectionURL constructs a PostgreSQL connection URL from discrete
// connection parameters in cfg, falling back to COCKROACH_* environment
// variables for any field that was not explicitly set.
//
// TLS behavior when --insecure is not set:
//   - ca.crt + client.<user>.crt + client.<user>.key found → sslmode=verify-full with all certs
//   - ca.crt only found                                     → sslmode=verify-full with root cert
//   - No certs found                                        → sslmode=require
func BuildConnectionURL(cfg ConnectionConfig) (string, error) {
	host := cfg.Host
	port := cfg.Port
	user := cfg.User
	database := cfg.Database
	insecure := cfg.Insecure
	certsDir := cfg.CertsDir

	if !cfg.UserSet {
		if v := os.Getenv("COCKROACH_USER"); v != "" {
			user = v
		}
	}
	if !cfg.DatabaseSet {
		if v := os.Getenv("COCKROACH_DATABASE"); v != "" {
			database = v
		}
	}
	if !cfg.InsecureSet {
		if v := os.Getenv("COCKROACH_INSECURE"); v != "" {
			val, err := strconv.ParseBool(v)
			if err != nil {
				return "", fmt.Errorf("invalid value for COCKROACH_INSECURE: %w", err)
			}
			insecure = val
		}
	}
	if !cfg.CertsDirSet {
		if v := os.Getenv("COCKROACH_CERTS_DIR"); v != "" {
			certsDir = v
		}
	}

	u := &url.URL{
		Scheme: "postgresql",
		User:   url.User(user),
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   database,
	}

	params := url.Values{}
	if insecure {
		params.Set("sslmode", "disable")
	} else {
		caCert := filepath.Join(certsDir, "ca.crt")
		clientCert := filepath.Join(certsDir, fmt.Sprintf("client.%s.crt", user))
		clientKey := filepath.Join(certsDir, fmt.Sprintf("client.%s.key", user))

		switch {
		case fileExists(caCert) && fileExists(clientCert) && fileExists(clientKey):
			params.Set("sslmode", "verify-full")
			params.Set("sslrootcert", caCert)
			params.Set("sslcert", clientCert)
			params.Set("sslkey", clientKey)
		case fileExists(caCert):
			params.Set("sslmode", "verify-full")
			params.Set("sslrootcert", caCert)
		default:
			params.Set("sslmode", "require")
		}
	}

	u.RawQuery = params.Encode()
	return u.String(), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
