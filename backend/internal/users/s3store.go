package users

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/encrypt"
)

// S3Config configures S3Store (AWS S3, MinIO, R2, Spaces, ... anything that
// speaks the S3 API).
type S3Config struct {
	Endpoint  string // host[:port], no scheme: "s3.eu-west-1.amazonaws.com"
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
	Prefix    string // key prefix, e.g. "photos" or "chat"
	// SSE asks the store for server-side encryption (AES-256, SSE-S3). Not
	// every S3-compatible server supports it (MinIO needs a KMS).
	SSE bool
}

// S3Store is a MediaStore backed by an object store. Keys are
// "<prefix>/<uuid>.jpg"; ids are validated as UUIDs, so callers can never
// choose an arbitrary key.
type S3Store struct {
	c   *minio.Client
	cfg S3Config
}

func NewS3Store(cfg S3Config) (*S3Store, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("s3: endpoint, bucket and credentials are required")
	}
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, err
	}
	return &S3Store{c: c, cfg: cfg}, nil
}

// WithPrefix returns a store writing under a different key prefix, sharing the connection.
func (s *S3Store) WithPrefix(p string) *S3Store {
	cp := *s
	cp.cfg.Prefix = p
	return &cp
}

func (s *S3Store) key(id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", ErrNotFound
	}
	k := id + ".jpg"
	if p := strings.Trim(s.cfg.Prefix, "/"); p != "" {
		k = p + "/" + k
	}
	return k, nil
}

func (s *S3Store) Put(ctx context.Context, id string, data []byte) error {
	k, err := s.key(id)
	if err != nil {
		return err
	}
	opts := minio.PutObjectOptions{ContentType: "image/jpeg"}
	if s.cfg.SSE {
		opts.ServerSideEncryption = encrypt.NewSSE()
	}
	_, err = s.c.PutObject(ctx, s.cfg.Bucket, k, bytes.NewReader(data), int64(len(data)), opts)
	return err
}

func (s *S3Store) Get(ctx context.Context, id string) ([]byte, error) {
	k, err := s.key(id)
	if err != nil {
		return nil, err
	}
	o, err := s.c.GetObject(ctx, s.cfg.Bucket, k, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer o.Close()
	b, err := io.ReadAll(io.LimitReader(o, 32<<20))
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return b, nil
}

func (s *S3Store) Delete(ctx context.Context, id string) error {
	k, err := s.key(id)
	if err != nil {
		return err
	}
	// Removing a missing key is not an error in S3.
	return s.c.RemoveObject(ctx, s.cfg.Bucket, k, minio.RemoveObjectOptions{})
}

// EnsureBucket creates the bucket if it does not exist (tests and first-run
// tooling; production buckets are provisioned with their own policies).
func (s *S3Store) EnsureBucket(ctx context.Context) error {
	ok, err := s.c.BucketExists(ctx, s.cfg.Bucket)
	if err != nil || ok {
		return err
	}
	return s.c.MakeBucket(ctx, s.cfg.Bucket, minio.MakeBucketOptions{Region: s.cfg.Region})
}
