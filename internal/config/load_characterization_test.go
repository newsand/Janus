package config

// T30 — characterization tests: they pin the CURRENT behavior of Load, the
// env parsers, parseServiceKeys, Validate and Get (decision #8). They pass
// against the code as it is; a failure later means the behavior changed.

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// configEnvKeys lists every variable Load reads.
var configEnvKeys = []string{
	"PORT", "DATABASE_URL", "LOG_LEVEL", "JWT_SECRET", "SERVICE_KEYS",
	"ACCESS_TOKEN_TTL", "REFRESH_TOKEN_TTL", "MAGIC_LINK_TTL", "INVITE_TTL",
	"RECOVER_TTL", "RATE_LIMIT_REQUESTS", "RATE_LIMIT_WINDOW",
	"LOCKOUT_THRESHOLD", "LOCKOUT_DURATION", "MAILER_STUB", "DEV_ENV",
}

// isolateConfig clears every env var Load reads (empty = unset for getEnv)
// and resets the singleton, restoring it after the test.
func isolateConfig(t *testing.T) {
	t.Helper()
	for _, k := range configEnvKeys {
		t.Setenv(k, "")
	}
	saved := cfg
	cfg = nil
	t.Cleanup(func() { cfg = saved })
}

func TestT30LoadDefaultsWhenEnvEmpty(t *testing.T) {
	isolateConfig(t)

	got := Load()

	want := &Config{
		Port:              "8080",
		DatabaseURL:       "postgres://postgres:postgres@localhost:5432/janus?sslmode=disable",
		LogLevel:          "info",
		JWTSecret:         "change-me-in-production",
		ServiceKeys:       nil,
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   14 * 24 * time.Hour,
		MagicLinkTTL:      15 * time.Minute,
		InviteTTL:         7 * 24 * time.Hour,
		RecoverTTL:        time.Hour,
		RateLimitRequests: 5,
		RateLimitWindow:   time.Minute,
		LockoutThreshold:  5,
		LockoutDuration:   15 * time.Minute,
		MailerStub:        true,
		DevEnv:            false,
		Version:           "alfa",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults:\n got  %+v\n want %+v", got, want)
	}
}

func TestT30LoadDefaultsFailValidateOutsideDev(t *testing.T) {
	isolateConfig(t)

	err := Load().Validate()

	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("default config outside dev: want JWT_SECRET error, got %v", err)
	}
}

func TestT30LoadReadsValidEnvValues(t *testing.T) {
	isolateConfig(t)
	t.Setenv("PORT", "9999")
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("JWT_SECRET", "s3cr3t")
	t.Setenv("SERVICE_KEYS", "k1,k2")
	t.Setenv("ACCESS_TOKEN_TTL", "30s")
	t.Setenv("REFRESH_TOKEN_TTL", "2h")
	t.Setenv("MAGIC_LINK_TTL", "5m")
	t.Setenv("INVITE_TTL", "48h")
	t.Setenv("RECOVER_TTL", "90m")
	t.Setenv("RATE_LIMIT_REQUESTS", "42")
	t.Setenv("RATE_LIMIT_WINDOW", "10s")
	t.Setenv("LOCKOUT_THRESHOLD", "0")
	t.Setenv("LOCKOUT_DURATION", "1h30m")
	t.Setenv("MAILER_STUB", "false")
	t.Setenv("DEV_ENV", "true")

	got := Load()

	want := &Config{
		Port:              "9999",
		DatabaseURL:       "postgres://x",
		LogLevel:          "debug",
		JWTSecret:         "s3cr3t",
		ServiceKeys:       []string{"k1", "k2"},
		AccessTokenTTL:    30 * time.Second,
		RefreshTokenTTL:   2 * time.Hour,
		MagicLinkTTL:      5 * time.Minute,
		InviteTTL:         48 * time.Hour,
		RecoverTTL:        90 * time.Minute,
		RateLimitRequests: 42,
		RateLimitWindow:   10 * time.Second,
		LockoutThreshold:  0,
		LockoutDuration:   90 * time.Minute,
		MailerStub:        false,
		DevEnv:            true,
		Version:           "alfa",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("env values:\n got  %+v\n want %+v", got, want)
	}
}

