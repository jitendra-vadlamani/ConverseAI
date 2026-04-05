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
}

func LoadConfig() *Config {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found: %v", err)
	}

	return &Config{
		Port:                   getEnv("PORT", "8080"),
		MongoURI:               getEnv("MONGO_URI", "mongodb://localhost:27017"),
		DBName:                 getEnv("DB_NAME", "ai_chat"),
		JWTSecret:              getEnv("JWT_SECRET", "converseai"),
		MinioEndpoint:          getEnv("MINIO_ENDPOINT", "localhost:9000"),
		MinioUser:              getEnv("MINIO_ROOT_USER", "admin"),
		MinioPass:              getEnv("MINIO_ROOT_PASSWORD", "password123"),
		MinioBucket:            getEnv("MINIO_BUCKET", "converseai"),
		MinioSSL:               getEnv("MINIO_USE_SSL", "false") == "true",
		ChromaURL:              getEnv("CHROMA_URL", "http://localhost:8000"),
		EmbeddingModel:         getEnv("EMBEDDING_MODEL", "nomic-embed-text-v2-moe:latest"),
		DefaultPlannerModel:    getEnv("DEFAULT_PLANNER_MODEL", "gemma4:latest"),
		DefaultChatModel:       getEnv("DEFAULT_CHAT_MODEL", "gemma4:latest"),
		DefaultOCRModel:        getEnv("DEFAULT_OCR_MODEL", "deepseek-ocr:3b"),
		DefaultVisionModel:     getEnv("DEFAULT_VISION_MODEL", "qwen3-vl:8b"),
		DefaultCodingModel:     getEnv("DEFAULT_CODING_MODEL", "qwen3-coder:latest"),
		DefaultTranslationModel: getEnv("DEFAULT_TRANSLATION_MODEL", "translategemma:12b"),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
