package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Values that ship in the repo or in old docs. Starting with any of them means
// the deployment was never configured, so the app refuses to boot.
var knownDefaultSecrets = map[string]bool{
	"":                                 true,
	"converseai":                       true,
	"change-me":                        true,
	"your-secret-key":                  true,
	"password123":                      true,
	"converseai_db_secret_key_32bytes": true,
	"change-me-32-byte-encryption-key": true,
}

type Config struct {
	Port        string
	Environment string // "production" (default) or "development"

	MongoURI string
	DBName   string

	JWTSecret    string
	JWTTTL       time.Duration
	CookieSecure bool
	TrustProxy   bool // honour X-Forwarded-For for rate limiting

	// DBEncryptionKey encrypts new data; DBEncryptionKeysOld can still decrypt.
	DBEncryptionKey     string
	DBEncryptionKeysOld []string

	MinioEndpoint string
	MinioUser     string
	MinioPass     string
	MinioBucket   string
	MinioSSL      bool

	ChromaURL      string
	ChromaTenant   string
	ChromaDatabase string

	OllamaBaseURL string
	// MaxNumCtx caps num_ctx for every model so the KV cache fits in VRAM.
	MaxNumCtx int
	// SingleModelMode keeps at most one chat model loaded at a time.
	SingleModelMode bool

	EmbeddingModel          string
	DefaultChatModel        string
	DefaultOCRModel         string
	DefaultVisionModel      string
	DefaultTranslationModel string

	RedisURL          string // optional: enables the Redis event broker
	MaxConcurrentRuns int
	RunTimeout        time.Duration
	MaxUploadBytes    int64

	// Requests per minute allowed per client IP (login, register) or per
	// user (completions).
	LoginRatePerMin      int
	RegisterRatePerMin   int
	CompletionRatePerMin int

	OTLPEndpoint string // optional: enables OpenTelemetry trace export
}

func (c *Config) IsDevelopment() bool { return c.Environment == "development" }

