package config

import (
	"os"
)

type Config struct {
	Port      string
	MongoURI  string
	DBName    string
	JWTSecret string
}

func LoadConfig() *Config {
	return &Config{
		Port:      getEnv("PORT", "8080"),
		MongoURI:  getEnv("MONGO_URI", "mongodb://localhost:27017"),
		DBName:    getEnv("DB_NAME", "ai_chat"),
		JWTSecret: getEnv("JWT_SECRET", "super-secret-random-key-change-me"),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
