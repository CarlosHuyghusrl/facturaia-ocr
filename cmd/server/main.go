package main

import (
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/facturaIA/invoice-ocr-service/api"
	"github.com/facturaIA/invoice-ocr-service/internal/auth"
	"github.com/facturaIA/invoice-ocr-service/internal/db"
	"github.com/facturaIA/invoice-ocr-service/internal/logging"
	"github.com/facturaIA/invoice-ocr-service/internal/models"
	"github.com/facturaIA/invoice-ocr-service/internal/storage"
	"github.com/facturaIA/invoice-ocr-service/internal/version"
	"gopkg.in/yaml.v3"
)

func main() {
	// Initialize structured logging (must be first — controls slog default handler).
	logging.Init()

	// Initialize JWT
	if err := auth.Init(); err != nil {
		log.Fatalf("Failed to initialize auth: %v", err)
	}
	slog.Info("JWT authentication initialized")

	// Initialize database connection pool with retry
	{
		maxRetries := 5
		var dbErr error
		for attempt := 1; attempt <= maxRetries; attempt++ {
			if dbErr = db.Init(); dbErr == nil {
				defer db.Close()
				slog.Info("Database connection pool initialized")
				break
			}
			if attempt < maxRetries {
				delay := time.Duration(1<<uint(attempt)) * time.Second // 2s, 4s, 8s, 16s
				slog.Warn("Database connection attempt failed",
					slog.Int("attempt", attempt),
					slog.Int("max_retries", maxRetries),
					slog.String("error", dbErr.Error()),
					slog.String("retry_in", delay.String()),
				)
				time.Sleep(delay)
			}
		}
		if dbErr != nil {
			slog.Warn("Database not available — running in OCR-only mode",
				slog.Int("attempts", maxRetries),
				slog.String("error", dbErr.Error()),
			)
			// Background reconnection goroutine
			go func() {
				for {
					time.Sleep(30 * time.Second)
					if db.Pool != nil {
						return // Already connected
					}
					slog.Info("Attempting database reconnection")
					if err := db.Init(); err == nil {
						slog.Info("Database reconnected successfully")
						return
					} else {
						slog.Warn("Database reconnection failed", slog.String("error", err.Error()))
					}
				}
			}()
		}
	}

	// Initialize MinIO storage (legacy; always attempted for backward compat).
	if err := storage.Init(); err != nil {
		slog.Warn("MinIO storage not available — images will not be stored",
			slog.String("error", err.Error()),
		)
	} else {
		slog.Info("MinIO storage initialized")
	}

	// Initialize ImageStore (factory: minio | supabase | dual).
	// Controlled by IMAGE_STORAGE_BACKEND env var.
	imageStore := storage.NewImageStore()
	slog.Info("ImageStore backend selected",
		slog.String("backend", os.Getenv("IMAGE_STORAGE_BACKEND")),
		slog.Bool("supabase_storage_url_set", os.Getenv("SUPABASE_STORAGE_URL") != ""),
	)

	// Load configuration
	config, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create API handler and inject ImageStore.
	handler := api.NewHandler(config).WithImageStore(imageStore)
	router := handler.SetupRoutes()

	// Add login endpoint
	router.HandleFunc("/api/login", auth.LoginHandler).Methods("POST")
	// Rutas para clientes (app móvil)
	router.HandleFunc("/api/clientes/login/", auth.ClientLoginHandler).Methods("POST")
	router.HandleFunc("/api/clientes/me/", auth.ClientMeHandler).Methods("GET")

	// Wrap router with JWT middleware (skips /health and /api/login)
	protectedRouter := auth.JWTMiddleware(router)

	// Start server
	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	slog.Info("Starting Invoice OCR Service",
		slog.String("version", version.Version),
		slog.String("commit", version.Commit),
		slog.String("addr", addr),
		slog.String("ocr_engine", config.OCR.Engine),
		slog.String("ai_provider", config.AI.DefaultProvider),
		slog.Bool("db_available", db.Pool != nil),
		slog.Bool("storage_available", storage.Client != nil),
	)

	if err := http.ListenAndServe(addr, protectedRouter); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func loadConfig(path string) (*models.Config, error) {
	// Read config file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	var config models.Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Override with environment variables if present
	if port := os.Getenv("PORT"); port != "" {
		fmt.Sscanf(port, "%d", &config.Port)
	}
	if host := os.Getenv("HOST"); host != "" {
		config.Host = host
	}
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		config.AI.OpenAI.APIKey = apiKey
	}
	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		config.AI.Gemini.APIKey = apiKey
	}
	if baseURL := os.Getenv("OLLAMA_BASE_URL"); baseURL != "" {
		config.AI.Ollama.BaseURL = baseURL
	}
	if provider := os.Getenv("AI_PROVIDER"); provider != "" {
		config.AI.DefaultProvider = provider
	}
	if baseURL := os.Getenv("OPENAI_BASE_URL"); baseURL != "" {
		config.AI.OpenAI.BaseURL = baseURL
	}
	if model := os.Getenv("OPENAI_MODEL"); model != "" {
		config.AI.OpenAI.Model = model
	}
	if model := os.Getenv("GEMINI_MODEL"); model != "" {
		config.AI.Gemini.Model = model
	}

	return &config, nil
}
