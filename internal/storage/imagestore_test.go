package storage

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// mockStore is a simple in-memory ImageStore for testing DualImageStore routing.
type mockStore struct {
	name      string
	uploadFn  func(ctx context.Context, alias, filename string, data []byte, ct string) (string, error)
	getImageFn func(ctx context.Context, archivoURL string) ([]byte, string, error)
	deleteFn  func(ctx context.Context, archivoURL string) error
}

func (m *mockStore) Upload(ctx context.Context, alias, filename string, data []byte, ct string) (string, error) {
	if m.uploadFn != nil {
		return m.uploadFn(ctx, alias, filename, data, ct)
	}
	return m.name + "://" + alias + "/" + filename, nil
}

func (m *mockStore) GetImage(ctx context.Context, archivoURL string) ([]byte, string, error) {
	if m.getImageFn != nil {
		return m.getImageFn(ctx, archivoURL)
	}
	return []byte(m.name + "-data"), "image/jpeg", nil
}

func (m *mockStore) Delete(ctx context.Context, archivoURL string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, archivoURL)
	}
	return nil
}

// TestDualImageStore_SchemaRouting verifies that DualImageStore routes reads
// to the correct backend based on the archivo_url prefix.
func TestDualImageStore_SchemaRouting(t *testing.T) {
	primary := &mockStore{name: "supabase"}   // handles supabase:// URLs
	secondary := &mockStore{name: "minio"}    // handles legacy MinIO paths

	dual := &DualImageStore{primary: primary, secondary: secondary}

	t.Run("supabase:// routes to primary", func(t *testing.T) {
		data, _, err := dual.GetImage(context.Background(), "supabase://facturas-imagenes/uuid/2026/01/file.jpg")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(string(data), "supabase") {
			t.Errorf("expected supabase primary data, got: %s", data)
		}
	})

	t.Run("legacy path routes to secondary", func(t *testing.T) {
		data, _, err := dual.GetImage(context.Background(), "facturas/huyghu/2026/01/file.jpg")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(string(data), "minio") {
			t.Errorf("expected minio secondary data, got: %s", data)
		}
	})

	t.Run("empty path routes to secondary", func(t *testing.T) {
		data, _, err := dual.GetImage(context.Background(), "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(string(data), "minio") {
			t.Errorf("expected minio secondary data, got: %s", data)
		}
	})
}

// TestDualImageStore_Delete_Routing verifies Delete routes by prefix.
func TestDualImageStore_Delete_Routing(t *testing.T) {
	var deletedOn string
	primary := &mockStore{name: "supabase", deleteFn: func(ctx context.Context, url string) error {
		deletedOn = "primary"
		return nil
	}}
	secondary := &mockStore{name: "minio", deleteFn: func(ctx context.Context, url string) error {
		deletedOn = "secondary"
		return nil
	}}

	dual := &DualImageStore{primary: primary, secondary: secondary}

	dual.Delete(context.Background(), "supabase://bucket/key.jpg")
	if deletedOn != "primary" {
		t.Errorf("expected primary delete, got: %s", deletedOn)
	}

	dual.Delete(context.Background(), "facturas/legacy/path.jpg")
	if deletedOn != "secondary" {
		t.Errorf("expected secondary delete, got: %s", deletedOn)
	}
}

// TestDualImageStore_Upload_FallsBackToSecondary verifies that if primary Upload
// fails, DualImageStore falls back to secondary.
func TestDualImageStore_Upload_FallsBackToSecondary(t *testing.T) {
	primary := &mockStore{name: "supabase", uploadFn: func(ctx context.Context, alias, filename string, data []byte, ct string) (string, error) {
		return "", fmt.Errorf("supabase unavailable")
	}}
	secondary := &mockStore{name: "minio"}

	dual := &DualImageStore{primary: primary, secondary: secondary}

	archivoURL, err := dual.Upload(context.Background(), "empresa", "test.jpg", []byte("data"), "image/jpeg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Fallback should return MinIO path (which starts with "minio://...")
	if strings.HasPrefix(archivoURL, "supabase://") {
		t.Errorf("should have fallen back to MinIO, got: %s", archivoURL)
	}
}

// TestDualImageStore_Upload_DualWrite verifies that on primary success, secondary
// also receives an upload (dual write).
func TestDualImageStore_Upload_DualWrite(t *testing.T) {
	var secondaryUploadCalled bool
	primary := &mockStore{name: "supabase"}
	secondary := &mockStore{name: "minio", uploadFn: func(ctx context.Context, alias, filename string, data []byte, ct string) (string, error) {
		secondaryUploadCalled = true
		return "facturas/" + alias + "/" + filename, nil
	}}

	dual := &DualImageStore{primary: primary, secondary: secondary}

	archivoURL, err := dual.Upload(context.Background(), "empresa", "test.jpg", []byte("data"), "image/jpeg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(archivoURL, "supabase://") {
		t.Errorf("expected supabase URL from primary, got: %s", archivoURL)
	}
	if !secondaryUploadCalled {
		t.Error("expected secondary (MinIO) to also be called for dual write")
	}
}

// TestNewImageStore_BackwardCompat verifies that with no env vars set,
// NewImageStore returns a MinIOImageStore (backward compat).
func TestNewImageStore_BackwardCompat(t *testing.T) {
	// Ensure env vars are cleared for this test.
	t.Setenv("IMAGE_STORAGE_BACKEND", "")
	t.Setenv("SUPABASE_STORAGE_URL", "")

	store := NewImageStore()
	if _, ok := store.(*MinIOImageStore); !ok {
		t.Errorf("expected *MinIOImageStore when no env set, got %T", store)
	}
}

// TestNewImageStore_SupabaseFallsBackToMinIOWhenURLEmpty verifies that requesting
// supabase or dual backend without SUPABASE_STORAGE_URL falls back to MinIO.
func TestNewImageStore_SupabaseFallsBackToMinIOWhenURLEmpty(t *testing.T) {
	t.Setenv("SUPABASE_STORAGE_URL", "")

	for _, backend := range []string{"supabase", "dual"} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv("IMAGE_STORAGE_BACKEND", backend)
			store := NewImageStore()
			if _, ok := store.(*MinIOImageStore); !ok {
				t.Errorf("expected MinIOImageStore fallback when SUPABASE_STORAGE_URL empty, got %T", store)
			}
		})
	}
}

// TestNewImageStore_DualWhenConfigured verifies NewImageStore returns DualImageStore
// when both IMAGE_STORAGE_BACKEND=dual and SUPABASE_STORAGE_URL are set.
func TestNewImageStore_DualWhenConfigured(t *testing.T) {
	t.Setenv("IMAGE_STORAGE_BACKEND", "dual")
	t.Setenv("SUPABASE_STORAGE_URL", "http://172.20.2.6:5000")

	store := NewImageStore()
	if _, ok := store.(*DualImageStore); !ok {
		t.Errorf("expected *DualImageStore, got %T", store)
	}
}
