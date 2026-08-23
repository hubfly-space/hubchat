// Package config loads and validates runtime configuration.
//
// Precedence, lowest to highest: built-in defaults, configuration file,
// environment variables, command-line flags. Later sources override earlier
// ones field by field, so a file can set most of a deployment and an
// environment variable can override one value without restating the rest.
//
// Every secret-bearing field is excluded from String() and from the diagnostic
// output of `hubchat config check` (§11.5).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully-resolved runtime configuration. It is immutable once
// Load returns; nothing reloads it while the process is running.
type Config struct {
	Server        Server
	Database      Database
	Storage       Storage
	Email         Email
	OAuth         OAuth
	Security      Security
	Realtime      Realtime
	Jobs          Jobs
	Limits        Limits
	Observability Observability

	// Dev relaxes several protections and serves browser assets from disk
	// instead of the embedded filesystem. Refused when PublicURL is https.
	Dev bool
}

type Server struct {
	// Listen is the bind address, e.g. ":8080" or "127.0.0.1:8080".
	Listen string
	// PublicURL is how browsers reach this deployment. It is not cosmetic:
	// cookie domains, widget origins, webhook callbacks, email links, and CORS
	// all derive from it, which is why it is required rather than inferred
	// from the request Host (a header an attacker controls).
	PublicURL *url.URL
	// Roles this process runs. Default is every role in one process (§8.5).
	Roles []Role

	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	MaxRequestBytes int64
}

// Role lets one binary run as the whole system or as a slice of it, without a
// second codebase or a second deliverable (§8.5).
type Role string

const (
	RoleHTTP      Role = "http"
	RoleRealtime  Role = "realtime"
	RoleWorker    Role = "worker"
	RoleScheduler Role = "scheduler"
)

var AllRoles = []Role{RoleHTTP, RoleRealtime, RoleWorker, RoleScheduler}

type Database struct {
	URL              string // secret
	MaxOpenConns     int
	MaxIdleConns     int
	ConnMaxLifetime  time.Duration
	StatementTimeout time.Duration
	// MigratePolicy decides what happens at boot when migrations are pending.
	// Defaulting to "verify" rather than "apply" is deliberate: a process
	// restart must never silently rewrite a production schema.
	MigratePolicy MigratePolicy
}

type MigratePolicy string

const (
	// MigrateVerify refuses to start if migrations are pending.
	MigrateVerify MigratePolicy = "verify"
	// MigrateApply applies pending migrations under an advisory lock.
	MigrateApply MigratePolicy = "apply"
	// MigrateSkip starts regardless. For read replicas and debugging.
	MigrateSkip MigratePolicy = "skip"
)

type Storage struct {
	// Backend is "local" or "s3".
	Backend   string
	LocalPath string

	S3Endpoint  string
	S3Region    string
	S3Bucket    string
	S3AccessKey string // secret
	S3SecretKey string // secret
	S3PathStyle bool

	MaxFileBytes     int64
	AllowedMimeTypes []string
}

type Email struct {
	Enabled      bool
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string // secret
	Encryption   string // "starttls", "tls", "none"
	FromName     string
	FromAddress  string
	ReplyTo      string
	InboundMode  string // "webhook", "imap", "off"
}

// OAuth is an optional deployment-level OIDC/OAuth2 sign-in adapter. The
// endpoints are configured by the operator rather than accepted from a
// request, which prevents callback SSRF and keeps provider secrets out of
// workspace-owned records. Workspace-specific SSO policy is a later layer.
type OAuth struct {
	Enabled  bool
	Provider string
	// Profile selects a built-in identity adapter. Empty infers google or
	// microsoft from Provider; all other values use the generic adapter.
	Profile          string
	ClientID         string
	ClientSecret     string // secret
	AuthorizationURL string
	TokenURL         string
	UserinfoURL      string
	Scopes           []string
	AllowedDomains   []string
}

