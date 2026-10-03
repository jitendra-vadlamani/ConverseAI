package config

import (
	"strings"
	"testing"
)

func valid() *Config {
	return &Config{
		JWTSecret: strings.Repeat("s", 40), DBEncryptionKey: "0123456789abcdef0123456789abcdef",
		MinioUser: "converse", MinioPass: "a-real-password", MaxNumCtx: 8192, MaxConcurrentRuns: 1,
		EmbeddingModel: "e", DefaultChatModel: "c",
	}
}

func TestValidateAcceptsRealSecrets(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsDefaultsAndEmpty(t *testing.T) {
	cases := map[string]func(*Config){
		"empty jwt":      func(c *Config) { c.JWTSecret = "" },
		"default jwt":    func(c *Config) { c.JWTSecret = "converseai" },
		"short jwt":      func(c *Config) { c.JWTSecret = "short-but-not-default" },
		"empty db key":   func(c *Config) { c.DBEncryptionKey = "" },
		"default db key": func(c *Config) { c.DBEncryptionKey = "converseai_db_secret_key_32bytes" },
		"wrong key size": func(c *Config) { c.DBEncryptionKey = "too-short" },
		"default minio":  func(c *Config) { c.MinioPass = "password123" },
		"bad old key":    func(c *Config) { c.DBEncryptionKeysOld = []string{"x"} },
		"tiny num_ctx":   func(c *Config) { c.MaxNumCtx = 512 },
		"no chat model":  func(c *Config) { c.DefaultChatModel = "" },
	}
	for name, mutate := range cases {
		c := valid()
		mutate(c)
		if c.Validate() == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestEmptyEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("EMBEDDING_MODEL", "")
	if getEnv("EMBEDDING_MODEL", "fallback") != "fallback" {
		t.Fatal("an empty env var must not override the default")
	}
}
