package config

import (
	"flag"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v6"
)

type ServerConfig struct {
	Address       string
	StoreInterval time.Duration
	FileStorage   string
	Restore       bool
}

const (
	defaultAddress       = "localhost:8080"
	defaultStoreInterval = 300 * time.Second
	defaultFileStorage   = "metrics-db.json"
	defaultRestore       = false
)

func loadServerConfig() (*ServerConfig, error) {
	cfg := &ServerConfig{
		Address:       defaultAddress,
		StoreInterval: defaultStoreInterval,
		FileStorage:   defaultFileStorage,
		Restore:       defaultRestore,
	}

	// Загрузка из env vars
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	var (
		flagAddress  string
		flagInterval int
		flagFile     string
		flagRestore  bool
	)

	flag.StringVar(&flagAddress, "a", "", "Server address")
	flag.IntVar(&flagInterval, "i", -1, "Store interval in seconds (0 = sync write)")
	flag.StringVar(&flagFile, "f", "", "File path for storage")
	flag.BoolVar(&flagRestore, "r", false, "Restore from storage file on start")

	flag.Parse()

	if envAddr := os.Getenv("ADDRESS"); envAddr == "" && flagAddress != "" {
		cfg.Address = flagAddress
	}

	if envInterval := os.Getenv("STORE_INTERVAL"); envInterval == "" {
		if flagInterval >= 0 {
			cfg.StoreInterval = time.Duration(flagInterval) * time.Second
		}
	} else {
		if val, err := strconv.Atoi(envInterval); err == nil {
			cfg.StoreInterval = time.Duration(val) * time.Second
		}
	}

	if envFile := os.Getenv("FILE_STORAGE_PATH"); envFile == "" && flagFile != "" {
		cfg.FileStorage = flagFile
	} else if envFile != "" {
		cfg.FileStorage = envFile
	}

	if envRestore := os.Getenv("RESTORE"); envRestore == "" {
		cfg.Restore = flagRestore
	} else {
		r := strings.ToLower(envRestore)
		cfg.Restore = r == "true" || r == "1"
	}

	return cfg, nil
}
