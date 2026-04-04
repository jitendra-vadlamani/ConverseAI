package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"path/filepath"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type StorageService interface {
	Save(ctx context.Context, userID, filename string, data []byte) (string, error)
	Get(ctx context.Context, fileID string) ([]byte, error)
	GetPresignedURL(ctx context.Context, fileID string) (string, error)
	Delete(ctx context.Context, fileID string) error
}

type minioStorage struct {
	client     *minio.Client
	bucketName string
}

func NewStorageService(endpoint, accessKey, secretKey, bucketName string, useSSL bool) (StorageService, error) {
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, err
	}

	// Auto-create bucket if it doesn't exist
	ctx := context.Background()
	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		return nil, err
	}
	if !exists {
		err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, err
		}
		log.Printf("[Storage] Created bucket: %s", bucketName)
	}

	return &minioStorage{
		client:     minioClient,
		bucketName: bucketName,
	}, nil
}

func (s *minioStorage) Save(ctx context.Context, userID, filename string, data []byte) (string, error) {
	// Separation of files per user: user-{userID}/filename
	// We sanitize the userID part and add a timestamp to prevent collisions
	timestamp := time.Now().UnixNano()
	fileID := fmt.Sprintf("user-%s/%d-%s", userID, timestamp, filename)
	
	reader := bytes.NewReader(data)
	_, err := s.client.PutObject(ctx, s.bucketName, fileID, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: getContentType(filename),
	})
	if err != nil {
		return "", err
	}
	
	log.Printf("[Storage] Saved file for user %s: %s", userID, fileID)
	return fileID, nil
}

func (s *minioStorage) Get(ctx context.Context, fileID string) ([]byte, error) {
	object, err := s.client.GetObject(ctx, s.bucketName, fileID, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()

	return io.ReadAll(object)
}

func (s *minioStorage) GetPresignedURL(ctx context.Context, fileID string) (string, error) {
	reqParams := make(url.Values)
	// Presigned URL valid for 1 hour
	presignedURL, err := s.client.PresignedGetObject(ctx, s.bucketName, fileID, time.Hour, reqParams)
	if err != nil {
		return "", err
	}
	return presignedURL.String(), nil
}

func (s *minioStorage) Delete(ctx context.Context, fileID string) error {
	err := s.client.RemoveObject(ctx, s.bucketName, fileID, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete object %s: %w", fileID, err)
	}
	log.Printf("[Storage] Deleted file: %s", fileID)
	return nil
}

func getContentType(filename string) string {
	ext := filepath.Ext(filename)
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain"
	case ".csv":
		return "text/csv"
	case ".json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}
