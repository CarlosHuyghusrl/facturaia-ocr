package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestParseSupabaseURL — unit tests for parseSupabaseURL helper.
func TestParseSupabaseURL(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantBucket string
		wantKey    string
		wantErr    bool
	}{
		{
			name:       "valid full path",
			url:        "supabase://facturas-imagenes/616b8f1b-d3f1-403d-883b-aec3302363e5/2026/01/file.jpg",
			wantBucket: "facturas-imagenes",
			wantKey:    "616b8f1b-d3f1-403d-883b-aec3302363e5/2026/01/file.jpg",
		},
		{
			name:       "valid bucket + simple key",
			url:        "supabase://my-bucket/path/to/img.png",
			wantBucket: "my-bucket",
			wantKey:    "path/to/img.png",
		},
		{
			name:    "missing supabase prefix",
			url:     "facturas/huyghu/2026/01/file.jpg",
			wantErr: true,
		},
		{
			name:    "no key after bucket",
			url:     "supabase://only-bucket",
			wantErr: true,
		},
		{
			name:    "empty string",
			url:     "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bucket, key, err := parseSupabaseURL(tc.url)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (bucket=%q key=%q)", bucket, key)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if bucket != tc.wantBucket {
				t.Errorf("bucket: got %q, want %q", bucket, tc.wantBucket)
			}
			if key != tc.wantKey {
				t.Errorf("key: got %q, want %q", key, tc.wantKey)
			}
		})
	}
}

// TestCollapseSlashes — unit test for the collapseSlashes helper.
func TestCollapseSlashes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"a//b//c", "a/b/c"},
		{"/leading", "leading"},
		{"//double//leading", "double/leading"},
		{"normal/path/here", "normal/path/here"},
		{"", ""},
	}
	for _, tc := range tests {
		got := collapseSlashes(tc.input)
		if got != tc.want {
			t.Errorf("collapseSlashes(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestSupabaseImageStore_Upload_Mock — tests Upload against a mock HTTP server.
func TestSupabaseImageStore_Upload_Mock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/object/facturas-imagenes/") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-service-key" {
			t.Errorf("unexpected Authorization: %s", auth)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"Key":"test-key"}`))
	}))
	defer srv.Close()

	s := &SupabaseImageStore{
		httpClient: srv.Client(),
		apiURL:     srv.URL,
		serviceKey: "test-service-key",
		bucket:     "facturas-imagenes",
	}

	archivoURL, err := s.Upload(context.Background(), "empresa123", "test.jpg", []byte("fake-img"), "image/jpeg")
	if err != nil {
		t.Fatalf("Upload returned error: %v", err)
	}
	if !strings.HasPrefix(archivoURL, "supabase://facturas-imagenes/empresa123/") {
		t.Errorf("unexpected archivo_url: %q", archivoURL)
	}
}

// TestSupabaseImageStore_GetImage_Mock — tests GetImage against a mock HTTP server.
func TestSupabaseImageStore_GetImage_Mock(t *testing.T) {
	fakeImg := []byte("fake-image-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		w.Write(fakeImg)
	}))
	defer srv.Close()

	s := &SupabaseImageStore{
		httpClient: srv.Client(),
		apiURL:     srv.URL,
		serviceKey: "test-key",
		bucket:     "facturas-imagenes",
	}

	archivoURL := "supabase://facturas-imagenes/empresa/2026/01/file.jpg"
	data, ct, err := s.GetImage(context.Background(), archivoURL)
	if err != nil {
		t.Fatalf("GetImage returned error: %v", err)
	}
	if ct != "image/jpeg" {
		t.Errorf("contentType: got %q, want image/jpeg", ct)
	}
	if string(data) != string(fakeImg) {
		t.Errorf("data mismatch: got %q, want %q", data, fakeImg)
	}
}

// TestSupabaseImageStore_GetImage_404 — confirms 404 is returned as error.
func TestSupabaseImageStore_GetImage_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	s := &SupabaseImageStore{
		httpClient: srv.Client(),
		apiURL:     srv.URL,
		serviceKey: "test-key",
		bucket:     "facturas-imagenes",
	}

	_, _, err := s.GetImage(context.Background(), "supabase://facturas-imagenes/missing/file.jpg")
	if err == nil {
		t.Error("expected error for 404, got nil")
	}
}

// TestSupabaseImageStore_E2E — integration test with real Supabase Storage.
// Skipped by default; only runs when SUPABASE_STORAGE_URL env var is set.
func TestSupabaseImageStore_E2E(t *testing.T) {
	supabaseURL := os.Getenv("SUPABASE_STORAGE_URL")
	if supabaseURL == "" {
		t.Skip("SUPABASE_STORAGE_URL not set, skipping E2E test")
	}

	s := NewSupabaseImageStore()
	ctx := context.Background()

	// List objects at known path prefix (22 migrated facturas).
	// We use GetImage to verify one of the migrated files returns bytes.
	// Path format: 616b8f1b-d3f1-403d-883b-aec3302363e5/<year>/<month>/<filename>
	// Use a HEAD-style test: Upload a tiny file and re-fetch it.
	testData := []byte("supabase-e2e-test-placeholder")
	archivoURL, err := s.Upload(ctx, "test-e2e", "e2e_test.txt", testData, "text/plain")
	if err != nil {
		t.Fatalf("E2E Upload failed: %v", err)
	}
	t.Logf("Uploaded to: %s", archivoURL)

	data, ct, err := s.GetImage(ctx, archivoURL)
	if err != nil {
		t.Fatalf("E2E GetImage failed: %v", err)
	}
	t.Logf("Retrieved %d bytes, content-type: %s", len(data), ct)

	if err := s.Delete(ctx, archivoURL); err != nil {
		t.Logf("E2E Delete warning (non-fatal): %v", err)
	}
}
