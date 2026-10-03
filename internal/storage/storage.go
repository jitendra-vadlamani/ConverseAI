package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ErrNotFound is returned when an object doesn't exist.
var ErrNotFound = errors.New("object not found")

type Object struct {
	Body        io.ReadCloser
	Size        int64
	ContentType string
}

type StorageService interface {
	// Save stores data under the user's prefix and returns the object key.
	// Identical content already stored for the user is reused.
	Save(ctx context.Context, userID, filename string, data []byte) (string, error)
	Get(ctx context.Context, fileID string) ([]byte, error)
	Open(ctx context.Context, fileID string) (*Object, error)
	Delete(ctx context.Context, fileID string) error
	DeleteUser(ctx context.Context, userID string) error
	Ping(ctx context.Context) error
}

// UserPrefix is the key prefix every object of a user lives under; it is
// also how handlers check that a file id belongs to the caller.
func UserPrefix(userID string) string { return "user-" + userID + "/" }

// DisplayName returns the filename part of an object key
// ("user-<id>/<unix-nanos>-<name>" -> "<name>").
func DisplayName(fileID string) string {
	base := path.Base(fileID)
	if _, name, ok := strings.Cut(base, "-"); ok {
		return name
	}
	return base
}

type minioStorage struct {
	client     *minio.Client
	bucketName string
}

func NewStorageService(ctx context.Context, endpoint, accessKey, secretKey, bucketName string, useSSL bool) (StorageService, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, err
	}
	exists, err := client.BucketExists(ctx, bucketName)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create bucket: %w", err)
		}
		slog.Info("created bucket", "bucket", bucketName)
	}
	return &minioStorage{client: client, bucketName: bucketName}, nil
}

func (s *minioStorage) Save(ctx context.Context, userID, filename string, data []byte) (string, error) {
	sum := md5.Sum(data)
	md5Str := hex.EncodeToString(sum[:])
	prefix := UserPrefix(userID)

	existing := map[string]bool{}
	for obj := range s.client.ListObjects(ctx, s.bucketName, minio.ListObjectsOptions{Prefix: prefix}) {
		if obj.Err != nil {
			return "", fmt.Errorf("list objects: %w", obj.Err)
		}
		// Single-part uploads have the content MD5 as their ETag.
		if strings.Trim(obj.ETag, "\"") == md5Str {
			return obj.Key, nil
		}
		existing[DisplayName(obj.Key)] = true
	}

	finalName := filename
	if existing[filename] {
		ext := filepath.Ext(filename)
		base := strings.TrimSuffix(filename, ext)
		for i := 1; ; i++ {
			candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
			if !existing[candidate] {
				finalName = candidate
				break
			}
		}
	}

	fileID := fmt.Sprintf("%s%d-%s", prefix, time.Now().UnixNano(), finalName)
	_, err := s.client.PutObject(ctx, s.bucketName, fileID, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: ContentType(finalName),
	})
	if err != nil {
		return "", fmt.Errorf("put object: %w", err)
	}
	slog.Info("stored file", "file", fileID, "bytes", len(data))
	return fileID, nil
}

func (s *minioStorage) Get(ctx context.Context, fileID string) ([]byte, error) {
	obj, err := s.Open(ctx, fileID)
	if err != nil {
		return nil, err
	}
	defer obj.Body.Close()
	return io.ReadAll(obj.Body)
}

func (s *minioStorage) Open(ctx context.Context, fileID string) (*Object, error) {
	obj, err := s.client.GetObject(ctx, s.bucketName, fileID, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &Object{Body: obj, Size: info.Size, ContentType: info.ContentType}, nil
}

func (s *minioStorage) Delete(ctx context.Context, fileID string) error {
	if err := s.client.RemoveObject(ctx, s.bucketName, fileID, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete object %s: %w", fileID, err)
	}
	return nil
}

func (s *minioStorage) DeleteUser(ctx context.Context, userID string) error {
	objects := s.client.ListObjects(ctx, s.bucketName, minio.ListObjectsOptions{Prefix: UserPrefix(userID), Recursive: true})
	for res := range s.client.RemoveObjects(ctx, s.bucketName, objects, minio.RemoveObjectsOptions{}) {
		if res.Err != nil {
			return fmt.Errorf("delete %s: %w", res.ObjectName, res.Err)
		}
	}
	return nil
}

func (s *minioStorage) Ping(ctx context.Context) error {
	_, err := s.client.BucketExists(ctx, s.bucketName)
	return err
}

// ContentType guesses a MIME type from the extension.
func ContentType(filename string) string {
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); t != "" {
		return t
	}
	return "application/octet-stream"
}