type Security struct {
	// SecretKey derives cookie signing keys, identity-token HMACs, and the
	// encryption key for recoverable integration secrets. Losing it makes
	// encrypted columns unreadable, which is why the doctor command checks it
	// before anything else.
	SecretKey []byte // secret

	SessionLifetime time.Duration
	CookieDomain    string
	CookieSecure    bool
	CSRFEnabled     bool

	TrustedProxies []string
	// GeoIPDatabasePath points to a local MaxMind-compatible country/city DB.
	// Empty disables lookup while still allowing masked prefixes to be stored.
	GeoIPDatabasePath string
	RateLimitRPM      int
	LoginAttempts     int
	LockoutWindow     time.Duration
}

type Realtime struct {
	Enabled           bool
	MaxConnections    int
	WriteBufferSize   int
	HeartbeatInterval time.Duration
	ClientTimeout     time.Duration
	// OutboundQueueSize bounds each client's send queue. A client that cannot
	// keep up is disconnected rather than allowed to consume memory (§17).
	OutboundQueueSize int
}

type Jobs struct {
	Enabled       bool
	Concurrency   int
	PollInterval  time.Duration
	LeaseDuration time.Duration
	MaxAttempts   int
}

type Limits struct {
	MaxEventBytes            int64
	MaxAttributesPerCustomer int
	MaxActionsPerRule        int
	MaxRuleDepth             int
}

type Observability struct {
	LogFormat       string // "text" or "json"
	LogLevel        string
	MetricsEnabled  bool
	TracingEndpoint string
	DevLite         DevLite
}

// DevLite configures the optional outbound observability sink. APIKey is the
// only enabling switch: an empty key leaves Hubchat's normal slog output and
// request handling completely unchanged.
type DevLite struct {
	APIKey          string // secret
	Endpoint        string
	Environment     string
	ServiceName     string
	Release         string
	SampleRate      float64
	FlushInterval   time.Duration
	MetricsInterval time.Duration
}

// Default returns the configuration a fresh installation runs with. Everything
// here works on a laptop with only a database URL supplied.
func Default() Config {
	return Config{
		Server: Server{
			Listen:          ":8080",
			Roles:           AllRoles,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    60 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 25 * time.Second,
			// Large enough for the default 25 MiB attachment limit plus
			// multipart overhead, while still bounding every request body.
			MaxRequestBytes: 32 << 20,
		},
		Database: Database{
			MaxOpenConns:     25,
			MaxIdleConns:     5,
			ConnMaxLifetime:  30 * time.Minute,
			StatementTimeout: 15 * time.Second,
			MigratePolicy:    MigrateVerify,
		},
		Storage: Storage{
			Backend:      "local",
			LocalPath:    "./data/files",
			MaxFileBytes: 25 << 20,
		},
		Email: Email{
			SMTPPort:    587,
			Encryption:  "starttls",
			InboundMode: "off",
		},
		Security: Security{
			SessionLifetime: 30 * 24 * time.Hour,
			CookieSecure:    true,
			CSRFEnabled:     true,
			RateLimitRPM:    600,
			LoginAttempts:   5,
			LockoutWindow:   15 * time.Minute,
		},
		Realtime: Realtime{
			Enabled:           true,
			MaxConnections:    10_000,
			WriteBufferSize:   4096,
			HeartbeatInterval: 25 * time.Second,
			ClientTimeout:     60 * time.Second,
			OutboundQueueSize: 256,
		},
		Jobs: Jobs{
			Enabled:       true,
			Concurrency:   8,
			PollInterval:  2 * time.Second,
			LeaseDuration: 5 * time.Minute,
			MaxAttempts:   5,
		},
		Limits: Limits{
			MaxEventBytes:            32 << 10,
			MaxAttributesPerCustomer: 64,
			MaxActionsPerRule:        20,
			MaxRuleDepth:             3,
		},
		Observability: Observability{
			LogFormat:      "text",
			LogLevel:       "info",
			MetricsEnabled: true,
			DevLite: DevLite{
				Environment:     "development",
				ServiceName:     "hubchat",
				SampleRate:      1,
				FlushInterval:   5 * time.Second,
				MetricsInterval: 30 * time.Second,
			},
		},
	}
}

