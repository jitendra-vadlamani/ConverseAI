package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"path/filepath"
	"crypto/md5"
	"encoding/hex"
	"strings"
	"path"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type StorageService interface {
	Save(ctx context.Context, userID, filename string, data []byte) (string, error)
	Get(ctx context.Context, fileID string) ([]byte, error)
	GetPresignedURL(ctx context.Context, fileID string) (string, error)
	Delete(ctx context.Context, fileID string) error
	ListUserFiles(ctx context.Context, userID string) ([]string, error)
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
	// 1. Calculate MD5
	hash := md5.Sum(data)
	md5Str := hex.EncodeToString(hash[:])

	userPrefix := fmt.Sprintf("user-%s/", userID)
	
	// 2. Check for duplicate content (exact same MD5)
	objects := s.client.ListObjects(ctx, s.bucketName, minio.ListObjectsOptions{
		Prefix: userPrefix,
	})
	
	existingFiles := make(map[string]string) // name -> ETag
	
	for obj := range objects {
		if obj.Err != nil {
			continue
		}
		// MinIO ETag is "md5hash" (with quotes)
		etag := strings.Trim(obj.ETag, "\"")
		if etag == md5Str {
			log.Printf("[Storage] Reuse existing file with same content: %s", obj.Key)
			return obj.Key, nil
		}
		
		// Collect existing filenames to handle name collisions/versioning
		baseName := path.Base(obj.Key)
		// Strip the timestamp prefix if present (e.g., "123456-filename.pdf")
		parts := strings.SplitN(baseName, "-", 2)
		if len(parts) == 2 {
			existingFiles[parts[1]] = etag
		} else {
			existingFiles[baseName] = etag
		}
	}

	// 3. Handle filename collision (intelligence: create a new version)
	finalFilename := filename
	if _, exists := existingFiles[filename]; exists {
		ext := filepath.Ext(filename)
		base := strings.TrimSuffix(filename, ext)
		counter := 1
		for {
			versioned := fmt.Sprintf("%s (%d)%s", base, counter, ext)
			if _, exists := existingFiles[versioned]; !exists {
				finalFilename = versioned
				break
			}
			counter++
		}
	}

	// 4. Save with timestamp prefix to keep unique paths
	timestamp := time.Now().UnixNano()
	fileID := fmt.Sprintf("%s%d-%s", userPrefix, timestamp, finalFilename)
	
	reader := bytes.NewReader(data)
	_, err := s.client.PutObject(ctx, s.bucketName, fileID, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: getContentType(finalFilename),
	})
	if err != nil {
		return "", err
	}
	
	log.Printf("[Storage] Saved file for user %s: %s (MD5: %s)", userID, fileID, md5Str)
	return fileID, nil
}

func (s *minioStorage) ListUserFiles(ctx context.Context, userID string) ([]string, error) {
	prefix := fmt.Sprintf("user-%s/", userID)
	objects := s.client.ListObjects(ctx, s.bucketName, minio.ListObjectsOptions{
		Prefix: prefix,
	})
	
	var files []string
	for obj := range objects {
		if obj.Err != nil {
			return nil, obj.Err
		}
		files = append(files, obj.Key)
	}
	return files, nil
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
