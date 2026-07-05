package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                   string
	MongoURI               string
	DBName                 string
	JWTSecret              string
	MinioEndpoint          string
	MinioUser              string
	MinioPass              string
	MinioBucket            string
	MinioSSL               bool
	ChromaURL              string
	EmbeddingModel         string
	DefaultPlannerModel    string
	DefaultChatModel       string
	DefaultOCRModel        string
	DefaultVisionModel     string
	DefaultCodingModel     string
	DefaultTranslationModel string
	DBEncryptionKey         string
	LLMBackend              string
	LLMBaseURL              string
	LLMModel                string
	PostgresURI             string
}

func LoadConfig() *Config {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found: %v", err)
	}

	cfg := &Config{
		Port:                   getEnv("PORT", "8080"),
		MongoURI:               getEnv("MONGO_URI", "mongodb://converseai-db:27017"),
		DBName:                 getEnv("DB_NAME", "ai_chat"),
		JWTSecret:              getEnv("JWT_SECRET", "converseai"),
		MinioEndpoint:          getEnv("MINIO_ENDPOINT", "converseai-storage:9000"),
		MinioUser:              getEnv("MINIO_ROOT_USER", "admin"),
		MinioPass:              getEnv("MINIO_ROOT_PASSWORD", "password123"),
		MinioBucket:            getEnv("MINIO_BUCKET", "converseai"),
		MinioSSL:               getEnv("MINIO_USE_SSL", "false") == "true",
		ChromaURL:              getEnv("CHROMA_URL", "http://converseai-vector:8000"),
		EmbeddingModel:         getEnv("EMBEDDING_MODEL", "gemma4:e4b"),
		DefaultPlannerModel:    getEnv("DEFAULT_PLANNER_MODEL", "gemma4:e4b"),
		DefaultChatModel:       getEnv("DEFAULT_CHAT_MODEL", "gemma4:e4b"),
		DefaultOCRModel:        getEnv("DEFAULT_OCR_MODEL", "deepseek-ocr:3b"),
		DefaultVisionModel:     getEnv("DEFAULT_VISION_MODEL", "qwen3-vl:8b"),
		DefaultCodingModel:     getEnv("DEFAULT_CODING_MODEL", "qwen3-coder:latest"),
		DefaultTranslationModel: getEnv("DEFAULT_TRANSLATION_MODEL", "translategemma:12b"),
		DBEncryptionKey:         getEnv("DB_ENCRYPTION_KEY", "converseai_db_secret_key_32bytes"),
		LLMBackend:              getEnv("LLM_BACKEND", "ollama"),
		LLMBaseURL:              getEnv("LLM_BASE_URL", getEnv("OLLAMA_BASE_URL", "http://localhost:11434")),
		LLMModel:                getEnv("LLM_MODEL", getEnv("DEFAULT_CHAT_MODEL", "gemma4-cos")),
		PostgresURI:             getEnv("POSTGRES_URI", "postgres://postgres:postgres@localhost:5432/converseai?sslmode=disable"),
	}

	if cfg.JWTSecret == "converseai" {
		log.Println("WARNING: Using default JWT secret. Set JWT_SECRET environment variable for production!")
	}
	
	// Ensure DB Encryption Key is strictly 32 bytes (AES-256)
	if len(cfg.DBEncryptionKey) != 32 {
		log.Fatalf("ERROR: DB_ENCRYPTION_KEY must be exactly 32 bytes long for AES-256. Current length: %d", len(cfg.DBEncryptionKey))
	}
	if cfg.DBEncryptionKey == "converseai_db_secret_key_32bytes" {
		log.Println("WARNING: Using default DB Encryption Key. Set DB_ENCRYPTION_KEY environment variable for production!")
	}

	return cfg
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
