package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/facturaIA/invoice-ocr-service/api"
	"github.com/facturaIA/invoice-ocr-service/internal/auth"
	"github.com/facturaIA/invoice-ocr-service/internal/db"
	"github.com/facturaIA/invoice-ocr-service/internal/models"
	"github.com/facturaIA/invoice-ocr-service/internal/storage"
	"gopkg.in/yaml.v3"
)

func main() {
	// Initialize JWT
	if err := auth.Init(); err != nil {
		log.Fatalf("Failed to initialize auth: %v", err)
	}
	log.Println("JWT authentication initialized")

	// Initialize database connection pool
	if err := db.Init(); err != nil {
		log.Printf("Warning: Database not available: %v", err)
		log.Println("Running in OCR-only mode (no persistence)")
	} else {
		defer db.Close()
		log.Println("Database connection pool initialized")
	}

	// Initialize MinIO storage
	if err := storage.Init(); err != nil {
		log.Printf("Warning: MinIO storage not available: %v", err)
		log.Println("Images will not be stored")
	} else {
		log.Println("MinIO storage initialized")
	}

	// Load configuration
	config, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create API handler
	handler := api.NewHandler(config)
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
	log.Printf("Starting Invoice OCR Service v2.1.0 on %s", addr)
	log.Printf("OCR Engine: %s", config.OCR.Engine)
	log.Printf("Default AI Provider: %s", config.AI.DefaultProvider)
	log.Printf("Database: %v", db.Pool != nil)
	log.Printf("Storage: %v", storage.Client != nil)
	log.Printf("Endpoints:")
	log.Printf("  POST http://%s/api/login              - Authenticate", addr)
	log.Printf("  POST http://%s/api/process-invoice    - Process invoice (requires JWT)", addr)
	log.Printf("  GET  http://%s/api/invoices           - Get all invoices (requires JWT)", addr)
	log.Printf("  GET  http://%s/api/invoice/{id}       - Get single invoice (requires JWT)", addr)
	log.Printf("  PUT  http://%s/api/invoice/{id}       - Update invoice (requires JWT)", addr)
	log.Printf("  DELETE http://%s/api/invoice/{id}     - Delete invoice (requires JWT)", addr)
	log.Printf("  GET  http://%s/api/stats              - Get monthly stats (requires JWT)", addr)
	log.Printf("  GET  http://%s/health                 - Health check", addr)

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
