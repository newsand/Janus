package config

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	long := strings.Repeat("a", MinSecretLength)
	short := strings.Repeat("a", MinSecretLength-1)

	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"dev accepts defaults", Config{DevEnv: true, JWTSecret: "change-me-in-production"}, false},
		{"dev accepts empty keys", Config{DevEnv: true, JWTSecret: "x"}, false},
		{"prod ok", Config{JWTSecret: long, ServiceKeys: []string{long, long}}, false},
		{"prod default jwt", Config{JWTSecret: "change-me-in-production", ServiceKeys: []string{long}}, true},
		{"prod short jwt", Config{JWTSecret: short, ServiceKeys: []string{long}}, true},
		{"prod empty jwt", Config{ServiceKeys: []string{long}}, true},
		{"prod no keys", Config{JWTSecret: long}, true},
		{"prod example keys", Config{JWTSecret: long, ServiceKeys: []string{"key1", "key2"}}, true},
		{"prod one short key among long", Config{JWTSecret: long, ServiceKeys: []string{long, short}}, true},
	}
	for _, c := range cases {
		err := c.cfg.Validate()
		if (err != nil) != c.wantErr {
			t.Errorf("%s: wantErr=%v got %v", c.name, c.wantErr, err)
		}
	}
}
