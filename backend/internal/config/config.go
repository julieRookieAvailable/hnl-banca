package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	APIHost             string
	APIPort             string
	DatabaseURL         string
	JWTSecret           string
	JWTAccessTTL        time.Duration
	JWTRefreshTTL       time.Duration
	TBAddress           string
	TBClusterID         uint32
	TBExternalAccountID uint64
	OpenRouterAPIKey    string
	OpenRouterModel     string
	CORSOrigin          string
	RequestTimeout      time.Duration
	SeedOnStart         bool
	SeedDataPath        string
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getDuration(key, def string) time.Duration {
	d, err := time.ParseDuration(getEnv(key, def))
	if err != nil {
		return 0
	}
	return d
}

func Load() (*Config, error) {
	cluster, err := strconv.ParseUint(getEnv("TB_CLUSTER_ID", "0"), 10, 32)
	if err != nil {
		return nil, err
	}
	external, err := strconv.ParseUint(getEnv("TB_EXTERNAL_ACCOUNT_ID", "9000001"), 10, 64)
	if err != nil {
		return nil, err
	}
	return &Config{
		APIHost:             getEnv("API_HOST", "0.0.0.0"),
		APIPort:             getEnv("API_PORT", "8080"),
		DatabaseURL:         getEnv("DATABASE_URL", "postgres://hnl:hnl_password_dev@localhost:5432/hnl_banca?sslmode=disable"),
		JWTSecret:           getEnv("JWT_SECRET", "dev-secret-cambiar-en-produccion"),
		JWTAccessTTL:        getDuration("JWT_ACCESS_TTL", "15m"),
		JWTRefreshTTL:       getDuration("JWT_REFRESH_TTL", "720h"),
		TBAddress:           getEnv("TB_ADDRESS", "localhost:3000"),
		TBClusterID:         uint32(cluster),
		TBExternalAccountID: external,
		OpenRouterAPIKey:    os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterModel:     getEnv("OPENROUTER_MODEL", "openrouter/auto"),
		CORSOrigin:          getEnv("CORS_ORIGIN", "http://localhost:5173"),
		RequestTimeout:      getDuration("REQUEST_TIMEOUT", "30s"),
		SeedOnStart:         getBool("SEED_ON_START", true),
		SeedDataPath:        getEnv("SEED_DATA_PATH", "cmd/seed/data/datos-prueba-HNL.json"),
	}, nil
}

func getBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
