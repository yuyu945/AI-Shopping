package knowledge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOStorage struct {
	client *minio.Client
}

func NewMinIOStorage(endpoint, accessKey, secretKey string, secure bool) (*MinIOStorage, error) {
	if endpoint == "" || accessKey == "" || secretKey == "" {
		return nil, errors.New("minio configuration is required")
	}
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: secure})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	return &MinIOStorage{client: client}, nil
}

func (s *MinIOStorage) PutObject(ctx context.Context, bucket, key string, content []byte, contentType string) error {
	if s == nil || s.client == nil {
		return errors.New("minio storage is unavailable")
	}
	_, err := s.client.PutObject(ctx, bucket, key, bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("put minio object: %w", err)
	}
	return nil
}

func (s *MinIOStorage) GetObject(ctx context.Context, bucket, key string) ([]byte, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("minio storage is unavailable")
	}
	object, err := s.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get minio object: %w", err)
	}
	defer object.Close()
	content, err := io.ReadAll(object)
	if err != nil {
		return nil, fmt.Errorf("read minio object: %w", err)
	}
	return content, nil
}

func (s *MinIOStorage) DeleteObject(ctx context.Context, bucket, key string) error {
	if s == nil || s.client == nil {
		return errors.New("minio storage is unavailable")
	}
	if err := s.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete minio object: %w", err)
	}
	return nil
}
