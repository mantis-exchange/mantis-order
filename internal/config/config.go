package config

import "os"

type Config struct {
	Port               string
	DBURL              string
	MatchingEngineAddr string
	KafkaBrokers       string
}

func Load() *Config {
	return &Config{
		Port:               getEnv("PORT", "50052"),
		DBURL:              getEnv("DB_URL", "postgres://mantis:mantis@localhost:5432/mantis_order?sslmode=disable"),
		MatchingEngineAddr: getEnv("MATCHING_ENGINE_ADDR", "localhost:50051"),
		KafkaBrokers:       getEnv("KAFKA_BROKERS", "localhost:9092"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
