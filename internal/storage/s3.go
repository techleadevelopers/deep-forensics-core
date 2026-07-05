package storage

import (
	"bytes"
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/verifood/verifood/internal/config"
)

// S3 abstrai o cliente para S3/MinIO (compatível).
type S3 struct {
	client *s3.Client
	bucket string
	base   string
}

// NewS3 constrói o cliente e valida se o bucket existe (silenciosamente).
func NewS3(ctx context.Context, cfg *config.Config) (*S3, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.S3Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, "")),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
			o.UsePathStyle = true
		}
	})
	return &S3{client: client, bucket: cfg.S3Bucket, base: cfg.S3Endpoint}, nil
}

// Put faz upload e retorna a URL pública (não assinada) do objeto.
func (s *S3) Put(ctx context.Context, key string, body []byte, contentType string) (string, error) {
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

// Get baixa um objeto por key.
func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
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
