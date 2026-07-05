package storage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/PixelAudit/PixelAudit/internal/config"
)

// S3 abstracts object storage. In development it can write to local disk when
// S3_ENDPOINT is empty, which keeps terminal-only backend runs lightweight.
type S3 struct {
	client   *s3.Client
	bucket   string
	base     string
	localDir string
}

func NewS3(ctx context.Context, cfg *config.Config) (*S3, error) {
	if cfg.S3Endpoint == "" {
		if err := os.MkdirAll(cfg.S3LocalDir, 0o755); err != nil {
			return nil, err
		}
		return &S3{bucket: cfg.S3Bucket, localDir: cfg.S3LocalDir}, nil
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.S3Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, "")),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.S3Endpoint)
		o.UsePathStyle = true
	})
	return &S3{client: client, bucket: cfg.S3Bucket, base: cfg.S3Endpoint}, nil
}

func (s *S3) Put(ctx context.Context, key string, body []byte, contentType string) (string, error) {
	if s.localDir != "" {
		path, err := s.localPath(key)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return "", err
		}
		return "file://" + filepath.ToSlash(path), nil
	}

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", err
	}
	if s.base != "" {
		return s.base + "/" + s.bucket + "/" + key, nil
	}
	return "https://" + s.bucket + ".s3.amazonaws.com/" + key, nil
}

func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	if s.localDir != "" {
		path, err := s.localPath(key)
		if err != nil {
			return nil, err
		}
		return os.ReadFile(path)
	}

	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(out.Body); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *S3) localPath(key string) (string, error) {
	clean := filepath.Clean(strings.TrimPrefix(key, "/"))
	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", os.ErrPermission
	}
	root, err := filepath.Abs(s.localDir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, clean)
	if !strings.HasPrefix(path, root) {
		return "", os.ErrPermission
	}
	return path, nil
}