func TestT30LoadInvalidValuesFallBackToDefaults(t *testing.T) {
	isolateConfig(t)
	t.Setenv("ACCESS_TOKEN_TTL", "15")        // no unit → invalid duration
	t.Setenv("REFRESH_TOKEN_TTL", "two days") // garbage
	t.Setenv("RATE_LIMIT_REQUESTS", "5.5")    // not an int
	t.Setenv("LOCKOUT_THRESHOLD", "abc")
	t.Setenv("MAILER_STUB", "nope") // not a bool
	t.Setenv("DEV_ENV", "yes")      // ParseBool rejects "yes"

	got := Load()

	if got.AccessTokenTTL != 15*time.Minute || got.RefreshTokenTTL != 14*24*time.Hour {
		t.Errorf("durations: got access=%v refresh=%v", got.AccessTokenTTL, got.RefreshTokenTTL)
	}
	if got.RateLimitRequests != 5 || got.LockoutThreshold != 5 {
		t.Errorf("ints: got rate=%d lockout=%d", got.RateLimitRequests, got.LockoutThreshold)
	}
	if got.MailerStub != true || got.DevEnv != false {
		t.Errorf("bools: got mailer=%v dev=%v", got.MailerStub, got.DevEnv)
	}
}

func TestT30GetIntEnv(t *testing.T) {
	cases := []struct {
		name, val string
		want      int
	}{
		{"empty uses fallback", "", 7},
		{"valid", "12", 12},
		{"negative accepted", "-3", -3},
		{"zero accepted", "0", 0},
		{"float falls back", "1.5", 7},
		{"spaces fall back", " 12 ", 7},
		{"text falls back", "twelve", 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("T30_INT", c.val)
			if got := getIntEnv("T30_INT", 7); got != c.want {
				t.Errorf("getIntEnv(%q) = %d, want %d", c.val, got, c.want)
			}
		})
	}
}

func TestT30GetBoolEnv(t *testing.T) {
	cases := []struct {
		name, val string
		fallback  bool
		want      bool
	}{
		{"empty uses fallback true", "", true, true},
		{"empty uses fallback false", "", false, false},
		{"true", "true", false, true},
		{"1 is true", "1", false, true},
		{"TRUE is true", "TRUE", false, true},
		{"false", "false", true, false},
		{"0 is false", "0", true, false},
		{"yes falls back", "yes", false, false},
		{"on falls back", "on", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("T30_BOOL", c.val)
			if got := getBoolEnv("T30_BOOL", c.fallback); got != c.want {
				t.Errorf("getBoolEnv(%q, %v) = %v, want %v", c.val, c.fallback, got, c.want)
			}
		})
	}
}

func TestT30GetDurationEnv(t *testing.T) {
	cases := []struct {
		name, val string
		want      time.Duration
	}{
		{"empty uses fallback", "", time.Minute},
		{"seconds", "45s", 45 * time.Second},
		{"compound", "1h15m", 75 * time.Minute},
		{"zero literal", "0", 0},
		{"negative accepted", "-1m", -time.Minute},
		{"no unit falls back", "10", time.Minute},
		{"days unit unsupported falls back", "7d", time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("T30_DUR", c.val)
			if got := getDurationEnv("T30_DUR", time.Minute); got != c.want {
				t.Errorf("getDurationEnv(%q) = %v, want %v", c.val, got, c.want)
			}
		})
	}
}

func TestT30GetEnv(t *testing.T) {
	t.Setenv("T30_STR", "")
	if got := getEnv("T30_STR", "fb"); got != "fb" {
		t.Errorf("empty: got %q, want fallback", got)
	}
	t.Setenv("T30_STR", "  v  ")
	if got := getEnv("T30_STR", "fb"); got != "  v  " {
		t.Errorf("value is not trimmed: got %q", got)
	}
}