// Load resolves configuration from the environment on top of the defaults.
//
// File and flag layers call into the same setters; they are omitted here so
// the resolution order stays visible in one function rather than spread across
// three.
func Load() (Config, error) {
	cfg := Default()

	// A .env file in the working directory fills in anything the real
	// environment has not already set. It is a development convenience only —
	// the file layer proper is documented above, and a deployment that ships a
	// .env alongside the binary has put its secrets on disk in plain text.
	loadDotEnv(".env")

	if v := os.Getenv("HUBCHAT_LISTEN"); v != "" {
		cfg.Server.Listen = v
	}

	if v := os.Getenv("HUBCHAT_PUBLIC_URL"); v != "" {
		parsed, err := url.Parse(v)
		if err != nil {
			return cfg, fmt.Errorf("HUBCHAT_PUBLIC_URL is not a valid URL: %w", err)
		}
		cfg.Server.PublicURL = parsed
	}

	if v := os.Getenv("HUBCHAT_ROLES"); v != "" {
		roles, err := parseRoles(v)
		if err != nil {
			return cfg, err
		}
		cfg.Server.Roles = roles
	}

	cfg.Database.URL = os.Getenv("HUBCHAT_DATABASE_URL")

	if v := os.Getenv("HUBCHAT_MIGRATE"); v != "" {
		switch MigratePolicy(v) {
		case MigrateVerify, MigrateApply, MigrateSkip:
			cfg.Database.MigratePolicy = MigratePolicy(v)
		default:
			return cfg, fmt.Errorf("HUBCHAT_MIGRATE must be verify, apply, or skip (got %q)", v)
		}
	}

	if v := os.Getenv("HUBCHAT_SECRET_KEY"); v != "" {
		cfg.Security.SecretKey = []byte(v)
	}
	if v := os.Getenv("HUBCHAT_GEOIP_DATABASE_PATH"); v != "" {
		cfg.Security.GeoIPDatabasePath = v
	}

	if v := os.Getenv("HUBCHAT_DATA_DIR"); v != "" {
		cfg.Storage.LocalPath = v
	}

	if v := os.Getenv("HUBCHAT_STORAGE_BACKEND"); v != "" {
		switch v {
		case "local", "s3":
			cfg.Storage.Backend = v
		default:
			return cfg, fmt.Errorf("HUBCHAT_STORAGE_BACKEND must be local or s3 (got %q)", v)
		}
	}
	if v := os.Getenv("HUBCHAT_S3_ENDPOINT"); v != "" {
		cfg.Storage.S3Endpoint = v
	}
	if v := os.Getenv("HUBCHAT_S3_REGION"); v != "" {
		cfg.Storage.S3Region = v
	}
	if v := os.Getenv("HUBCHAT_S3_BUCKET"); v != "" {
		cfg.Storage.S3Bucket = v
	}
	if v := os.Getenv("HUBCHAT_S3_ACCESS_KEY"); v != "" {
		cfg.Storage.S3AccessKey = v
	}
	if v := os.Getenv("HUBCHAT_S3_SECRET_KEY"); v != "" {
		cfg.Storage.S3SecretKey = v
	}
	cfg.Storage.S3PathStyle = os.Getenv("HUBCHAT_S3_PATH_STYLE") == "1"

	if v := os.Getenv("HUBCHAT_SMTP_HOST"); v != "" {
		cfg.Email.Enabled = true
		cfg.Email.SMTPHost = v
		cfg.Email.SMTPUsername = os.Getenv("HUBCHAT_SMTP_USERNAME")
		cfg.Email.SMTPPassword = os.Getenv("HUBCHAT_SMTP_PASSWORD")
		cfg.Email.FromAddress = os.Getenv("HUBCHAT_SMTP_FROM")
		if port := os.Getenv("HUBCHAT_SMTP_PORT"); port != "" {
			parsed, err := strconv.Atoi(port)
			if err != nil {
				return cfg, fmt.Errorf("HUBCHAT_SMTP_PORT must be a number: %w", err)
			}
			cfg.Email.SMTPPort = parsed
		}
	}

	if v := os.Getenv("HUBCHAT_OAUTH_PROVIDER"); v != "" {
		cfg.OAuth.Enabled = true
		cfg.OAuth.Provider = strings.ToLower(strings.TrimSpace(v))
		cfg.OAuth.Profile = strings.ToLower(strings.TrimSpace(os.Getenv("HUBCHAT_OAUTH_PROFILE")))
		cfg.OAuth.ClientID = os.Getenv("HUBCHAT_OAUTH_CLIENT_ID")
		cfg.OAuth.ClientSecret = os.Getenv("HUBCHAT_OAUTH_CLIENT_SECRET")
		cfg.OAuth.AuthorizationURL = os.Getenv("HUBCHAT_OAUTH_AUTHORIZATION_URL")
		cfg.OAuth.TokenURL = os.Getenv("HUBCHAT_OAUTH_TOKEN_URL")
		cfg.OAuth.UserinfoURL = os.Getenv("HUBCHAT_OAUTH_USERINFO_URL")
		if scopes := os.Getenv("HUBCHAT_OAUTH_SCOPES"); scopes != "" {
			for _, scope := range strings.Split(scopes, ",") {
				if scope = strings.TrimSpace(scope); scope != "" {
					cfg.OAuth.Scopes = append(cfg.OAuth.Scopes, scope)
				}
			}
		}
		if domains := os.Getenv("HUBCHAT_OAUTH_ALLOWED_DOMAINS"); domains != "" {
			for _, domain := range strings.Split(domains, ",") {
				if domain = strings.ToLower(strings.TrimSpace(domain)); domain != "" {
					cfg.OAuth.AllowedDomains = append(cfg.OAuth.AllowedDomains, domain)
				}
			}
		}
		if err := applyOAuthProfileDefaults(&cfg.OAuth); err != nil {
			return cfg, err
		}
	}

	if v := os.Getenv("HUBCHAT_LOG_FORMAT"); v != "" {
		cfg.Observability.LogFormat = v
	}
	if v := os.Getenv("HUBCHAT_LOG_LEVEL"); v != "" {
		cfg.Observability.LogLevel = v
	}
	if v := os.Getenv("DEVLITE_API_KEY"); v != "" {
		cfg.Observability.DevLite.APIKey = v
	}
	if v := os.Getenv("DEVLITE_ENDPOINT"); v != "" {
		cfg.Observability.DevLite.Endpoint = v
	}
	if v := os.Getenv("DEVLITE_ENVIRONMENT"); v != "" {
		cfg.Observability.DevLite.Environment = v
	}
	if v := os.Getenv("DEVLITE_SERVICE_NAME"); v != "" {
		cfg.Observability.DevLite.ServiceName = v
	}
	if v := os.Getenv("DEVLITE_RELEASE"); v != "" {
		cfg.Observability.DevLite.Release = v
	}
	if v := os.Getenv("DEVLITE_SAMPLE_RATE"); v != "" {
		rate, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return cfg, fmt.Errorf("DEVLITE_SAMPLE_RATE must be a number between 0 and 1: %w", err)
		}
		cfg.Observability.DevLite.SampleRate = rate
	}
	if v := os.Getenv("DEVLITE_FLUSH_INTERVAL"); v != "" {
		interval, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("DEVLITE_FLUSH_INTERVAL must be a duration such as 5s: %w", err)
		}
		cfg.Observability.DevLite.FlushInterval = interval
	}
	if v := os.Getenv("DEVLITE_METRICS_INTERVAL"); v != "" {
		interval, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("DEVLITE_METRICS_INTERVAL must be a duration such as 30s: %w", err)
		}
		cfg.Observability.DevLite.MetricsInterval = interval
	}

	cfg.Dev = os.Getenv("HUBCHAT_DEV") == "1"

	// Development mode runs over plain http (localhost), where a Secure cookie
	// is at best ignored and at worst silently dropped by some browsers — the
	// same reason Validate() refuses Dev behind an https PublicURL. The rest of
	// the dev posture already relaxes protections (§httpserver/csrf.go widens
	// the origin allowlist to loopback); the cookie flag is part of that.
	if cfg.Dev {
		cfg.Security.CookieSecure = false
	}

	return cfg, nil
}

