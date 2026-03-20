package config

import "os"

type Config struct {
	Port               string
	DBURL              string
	MatchingEngineAddr string
	KafkaBrokers       string
	AccountServiceAddr string
	RiskServiceAddr    string
	MakerFeePct        string
	TakerFeePct        string
}

func Load() *Config {
	return &Config{
		Port:               getEnv("PORT", "50052"),
		DBURL:              getEnv("DB_URL", "postgres://mantis:mantis@localhost:5432/mantis_order?sslmode=disable"),
		MatchingEngineAddr: getEnv("MATCHING_ENGINE_ADDR", "localhost:50051"),
		KafkaBrokers:       getEnv("KAFKA_BROKERS", "localhost:9092"),
		AccountServiceAddr: getEnv("ACCOUNT_SERVICE_ADDR", "http://localhost:50053"),
		RiskServiceAddr:    getEnv("RISK_SERVICE_ADDR", "http://localhost:50055"),
		MakerFeePct:        getEnv("MAKER_FEE_PCT", "0.001"),  // 0.1%
		TakerFeePct:        getEnv("TAKER_FEE_PCT", "0.002"),  // 0.2%
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