func TestT30ParseServiceKeys(t *testing.T) {
	cases := []struct {
		name, in string
		want     []string
	}{
		{"empty is nil", "", nil},
		{"single", "k1", []string{"k1"}},
		{"comma separated", "k1,k2,k3", []string{"k1", "k2", "k3"}},
		{"spaces trimmed", "  k1 , k2\t,\nk3 ", []string{"k1", "k2", "k3"}},
		{"empty entries dropped", ",k1,,k2,", []string{"k1", "k2"}},
		{"only separators is empty non-nil", " , ,, ", []string{}},
		{"inner spaces kept", "a b,c", []string{"a b", "c"}},
		{"duplicates kept", "k,k", []string{"k", "k"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseServiceKeys(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("parseServiceKeys(%q) = %#v, want %#v", c.in, got, c.want)
			}
		})
	}
}

func TestT30ValidateErrorsAndBoundaries(t *testing.T) {
	long := strings.Repeat("a", MinSecretLength)
	short := strings.Repeat("a", MinSecretLength-1)

	if MinSecretLength != 45 {
		t.Fatalf("MinSecretLength = %d, want 45", MinSecretLength)
	}

	cases := []struct {
		name    string
		cfg     Config
		wantSub string // "" = no error
	}{
		{"exactly min length ok", Config{JWTSecret: long, ServiceKeys: []string{long}}, ""},
		{"jwt one char short", Config{JWTSecret: short, ServiceKeys: []string{long}}, "JWT_SECRET must have at least 45 characters (got 44)"},
		{"jwt missing", Config{ServiceKeys: []string{long}}, "JWT_SECRET must have at least 45 characters (got 0)"},
		{"jwt checked before keys", Config{JWTSecret: short}, "JWT_SECRET"},
		{"nil keys", Config{JWTSecret: long}, "SERVICE_KEYS must not be empty"},
		{"empty non-nil keys", Config{JWTSecret: long, ServiceKeys: []string{}}, "SERVICE_KEYS must not be empty"},
		{"second key short reports #2", Config{JWTSecret: long, ServiceKeys: []string{long, short}}, "SERVICE_KEYS entry #2 must have at least 45 characters (got 44)"},
		{"first short key wins", Config{JWTSecret: long, ServiceKeys: []string{"k", short}}, "SERVICE_KEYS entry #1 must have at least 45 characters (got 1)"},
		{"dev skips everything", Config{DevEnv: true}, ""},
		{"dev skips short keys", Config{DevEnv: true, JWTSecret: "x", ServiceKeys: []string{"k"}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.cfg.Validate()
			if c.wantSub == "" {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantSub) {
				t.Fatalf("want error containing %q, got %v", c.wantSub, err)
			}
			if !strings.Contains(err.Error(), "DEV_ENV=true only for local testing") {
				t.Errorf("error lacks DEV_ENV hint: %v", err)
			}
		})
	}
}

func TestT30ValidateFromEnvDevPathAndProdPath(t *testing.T) {
	long := strings.Repeat("b", MinSecretLength)

	t.Run("DEV_ENV=true with defaults is valid", func(t *testing.T) {
		isolateConfig(t)
		t.Setenv("DEV_ENV", "true")
		if err := Load().Validate(); err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})
	t.Run("strong secrets from env are valid outside dev", func(t *testing.T) {
		isolateConfig(t)
		t.Setenv("JWT_SECRET", long)
		t.Setenv("SERVICE_KEYS", " "+long+" , "+long+",")
		if err := Load().Validate(); err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})
	t.Run("separator-only SERVICE_KEYS is empty outside dev", func(t *testing.T) {
		isolateConfig(t)
		t.Setenv("JWT_SECRET", long)
		t.Setenv("SERVICE_KEYS", " , ")
		err := Load().Validate()
		if err == nil || !strings.Contains(err.Error(), "SERVICE_KEYS must not be empty") {
			t.Fatalf("want empty-keys error, got %v", err)
		}
	})
}

func TestT30LoadAndGetAreSingleton(t *testing.T) {
	isolateConfig(t)
	t.Setenv("PORT", "1111")

	first := Get() // cfg nil → Get loads
	if first.Port != "1111" {
		t.Fatalf("Get on empty singleton: Port = %q, want 1111", first.Port)
	}

	t.Setenv("PORT", "2222")
	if again := Load(); again != first || again.Port != "1111" {
		t.Errorf("Load after first load must return the cached pointer (Port 1111), got %p Port %q", again, again.Port)
	}
	if again := Get(); again != first {
		t.Errorf("Get must return the cached pointer")
	}
}
