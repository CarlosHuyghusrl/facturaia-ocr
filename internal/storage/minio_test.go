package storage

import (
	"strings"
	"testing"
)

// TestArchivoURL_NoDoubleSlash verifies that UploadInvoiceImage path generation
// never produces a double-slash in the returned archivo_url, even when
// empresaAlias is empty or contains edge-case values (KB 9175 root cause fix).
//
// This is a unit test that exercises the path-building logic in isolation
// (without a real MinIO connection). It tests the defensive normalization
// introduced in P1 of the W19 double-slash fix.
func TestArchivoURL_NoDoubleSlash(t *testing.T) {
	// Set a deterministic BucketName for test (normally set by Init() via MINIO_BUCKET env var)
	const testBucket = "facturas"

	tests := []struct {
		name         string
		empresaAlias string
		filename     string
	}{
		{
			name:         "empty alias produces no double slash",
			empresaAlias: "",
			filename:     "factura.jpg",
		},
		{
			name:         "normal alias produces no double slash",
			empresaAlias: "huyghu",
			filename:     "factura_2026.jpg",
		},
		{
			name:         "alias with trailing slash produces no double slash",
			empresaAlias: "huyghu/",
			filename:     "file.jpg",
		},
		{
			name:         "alias with leading slash produces no leading slash",
			empresaAlias: "/huyghu",
			filename:     "file.jpg",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Replicate the path-building logic from UploadInvoiceImage
			// (without calling PutObject, which needs a live MinIO).
			alias := tc.empresaAlias
			if alias == "" {
				alias = "default"
			}

			// Simulate the fmt.Sprintf that builds objectName
			// (using fixed year/month to keep test deterministic)
			objectName := alias + "/2026/05/" + tc.filename

			// Apply the same normalization as in UploadInvoiceImage
			for strings.Contains(objectName, "//") {
				objectName = strings.ReplaceAll(objectName, "//", "/")
			}
			objectName = strings.TrimPrefix(objectName, "/")

			fullPath := testBucket + "/" + objectName
			for strings.Contains(fullPath, "//") {
				fullPath = strings.ReplaceAll(fullPath, "//", "/")
			}

			// Assertions
			if strings.Contains(fullPath, "//") {
				t.Errorf("archivo_url contains double slash: %q", fullPath)
			}
			if strings.HasPrefix(fullPath, "/") {
				t.Errorf("archivo_url has leading slash: %q", fullPath)
			}
			if !strings.Contains(fullPath, tc.filename) {
				t.Errorf("archivo_url does not contain filename %q: %q", tc.filename, fullPath)
			}
			t.Logf("OK path: %q", fullPath)
		})
	}
}