const (
	OAuthProfileGeneric   = "generic"
	OAuthProfileGoogle    = "google"
	OAuthProfileMicrosoft = "microsoft"
)

// applyOAuthProfileDefaults fills only endpoints and scopes owned by a
// built-in adapter. Generic providers still have to provide every URL
// explicitly, so adding a profile cannot turn an arbitrary provider name into
// an outbound request target.
func applyOAuthProfileDefaults(oauth *OAuth) error {
	if oauth == nil {
		return nil
	}
	profile := strings.TrimSpace(strings.ToLower(oauth.Profile))
	if profile == "" {
		switch oauth.Provider {
		case OAuthProfileGoogle:
			profile = OAuthProfileGoogle
		case "microsoft", "entra", "entra-id":
			profile = OAuthProfileMicrosoft
		default:
			profile = OAuthProfileGeneric
		}
	}
	switch profile {
	case OAuthProfileGeneric:
	case OAuthProfileGoogle:
		if oauth.AuthorizationURL == "" {
			oauth.AuthorizationURL = "https://accounts.google.com/o/oauth2/v2/auth"
		}
		if oauth.TokenURL == "" {
			oauth.TokenURL = "https://oauth2.googleapis.com/token"
		}
		if oauth.UserinfoURL == "" {
			oauth.UserinfoURL = "https://openidconnect.googleapis.com/v1/userinfo"
		}
		if len(oauth.Scopes) == 0 {
			oauth.Scopes = []string{"openid", "email", "profile"}
		}
	case OAuthProfileMicrosoft:
		if oauth.AuthorizationURL == "" {
			oauth.AuthorizationURL = "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"
		}
		if oauth.TokenURL == "" {
			oauth.TokenURL = "https://login.microsoftonline.com/common/oauth2/v2.0/token"
		}
		if oauth.UserinfoURL == "" {
			oauth.UserinfoURL = "https://graph.microsoft.com/v1.0/me"
		}
		if len(oauth.Scopes) == 0 {
			oauth.Scopes = []string{"openid", "email", "profile", "User.Read"}
		}
	default:
		return fmt.Errorf("oauth: unsupported profile %q (use generic, google, or microsoft)", profile)
	}
	oauth.Profile = profile
	return nil
}

