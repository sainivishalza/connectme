package users

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

// Opt-in against a real S3 API:
//
//	docker run --rm -p 9000:9000 -e MINIO_ROOT_USER=minio -e MINIO_ROOT_PASSWORD=minio12345 minio/minio server /data
//	TEST_S3_ENDPOINT=127.0.0.1:9000 TEST_S3_ACCESS=minio TEST_S3_SECRET=minio12345 go test ./internal/users -run S3
func TestS3StoreRoundTrip(t *testing.T) {
	ep := os.Getenv("TEST_S3_ENDPOINT")
	if ep == "" {
		t.Skip("TEST_S3_ENDPOINT not set")
	}
	cfg := S3Config{Endpoint: ep, Bucket: "connectme-test", AccessKey: os.Getenv("TEST_S3_ACCESS"),
		SecretKey: os.Getenv("TEST_S3_SECRET"), Prefix: "photos"}
	s, err := NewS3Store(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if ok, _ := s.c.BucketExists(ctx, cfg.Bucket); !ok {
		if err := s.c.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	id := uuid.NewString()
	data := bytes.Repeat([]byte{0xff, 0xd8, 0xff, 0x01}, 1000)
	if err := s.Put(ctx, id, data); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, id)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("round trip failed: %v (len %d)", err, len(got))
	}
	// A different prefix is a different namespace.
	if _, err := s.WithPrefix("chat").Get(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("other prefix returned %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, id); err != nil {
		t.Errorf("deleting a missing object should succeed: %v", err)
	}
	// Ids that are not UUIDs can never become object keys.
	for _, bad := range []string{"../../etc/passwd", "a/b", "", "photo"} {
		if err := s.Put(ctx, bad, data); !errors.Is(err, ErrNotFound) {
			t.Errorf("id %q accepted: %v", bad, err)
		}
	}
}