// LoadConfig reads the environment (and .env if present) and validates it.
func LoadConfig() (*Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("could not read .env", "err", err)
	}

	cfg := &Config{
		Port:        getEnv("PORT", "8080"),
		Environment: getEnv("APP_ENV", "production"),

		MongoURI: getEnv("MONGO_URI", "mongodb://converseai-db:27017"),
		DBName:   getEnv("DB_NAME", "ai_chat"),

		JWTSecret:    os.Getenv("JWT_SECRET"),
		JWTTTL:       getDuration("JWT_TTL", 24*time.Hour),
		CookieSecure: getBool("COOKIE_SECURE", true),
		TrustProxy:   getBool("TRUST_PROXY", false),

		DBEncryptionKey:     os.Getenv("DB_ENCRYPTION_KEY"),
		DBEncryptionKeysOld: splitList(os.Getenv("DB_ENCRYPTION_KEYS_OLD")),

		MinioEndpoint: getEnv("MINIO_ENDPOINT", "converseai-storage:9000"),
		MinioUser:     os.Getenv("MINIO_ROOT_USER"),
		MinioPass:     os.Getenv("MINIO_ROOT_PASSWORD"),
		MinioBucket:   getEnv("MINIO_BUCKET", "converseai"),
		MinioSSL:      getBool("MINIO_USE_SSL", false),

		ChromaURL:      strings.TrimSuffix(getEnv("CHROMA_URL", "http://converseai-vector:8000"), "/"),
		ChromaTenant:   getEnv("CHROMA_TENANT", "default_tenant"),
		ChromaDatabase: getEnv("CHROMA_DATABASE", "default_database"),

		OllamaBaseURL:   strings.TrimSuffix(getEnv("OLLAMA_BASE_URL", "http://localhost:11434"), "/"),
		MaxNumCtx:       getInt("MAX_NUM_CTX", 8192),
		SingleModelMode: getBool("SINGLE_MODEL_MODE", true),

		EmbeddingModel:          getEnv("EMBEDDING_MODEL", "nomic-embed-text-v2-moe:latest"),
		DefaultChatModel:        getEnv("DEFAULT_CHAT_MODEL", "gemma4:latest"),
		DefaultOCRModel:         getEnv("DEFAULT_OCR_MODEL", "deepseek-ocr:3b"),
		DefaultVisionModel:      getEnv("DEFAULT_VISION_MODEL", "qwen3-vl:8b"),
		DefaultTranslationModel: getEnv("DEFAULT_TRANSLATION_MODEL", "translategemma:12b"),

		RedisURL:          os.Getenv("REDIS_URL"),
		MaxConcurrentRuns: getInt("MAX_CONCURRENT_RUNS", 2),
		RunTimeout:        getDuration("RUN_TIMEOUT", 15*time.Minute),
		MaxUploadBytes:    int64(getInt("MAX_UPLOAD_MB", 25)) << 20,

		LoginRatePerMin:      getInt("LOGIN_RATE_PER_MIN", 10),
		RegisterRatePerMin:   getInt("REGISTER_RATE_PER_MIN", 5),
		CompletionRatePerMin: getInt("COMPLETION_RATE_PER_MIN", 20),

		OTLPEndpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate fails on missing or default secrets so a misconfigured deployment
// never starts with forgeable sessions or a publicly known encryption key.
func (c *Config) Validate() error {
	var errs []error
	if knownDefaultSecrets[c.JWTSecret] {
		errs = append(errs, errors.New("JWT_SECRET is empty or a known default; generate one with `openssl rand -hex 32`"))
	} else if len(c.JWTSecret) < 32 {
		errs = append(errs, fmt.Errorf("JWT_SECRET must be at least 32 characters (got %d)", len(c.JWTSecret)))
	}
	if err := validateEncryptionKey("DB_ENCRYPTION_KEY", c.DBEncryptionKey); err != nil {
		errs = append(errs, err)
	}
	for i, k := range c.DBEncryptionKeysOld {
		if len(k) != 32 {
			errs = append(errs, fmt.Errorf("DB_ENCRYPTION_KEYS_OLD entry %d must be exactly 32 bytes", i+1))
		}
	}
	if c.MinioUser == "" || knownDefaultSecrets[c.MinioPass] {
		errs = append(errs, errors.New("MINIO_ROOT_USER and MINIO_ROOT_PASSWORD must be set to non-default values"))
	}
	if c.MaxNumCtx < 2048 {
		errs = append(errs, errors.New("MAX_NUM_CTX must be at least 2048"))
	}
	if c.LoginRatePerMin < 1 || c.RegisterRatePerMin < 1 || c.CompletionRatePerMin < 1 {
		errs = append(errs, errors.New("rate limits must be at least 1 per minute"))
	}
	if c.MaxConcurrentRuns < 1 {
		errs = append(errs, errors.New("MAX_CONCURRENT_RUNS must be at least 1"))
	}
	if c.EmbeddingModel == "" || c.DefaultChatModel == "" {
		errs = append(errs, errors.New("EMBEDDING_MODEL and DEFAULT_CHAT_MODEL must not be empty"))
	}
	return errors.Join(errs...)
}

func validateEncryptionKey(name, key string) error {
	if knownDefaultSecrets[key] {
		return fmt.Errorf("%s is empty or a known default; generate one with `openssl rand -hex 16` (32 hex chars)", name)
	}
	if len(key) != 32 {
		return fmt.Errorf("%s must be exactly 32 bytes for AES-256 (got %d)", name, len(key))
	}
	return nil
}

// getEnv treats an empty value the same as an unset one, so compose files that
// pass `KEY=${KEY}` don't silently override defaults with "".
func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getBool(key string, fallback bool) bool {
	v, err := strconv.ParseBool(getEnv(key, strconv.FormatBool(fallback)))
	if err != nil {
		return fallback
	}
	return v
}

func getInt(key string, fallback int) int {
	v, err := strconv.Atoi(getEnv(key, strconv.Itoa(fallback)))
	if err != nil {
		return fallback
	}
	return v
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v, err := time.ParseDuration(getEnv(key, fallback.String()))
	if err != nil {
		return fallback
	}
	return v
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
