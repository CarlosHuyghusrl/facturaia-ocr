package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var Client *minio.Client
var BucketName string

func Init() error {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		endpoint = "minio:9000"
	}

	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "facturaia-backend"
	}

	secretKey := os.Getenv("MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = "z0AKTjQXUDBe9QSuNpJz98WM4gdir8uP"
	}

	BucketName = os.Getenv("MINIO_BUCKET")
	if BucketName == "" {
		BucketName = "facturas"
	}

	useSSL := os.Getenv("MINIO_USE_SSL") == "true"

	var err error
	Client, err = minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return fmt.Errorf("failed to create MinIO client: %w", err)
	}

	// Verify bucket exists
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := Client.BucketExists(ctx, BucketName)
	if err != nil {
		return fmt.Errorf("failed to check bucket: %w", err)
	}
	if !exists {
		return fmt.Errorf("bucket %s does not exist", BucketName)
	}

	return nil
}

// UploadInvoiceImage uploads an invoice image with multi-tenant path structure
// Path format: {empresa_alias}/YYYY/MM/{filename}
func UploadInvoiceImage(ctx context.Context, empresaAlias string, filename string, reader io.Reader, size int64, contentType string) (string, error) {
	// Capa 1: defensive fallback — empresaAlias="" would produce a leading slash
	if empresaAlias == "" {
		empresaAlias = "default"
	}

	now := time.Now()
	objectName := fmt.Sprintf("%s/%d/%02d/%s",
		empresaAlias,
		now.Year(),
		now.Month(),
		filename,
	)

	// Capa 2: collapse any accidental double slashes (defense in depth)
	for strings.Contains(objectName, "//") {
		objectName = strings.ReplaceAll(objectName, "//", "/")
	}
	objectName = strings.TrimPrefix(objectName, "/")

	_, err := Client.PutObject(ctx, BucketName, objectName, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload image: %w", err)
	}

	// Return the full path for storage in DB
	fullPath := fmt.Sprintf("%s/%s", BucketName, objectName)

	// Capa 3: final normalize — ensure no double slashes in returned path
	for strings.Contains(fullPath, "//") {
		fullPath = strings.ReplaceAll(fullPath, "//", "/")
	}

	return fullPath, nil
}

// GetPresignedURL generates a presigned URL for viewing an image
func GetPresignedURL(ctx context.Context, objectPath string) (string, error) {
	// Remove bucket prefix if present
	objectName := objectPath
	if len(objectPath) > len(BucketName)+1 && objectPath[:len(BucketName)+1] == BucketName+"/" {
		objectName = objectPath[len(BucketName)+1:]
	}

	url, err := Client.PresignedGetObject(ctx, BucketName, objectName, 24*time.Hour, nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return url.String(), nil
}

// DeleteImage removes an image from storage
func DeleteImage(ctx context.Context, objectPath string) error {
	objectName := objectPath
	if len(objectPath) > len(BucketName)+1 && objectPath[:len(BucketName)+1] == BucketName+"/" {
		objectName = objectPath[len(BucketName)+1:]
	}

	return Client.RemoveObject(ctx, BucketName, objectName, minio.RemoveObjectOptions{})
}

// =============================================================================
// MinIOImageStore — ImageStore wrapper around the existing package-level MinIO
// functions. Allows DualImageStore and NewImageStore() to use MinIO without
// changing the existing callsites in handler.go / client_handlers.go.
// =============================================================================

// MinIOImageStore implements ImageStore on top of MinIO.
type MinIOImageStore struct{}

// NewMinIOImageStore returns a MinIOImageStore. It uses the package-level Client
// and BucketName initialized by Init().
func NewMinIOImageStore() *MinIOImageStore { return &MinIOImageStore{} }

// Upload stores image bytes using the existing UploadInvoiceImage logic.
func (m *MinIOImageStore) Upload(ctx context.Context, empresaAlias, filename string, data []byte, contentType string) (string, error) {
	reader := bytes.NewReader(data)
	return UploadInvoiceImage(ctx, empresaAlias, filename, reader, int64(len(data)), contentType)
}

// GetImage fetches an image from MinIO by its archivo_url (legacy MinIO path).
// Supports paths with or without the bucket prefix.
func (m *MinIOImageStore) GetImage(ctx context.Context, archivoURL string) ([]byte, string, error) {
	if Client == nil {
		return nil, "", fmt.Errorf("minio: client not initialized")
	}
	objectName := archivoURL
	prefix := BucketName + "/"
	if strings.HasPrefix(objectName, prefix) {
		objectName = objectName[len(prefix):]
	}

	obj, err := Client.GetObject(ctx, BucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("minio get object: %w", err)
	}
	defer obj.Close()

	info, err := obj.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("minio stat object: %w", err)
	}

	ct := info.ContentType
	if ct == "" || ct == "application/octet-stream" {
		ct = "image/jpeg"
	}

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, "", fmt.Errorf("minio read object: %w", err)
	}
	return data, ct, nil
}

// Delete removes an image from MinIO by its archivo_url.
func (m *MinIOImageStore) Delete(ctx context.Context, archivoURL string) error {
	return DeleteImage(ctx, archivoURL)
}

// GetSignedURL generates a MinIO presigned GET URL for the given archivo_url.
// Returns ErrSignedURLNotSupported if the MinIO client is not initialized.
func (m *MinIOImageStore) GetSignedURL(ctx context.Context, archivoURL string, expiresIn time.Duration) (string, error) {
	if Client == nil {
		return "", ErrSignedURLNotSupported
	}
	return GetPresignedURL(ctx, archivoURL)
}

// GetFileExtension extracts file extension from content type
func GetFileExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	default:
		return ".bin"
	}
}