// Validate reports every problem it finds rather than the first, because an
// operator fixing configuration should not have to restart six times to
// discover six mistakes.
func (c Config) Validate() error {
	var problems []error

	if c.Database.URL == "" {
		problems = append(problems, errors.New(
			"database: HUBCHAT_DATABASE_URL is required (postgres://user:pass@host:5432/hubchat)"))
	}

	if c.Server.PublicURL == nil {
		problems = append(problems, errors.New(
			"server: HUBCHAT_PUBLIC_URL is required — cookies, widget origins, and email links derive from it"))
	} else {
		switch c.Server.PublicURL.Scheme {
		case "http", "https":
		default:
			problems = append(problems, fmt.Errorf(
				"server: public URL scheme must be http or https (got %q)", c.Server.PublicURL.Scheme))
		}
		if c.Server.PublicURL.Host == "" {
			problems = append(problems, errors.New("server: public URL is missing a host"))
		}
	}

	switch {
	case len(c.Security.SecretKey) == 0:
		problems = append(problems, errors.New(
			"security: HUBCHAT_SECRET_KEY is required — generate one with `openssl rand -base64 32`"))
	case len(c.Security.SecretKey) < 32:
		problems = append(problems, fmt.Errorf(
			"security: HUBCHAT_SECRET_KEY must be at least 32 bytes (got %d)", len(c.Security.SecretKey)))
	}

	if c.Dev && c.Server.PublicURL != nil && c.Server.PublicURL.Scheme == "https" {
		problems = append(problems, errors.New(
			"server: development mode refuses to run behind an https public URL — it disables protections that a public deployment needs"))
	}

	if c.Storage.Backend == "s3" {
		if c.Storage.S3Bucket == "" {
			problems = append(problems, errors.New("storage: S3 backend requires a bucket"))
		}
		if c.Storage.S3AccessKey == "" || c.Storage.S3SecretKey == "" {
			problems = append(problems, errors.New("storage: S3 backend requires credentials"))
		}
	}

	if c.OAuth.Enabled {
		if err := applyOAuthProfileDefaults(&c.OAuth); err != nil {
			problems = append(problems, err)
		}
		if c.OAuth.Provider == "" || c.OAuth.ClientID == "" || c.OAuth.ClientSecret == "" ||
			c.OAuth.AuthorizationURL == "" || c.OAuth.TokenURL == "" || c.OAuth.UserinfoURL == "" {
			problems = append(problems, errors.New("oauth: provider, client credentials, authorization URL, token URL, and userinfo URL are required when OAuth is enabled"))
		}
		for name, raw := range map[string]string{
			"authorization": c.OAuth.AuthorizationURL,
			"token":         c.OAuth.TokenURL,
			"userinfo":      c.OAuth.UserinfoURL,
		} {
			if raw == "" {
				continue
			}
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				problems = append(problems, fmt.Errorf("oauth: %s URL must be an absolute https URL", name))
			}
		}
	}

	if len(c.Server.Roles) == 0 {
		problems = append(problems, errors.New("server: at least one role must be enabled"))
	}

	if c.Observability.DevLite.APIKey != "" {
		if c.Observability.DevLite.SampleRate < 0 || c.Observability.DevLite.SampleRate > 1 {
			problems = append(problems, fmt.Errorf("observability: DEVLITE_SAMPLE_RATE must be between 0 and 1 (got %v)", c.Observability.DevLite.SampleRate))
		}
		if c.Observability.DevLite.FlushInterval <= 0 {
			problems = append(problems, errors.New("observability: DEVLITE_FLUSH_INTERVAL must be greater than zero"))
		}
		if c.Observability.DevLite.MetricsInterval <= 0 {
			problems = append(problems, errors.New("observability: DEVLITE_METRICS_INTERVAL must be greater than zero"))
		}
		if raw := strings.TrimSpace(c.Observability.DevLite.Endpoint); raw != "" {
			endpoint, err := url.Parse(raw)
			if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
				problems = append(problems, errors.New("observability: DEVLITE_ENDPOINT must be an absolute https URL"))
			}
		}
	}

	return errors.Join(problems...)
}

