package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// SupabaseImageStore stores and retrieves images from Supabase Storage via REST API.
// archivo_url format: "supabase://<bucket>/<key>"
type SupabaseImageStore struct {
	httpClient *http.Client
	apiURL     string // e.g. "http://172.20.2.6:5000" (SUPABASE_STORAGE_URL)
	serviceKey string // SUPABASE_SERVICE_KEY (Bearer token)
	bucket     string // SUPABASE_BUCKET (default "facturas-imagenes")
}

// NewSupabaseImageStore creates a SupabaseImageStore from environment variables.
//
//	SUPABASE_STORAGE_URL  — storage API base URL (required; "" disables Supabase)
//	SUPABASE_SERVICE_KEY  — service_role JWT
//	SUPABASE_BUCKET       — bucket name (default "facturas-imagenes")
func NewSupabaseImageStore() *SupabaseImageStore {
	apiURL := os.Getenv("SUPABASE_STORAGE_URL")
	if apiURL == "" {
		apiURL = "http://172.20.2.6:5000"
	}
	bucket := os.Getenv("SUPABASE_BUCKET")
	if bucket == "" {
		bucket = "facturas-imagenes"
	}
	return &SupabaseImageStore{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiURL:     strings.TrimRight(apiURL, "/"),
		serviceKey: os.Getenv("SUPABASE_SERVICE_KEY"),
		bucket:     bucket,
	}
}

// Upload stores data under "<empresaAlias>/<year>/<month>/<filename>" in the bucket.
// Returns "supabase://<bucket>/<key>".
// empresaAlias should be the empresa UUID or alias — caller is responsible for correct value.
func (s *SupabaseImageStore) Upload(ctx context.Context, empresaAlias, filename string, data []byte, contentType string) (string, error) {
	if s.apiURL == "" {
		return "", fmt.Errorf("supabase: SUPABASE_STORAGE_URL not set")
	}
	if empresaAlias == "" {
		empresaAlias = "default"
	}

	now := time.Now()
	objectKey := fmt.Sprintf("%s/%d/%02d/%s", empresaAlias, now.Year(), now.Month(), filename)
	objectKey = collapseSlashes(objectKey)

	url := fmt.Sprintf("%s/object/%s/%s", s.apiURL, s.bucket, objectKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("supabase upload: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.serviceKey)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-upsert", "true")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("supabase upload http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("supabase upload HTTP %d: %s", resp.StatusCode, string(body))
	}

	archivoURL := fmt.Sprintf("supabase://%s/%s", s.bucket, objectKey)
	return archivoURL, nil
}

// GetImage retrieves image bytes from Supabase Storage.
// archivoURL must have prefix "supabase://<bucket>/<key>".
func (s *SupabaseImageStore) GetImage(ctx context.Context, archivoURL string) ([]byte, string, error) {
	if s.apiURL == "" {
		return nil, "", fmt.Errorf("supabase: SUPABASE_STORAGE_URL not set")
	}
	bucket, key, err := parseSupabaseURL(archivoURL)
	if err != nil {
		return nil, "", err
	}

	url := fmt.Sprintf("%s/object/%s/%s", s.apiURL, bucket, key)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("supabase get: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.serviceKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("supabase get http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("supabase get HTTP %d: %s", resp.StatusCode, string(body))
	}

	imgData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("supabase get read body: %w", err)
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		ct = "image/jpeg"
	}
	return imgData, ct, nil
}

// Delete removes an object from Supabase Storage.
func (s *SupabaseImageStore) Delete(ctx context.Context, archivoURL string) error {
	if s.apiURL == "" {
		return fmt.Errorf("supabase: SUPABASE_STORAGE_URL not set")
	}
	bucket, key, err := parseSupabaseURL(archivoURL)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/object/%s/%s", s.apiURL, bucket, key)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("supabase delete: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.serviceKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("supabase delete http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase delete HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// parseSupabaseURL parses "supabase://<bucket>/<key>" into bucket + key.
func parseSupabaseURL(archivoURL string) (bucket, key string, err error) {
	if !strings.HasPrefix(archivoURL, "supabase://") {
		return "", "", fmt.Errorf("supabase: not a supabase:// URL: %q", archivoURL)
	}
	trimmed := strings.TrimPrefix(archivoURL, "supabase://")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("supabase: invalid URL format %q (expected supabase://<bucket>/<key>)", archivoURL)
	}
	return parts[0], parts[1], nil
}

// collapseSlashes removes double-slashes and leading slash from a path.
func collapseSlashes(s string) string {
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	return strings.TrimPrefix(s, "/")
}
