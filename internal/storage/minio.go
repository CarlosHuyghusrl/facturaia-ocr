package storage

import (
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
