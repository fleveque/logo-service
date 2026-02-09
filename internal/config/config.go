// Package config handles application configuration using Viper.
// Viper supports YAML files, environment variables, and defaults — merged in priority order.
// Go convention: configuration is loaded into structs, not accessed as raw key-value pairs.
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config is the root configuration struct. Nested structs organize related settings.
// `mapstructure` tags tell Viper how to map YAML/env keys to struct fields.
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Storage  StorageConfig  `mapstructure:"storage"`
	Auth     AuthConfig     `mapstructure:"auth"`
	CORS     CORSConfig     `mapstructure:"cors"`
	LLM      LLMConfig      `mapstructure:"llm"`
	GitHub   GitHubConfig   `mapstructure:"github"`
	RateLimit RateLimitConfig `mapstructure:"rate_limit"`
	Log      LogConfig      `mapstructure:"log"`
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type StorageConfig struct {
	DatabasePath string `mapstructure:"database_path"`
	LogoDir      string `mapstructure:"logo_dir"`
}

type AuthConfig struct {
	APIKeys   []string `mapstructure:"api_keys"`
	AdminKeys []string `mapstructure:"admin_keys"`
}

type CORSConfig struct {
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

type LLMConfig struct {
	// ProviderOrder controls which LLM providers are used and in what order.
	// First provider is primary, rest are fallbacks. Example: ["anthropic", "openai"]
	ProviderOrder []string        `mapstructure:"provider_order"`
	Anthropic     AnthropicConfig `mapstructure:"anthropic"`
	OpenAI        OpenAIConfig    `mapstructure:"openai"`
	RatePerMinute int             `mapstructure:"rate_per_minute"`
}

type AnthropicConfig struct {
	APIKey string `mapstructure:"api_key"`
	Model  string `mapstructure:"model"`
}

type OpenAIConfig struct {
	APIKey string `mapstructure:"api_key"`
	Model  string `mapstructure:"model"`
}

type GitHubConfig struct {
	Repos []string `mapstructure:"repos"`
}

type RateLimitConfig struct {
	RequestsPerSecond float64 `mapstructure:"requests_per_second"`
	Burst             int     `mapstructure:"burst"`
}

type LogConfig struct {
	Level string `mapstructure:"level"`
}

// Load reads configuration from a YAML file and environment variables.
// In Go, functions return errors as the last return value — callers must check them.
// This pattern replaces try/catch: if err != nil { handle it }.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults — these apply when neither file nor env provides a value
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("storage.database_path", "./storage/logo-service.db")
	v.SetDefault("storage.logo_dir", "./storage/logos")
	v.SetDefault("cors.allowed_origins", []string{"http://localhost:3000", "http://localhost:3036"})
	v.SetDefault("llm.provider_order", []string{"anthropic", "openai"})
	v.SetDefault("llm.anthropic.model", "claude-sonnet-4-5-20250929")
	v.SetDefault("llm.openai.model", "gpt-4o")
	v.SetDefault("llm.rate_per_minute", 10)
	v.SetDefault("github.repos", []string{
		"davidepalazzo/ticker-logos",
		"nvstly/icons",
	})
	v.SetDefault("rate_limit.requests_per_second", 10)
	v.SetDefault("rate_limit.burst", 20)
	v.SetDefault("log.level", "info")

	// Read from YAML config file if provided
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
	}

	// Read config file (ignore "not found" — defaults + env are enough)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok && configPath != "" {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	// Environment variables override everything.
	// LOGO_ prefix + nested keys: LOGO_SERVER_PORT=9090 → server.port=9090
	v.SetEnvPrefix("LOGO")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Unmarshal into our Config struct
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	return &cfg, nil
}

// Address returns the listen address string like "0.0.0.0:8080".
// This is a method on ServerConfig — Go attaches methods to types via receiver syntax.
func (s ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}
