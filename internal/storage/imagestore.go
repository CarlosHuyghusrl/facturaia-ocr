package storage

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// ImageStore abstracts the image storage backend (MinIO, Supabase, Dual).
type ImageStore interface {
	// Upload stores image data and returns an archivo_url string.
	// empresaAlias is used as path prefix (e.g. empresa UUID or alias).
	Upload(ctx context.Context, empresaAlias, filename string, data []byte, contentType string) (archivoURL string, err error)

	// GetImage fetches image bytes by its archivo_url (schema-aware).
	// Returns (data, contentType, error).
	GetImage(ctx context.Context, archivoURL string) ([]byte, string, error)

	// Delete removes an image by its archivo_url.
	Delete(ctx context.Context, archivoURL string) error

	// GetSignedURL generates a time-limited direct-access URL for the given archivo_url.
	// For Supabase backends this avoids backend bandwidth by letting the frontend fetch
	// directly from Supabase Storage. For MinIO it returns a presigned URL.
	// Returns ErrNotSupported if the backend does not support signed URLs.
	GetSignedURL(ctx context.Context, archivoURL string, expiresIn time.Duration) (string, error)
}

// ErrSignedURLNotSupported is returned by backends that do not support signed URL generation
// (e.g. legacy MinIO paths where the client is not configured).
var ErrSignedURLNotSupported = fmt.Errorf("signed URL not supported for this storage backend")

// NewImageStore factory controlled by IMAGE_STORAGE_BACKEND env var.
//
//   - ""       → "minio" (backward compat, unchanged)
//   - "minio"  → MinIOImageStore only
//   - "supabase" → SupabaseImageStore only (only if SUPABASE_STORAGE_URL set)
//   - "dual"   → DualImageStore: write both, read schema-aware (transition mode)
//
// If SUPABASE_STORAGE_URL is not set, supabase/dual modes fall back to minio.
func NewImageStore() ImageStore {
	supabaseURL := os.Getenv("SUPABASE_STORAGE_URL")
	backend := os.Getenv("IMAGE_STORAGE_BACKEND")

	// If Supabase not configured, always use MinIO regardless of backend flag.
	if supabaseURL == "" && (backend == "supabase" || backend == "dual") {
		backend = "minio"
	}

	switch backend {
	case "supabase":
		return NewSupabaseImageStore()
	case "dual":
		return NewDualImageStore()
	case "minio", "":
		return NewMinIOImageStore()
	default:
		return NewMinIOImageStore()
	}
}

// DualImageStore writes to both backends (Supabase primary, MinIO secondary)
// and reads schema-aware (supabase:// → Supabase, else → MinIO).
// Used during cutover transition.
type DualImageStore struct {
	primary   ImageStore // Supabase — new writes go here
	secondary ImageStore // MinIO   — legacy reads + backup writes
}

// NewDualImageStore creates a DualImageStore with Supabase primary + MinIO secondary.
func NewDualImageStore() *DualImageStore {
	return &DualImageStore{
		primary:   NewSupabaseImageStore(),
		secondary: NewMinIOImageStore(),
	}
}

// Upload writes to Supabase (primary); if that fails, writes MinIO only.
// On primary success also writes MinIO as backup (silent failure OK).
func (d *DualImageStore) Upload(ctx context.Context, empresaAlias, filename string, data []byte, contentType string) (string, error) {
	path, err := d.primary.Upload(ctx, empresaAlias, filename, data, contentType)
	if err == nil {
		// Dual-write to MinIO as backup (non-blocking best-effort).
		_, _ = d.secondary.Upload(ctx, empresaAlias, filename, data, contentType)
		return path, nil
	}
	// Supabase failed — fall back to MinIO only.
	return d.secondary.Upload(ctx, empresaAlias, filename, data, contentType)
}

// GetImage routes by prefix: supabase:// → Supabase, else → MinIO.
func (d *DualImageStore) GetImage(ctx context.Context, archivoURL string) ([]byte, string, error) {
	if strings.HasPrefix(archivoURL, "supabase://") {
		return d.primary.GetImage(ctx, archivoURL)
	}
	return d.secondary.GetImage(ctx, archivoURL)
}

// Delete routes by prefix: supabase:// → Supabase, else → MinIO.
func (d *DualImageStore) Delete(ctx context.Context, archivoURL string) error {
	if strings.HasPrefix(archivoURL, "supabase://") {
		return d.primary.Delete(ctx, archivoURL)
	}
	return d.secondary.Delete(ctx, archivoURL)
}

// GetSignedURL routes by prefix: supabase:// → Supabase, else → MinIO.
func (d *DualImageStore) GetSignedURL(ctx context.Context, archivoURL string, expiresIn time.Duration) (string, error) {
	if strings.HasPrefix(archivoURL, "supabase://") {
		return d.primary.GetSignedURL(ctx, archivoURL, expiresIn)
	}
	return d.secondary.GetSignedURL(ctx, archivoURL, expiresIn)
}
