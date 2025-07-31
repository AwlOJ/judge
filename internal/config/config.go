package config

import (
	"fmt"
	"os"
)

// Config holds all configuration loaded from environment variables.
type Config struct {
	RedisURL          string
	RedisQueueName    string
	MongoURI          string
	MongoDBName       string
	InternalApiUrl    string // URL for the callback API
	InternalApiSecret string // Secret for the callback API
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		RedisURL:          os.Getenv("REDIS_URL"),
		RedisQueueName:    os.Getenv("REDIS_QUEUE_NAME"),
		MongoURI:          os.Getenv("MONGO_URI"),
		MongoDBName:       os.Getenv("MONGO_DB_NAME"),
		InternalApiUrl:    os.Getenv("INTERNAL_API_URL"),
		InternalApiSecret: os.Getenv("INTERNAL_API_SECRET"),
	}

	if cfg.MongoURI == "" {
		return nil, fmt.Errorf("MONGO_URI environment variable not set")
	}
	if cfg.MongoDBName == "" {
		cfg.MongoDBName = "judger" // Default value
	}
	if cfg.RedisURL == "" {
		return nil, fmt.Errorf("REDIS_URL environment variable not set")
	}
	if cfg.RedisQueueName == "" {
		cfg.RedisQueueName = "submission_queue" // Default value
	}
	if cfg.InternalApiUrl == "" {
		return nil, fmt.Errorf("INTERNAL_API_URL environment variable not set")
	}
	if cfg.InternalApiSecret == "" {
		return nil, fmt.Errorf("INTERNAL_API_SECRET environment variable not set")
	}

	return cfg, nil
}


// Language defines the compilation and execution properties for a language.
type Language struct {
	SourceFileName     string `json:"sourceFileName"`
	ExecutableFileName string `json:"executableFileName"`
	CompileCmd         string `json:"compileCmd,omitempty"`
}

// LoadLanguageConfig loads language definitions.
func LoadLanguageConfig() (map[string]Language, error) {
	languages := make(map[string]Language)
	
	languages["cpp"] = Language{
		SourceFileName:     "main.cpp",
		ExecutableFileName: "main",
		CompileCmd:         "g++ main.cpp -o main -O2 -std=c++17",
	}
	
	return languages, nil
}
