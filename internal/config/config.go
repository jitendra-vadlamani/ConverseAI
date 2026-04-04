package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port          string
	MongoURI      string
	DBName        string
	JWTSecret     string
	MinioEndpoint string
	MinioUser     string
	MinioPass     string
	MinioBucket    string
	MinioSSL       bool
	ChromaURL      string
	EmbeddingModel string
}

func LoadConfig() *Config {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found: %v", err)
	}

	return &Config{
		Port:           getEnv("PORT", "8080"),
		MongoURI:       getEnv("MONGO_URI", "mongodb://localhost:27017"),
		DBName:         getEnv("DB_NAME", "ai_chat"), // Restored from earlier context
		JWTSecret:      getEnv("JWT_SECRET", "converseai"),
		MinioEndpoint:  getEnv("MINIO_ENDPOINT", "localhost:9000"),
		MinioUser:      getEnv("MINIO_ROOT_USER", "admin"),
		MinioPass:      getEnv("MINIO_ROOT_PASSWORD", "password123"),
		MinioBucket:    getEnv("MINIO_BUCKET", "converseai"),
		MinioSSL:       getEnv("MINIO_USE_SSL", "false") == "true",
		ChromaURL:      getEnv("CHROMA_URL", "http://localhost:8000"),
		EmbeddingModel: getEnv("EMBEDDING_MODEL", "nomic-embed-text"),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
