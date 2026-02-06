package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is the global database connection pool
var Pool *pgxpool.Pool

// Init initializes the database connection pool
func Init() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		// Build from individual environment variables
		host := os.Getenv("DB_HOST")
		port := os.Getenv("DB_PORT")
		user := os.Getenv("DB_USER")
		password := os.Getenv("DB_PASSWORD")
		dbname := os.Getenv("DB_NAME")

		if host != "" && user != "" && dbname != "" {
			if port == "" {
				port = "5432"
			}
			databaseURL = fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=disable",
				user, password, host, port, dbname)
		} else {
			// No database configured - this is OK for OCR-only mode
			log.Println("No database configuration found - running in OCR-only mode")
			return fmt.Errorf("no database configuration")
		}
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("failed to parse database URL: %w", err)
	}

	// Connection pool settings optimized for PgBouncer
	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnLifetime = 1 * time.Hour
	config.MaxConnIdleTime = 30 * time.Minute
	config.HealthCheckPeriod = 1 * time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	Pool = pool
	log.Println("Database connection pool initialized successfully")
	return nil
}

// Close closes the database connection pool
func Close() {
	if Pool != nil {
		Pool.Close()
		log.Println("Database connection pool closed")
	}
}

// GetPool returns the current connection pool
func GetPool() *pgxpool.Pool {
	return Pool
}

// GetSchemaForEmpresa returns the schema name for a given empresa alias
func GetSchemaForEmpresa(alias string) string {
	if alias == "" { return "public" }
	return "emp_" + alias
}