// Has reports whether this process runs the given role.
func (s Server) Has(role Role) bool {
	for _, candidate := range s.Roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func parseRoles(value string) ([]Role, error) {
	if value == "all" {
		return AllRoles, nil
	}

	var roles []Role
	for _, part := range strings.Split(value, ",") {
		role := Role(strings.TrimSpace(part))
		switch role {
		case RoleHTTP, RoleRealtime, RoleWorker, RoleScheduler:
			roles = append(roles, role)
		default:
			return nil, fmt.Errorf("unknown role %q (want http, realtime, worker, scheduler, or all)", part)
		}
	}
	return roles, nil
}

// ParseRoles validates the process-role list used by configuration and the
// serve command's explicit --roles override. Keeping one parser prevents the
// CLI and environment variable from accepting different deployment shapes.
func ParseRoles(value string) ([]Role, error) {
	return parseRoles(value)
}

// Redacted returns a copy safe to print in diagnostics (§11.5).
func (c Config) Redacted() Config {
	redacted := c
	redacted.Database.URL = redactURL(c.Database.URL)
	redacted.Security.SecretKey = nil
	redacted.Storage.S3AccessKey = mask(c.Storage.S3AccessKey)
	redacted.Storage.S3SecretKey = mask(c.Storage.S3SecretKey)
	redacted.Email.SMTPPassword = mask(c.Email.SMTPPassword)
	redacted.OAuth.ClientSecret = mask(c.OAuth.ClientSecret)
	redacted.Observability.DevLite.APIKey = mask(c.Observability.DevLite.APIKey)
	return redacted
}

func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	if _, hasPassword := parsed.User.Password(); hasPassword {
		parsed.User = url.UserPassword(parsed.User.Username(), "xxxxx")
	}
	return parsed.String()
}

func mask(secret string) string {
	if secret == "" {
		return ""
	}
	return "xxxxx"
}
