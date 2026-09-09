package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go-diploma-issuer/diploma"
	"go-diploma-issuer/logging"
	"go-diploma-issuer/redis"
)

//go:generate swag init --dir ./,./models --parseInternal -o docs

// @title Go Diploma Issuer API
// @version 1.0
// @description API for verifying digital diploma extracts from DUO (Dienst Uitvoering Onderwijs, "Mijn diploma's") and issuing them as privacy-preserving Yivi credentials.
// @description The service verifies the PAdES signature DUO puts on every extract (certification signature, RFC 3161 timestamp, certificate chain to a qualified trust service on the EU Trusted Lists, DUO as signer), reads the printed data (qualification, holder, institution, date), lets the holder prove their identity with a BRP, passport, ID card or driving licence credential in the Yivi app, checks that this identity matches the holder named on the extracts and then issues one diploma credential per extract (IRMA and SD-JWT VC formats) over the IRMA protocol. Several extracts of the same holder can be uploaded and issued at once.

// @contact.name Privacy by Design Foundation

// @license.name Apache 2.0
// @license.url https://www.apache.org/licenses/LICENSE-2.0

// @BasePath /api

type Config struct {
	ServerConfig      ServerConfig `json:"server_config"`
	IrmaServerUrl     string       `json:"irma_server_url"`
	IssuerId          string       `json:"issuer_id"`
	JwtPrivateKeyPath string       `json:"jwt_private_key_path"`
	SdJwtBatchSize    uint         `json:"sd_jwt_batch_size"`
	// Validity of the issued credentials in days. Defaults to 365.
	CredentialValidityDays int                  `json:"credential_validity_days"`
	Credentials            AllCredentialConfigs `json:"credentials"`
	// The identity credentials the holder may disclose to prove who they are.
	IdentityCredentials IdentityCredentials `json:"identity_credentials"`
	// Where the trust anchors for the signature verification come from.
	Trust      diploma.TrustConfig `json:"trust"`
	Validation ValidationConfig    `json:"validation"`
	// Maximum accepted size of one uploaded PDF in bytes. Defaults to 5 MiB.
	MaxUploadSizeBytes int64 `json:"max_upload_size_bytes"`
	// Maximum number of extracts in one upload. Defaults to 10.
	MaxFiles            int                       `json:"max_files"`
	StorageType         string                    `json:"storage_type"`
	RedisConfig         redis.RedisConfig         `json:"redis_config"`
	RedisSentinelConfig redis.RedisSentinelConfig `json:"redis_sentinel_config"`
	LogLevel            string                    `json:"log_level"`
}

type CredentialConfig struct {
	FullCredential string `json:"full_credential"`
}

type AllCredentialConfigs struct {
	Diploma CredentialConfig `json:"diploma"`
}

// ValidationConfig configures the signature verification.
type ValidationConfig struct {
	// Also check the signer certificate online against DUO's OCSP responder.
	// Defaults to false: the chain, timestamp and trusted list checks run
	// offline against the loaded anchors.
	Ocsp bool `json:"ocsp"`
}

func main() {
	configPath := flag.String("config", "", "Path for the config.json to use")
	flag.Parse()

	if *configPath == "" {
		slog.Error("please provide a config path using the --config flag")
		os.Exit(1)
	}

	config, err := readConfigFile(*configPath)
	if err != nil {
		slog.Error("failed to read config file", "error", err)
		os.Exit(1)
	}

	logLevel := config.LogLevel
	if logLevel == "" {
		logLevel = "info"
	}
	logging.InitLogger(logLevel)

	slog.Info("using config", "path", *configPath)
	slog.Info("hosting on", "host", config.ServerConfig.Host, "port", config.ServerConfig.Port)

	validityDays := config.CredentialValidityDays
	if validityDays <= 0 {
		validityDays = 365
	}
	jwtCreator, err := NewIrmaJwtCreator(
		config.JwtPrivateKeyPath,
		config.IssuerId,
		config.Credentials.Diploma.FullCredential,
		config.SdJwtBatchSize,
		time.Duration(validityDays)*24*time.Hour,
		config.IdentityCredentials,
	)
	if err != nil {
		slog.Error("failed to instantiate jwt creator", "error", err)
		os.Exit(1)
	}

	sessionStorage, err := createSessionStorage(&config)
	if err != nil {
		slog.Error("failed to instantiate session storage", "error", err)
		os.Exit(1)
	}

	parser, err := diploma.NewPdfiumParser()
	if err != nil {
		slog.Error("failed to initialise pdf parser", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := parser.Close(); err != nil {
			slog.Warn("failed to close pdf parser", "error", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	trustStore, err := diploma.NewTrustStore(ctx, config.Trust, nil)
	if err != nil {
		slog.Error("failed to load trust anchors", "error", err)
		os.Exit(1)
	}
	go trustStore.RunRefresh(ctx)
	validator := diploma.NewPadesValidator(trustStore, config.Validation.Ocsp, nil)
	slog.Info("using signature verification", "trust", trustStore.Description(), "ocsp", validator.OCSP())

	serverState := ServerState{
		irmaServerURL:       config.IrmaServerUrl,
		sessionStorage:      sessionStorage,
		jwtCreator:          jwtCreator,
		validator:           validator,
		parser:              parser,
		irmaClient:          NewHttpIrmaClient(config.IrmaServerUrl, 15*time.Second),
		identityCredentials: config.IdentityCredentials,
		maxUploadSize:       config.MaxUploadSizeBytes,
		maxFiles:            config.MaxFiles,
	}

	server, err := NewServer(&serverState, config.ServerConfig)
	if err != nil {
		slog.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	if err := server.ListenAndServe(); err != nil {
		slog.Error("failed to listen and serve", "error", err)
		os.Exit(1)
	}
}

func readConfigFile(path string) (Config, error) {
	configBytes, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var config Config
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func createSessionStorage(config *Config) (SessionStorage, error) {
	switch config.StorageType {
	case "redis":
		slog.Info("Using redis session storage")
		client, err := redis.NewRedisClient(&config.RedisConfig)
		if err != nil {
			return nil, err
		}
		return NewRedisSessionStorage(client, config.RedisConfig.Namespace), nil
	case "redis_sentinel":
		slog.Info("Using redis sentinel session storage")
		client, err := redis.NewRedisSentinelClient(&config.RedisSentinelConfig)
		if err != nil {
			return nil, err
		}
		return NewRedisSessionStorage(client, config.RedisSentinelConfig.Namespace), nil
	case "memory":
		slog.Info("Using in memory session storage")
		return NewInMemorySessionStorage(), nil
	}
	return nil, fmt.Errorf("%v is not a valid storage type", config.StorageType)
}
