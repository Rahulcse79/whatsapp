package adapters

import (
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/whatsapp-v2/server/internal/media"
)

// MinIO implements media.Objects. Blobs never transit media-svc — this only
// orchestrates multipart uploads and mints presigned URLs the client uses
// directly against MinIO (media-svc-lld §1).
type MinIO struct {
	client *minio.Client
	core   *minio.Core
	bucket string
}

func NewMinIO(endpoint, accessKey, secretKey, bucket string, secure bool) (*MinIO, error) {
	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	}
	client, err := minio.New(endpoint, opts)
	if err != nil {
		return nil, err
	}
	core, err := minio.NewCore(endpoint, opts)
	if err != nil {
		return nil, err
	}
	return &MinIO{client: client, core: core, bucket: bucket}, nil
}

func (m *MinIO) StartUpload(ctx context.Context, key string, parts int, expires time.Duration) (string, []media.PartURL, error) {
	handle, err := m.core.NewMultipartUpload(ctx, m.bucket, key, minio.PutObjectOptions{})
	if err != nil {
		return "", nil, err
	}
	nums := make([]int, parts)
	for i := range nums {
		nums[i] = i + 1
	}
	urls, err := m.presignParts(ctx, key, handle, nums, expires)
	if err != nil {
		return "", nil, err
	}
	return handle, urls, nil
}

func (m *MinIO) PresignParts(ctx context.Context, key, handle string, partNumbers []int, expires time.Duration) ([]media.PartURL, error) {
	return m.presignParts(ctx, key, handle, partNumbers, expires)
}

func (m *MinIO) presignParts(ctx context.Context, key, handle string, nums []int, expires time.Duration) ([]media.PartURL, error) {
	out := make([]media.PartURL, 0, len(nums))
	for _, n := range nums {
		vals := url.Values{}
		vals.Set("uploadId", handle)
		vals.Set("partNumber", strconv.Itoa(n))
		u, err := m.client.Presign(ctx, http.MethodPut, m.bucket, key, expires, vals)
		if err != nil {
			return nil, err
		}
		out = append(out, media.PartURL{PartNumber: n, URL: u.String()})
	}
	return out, nil
}

func (m *MinIO) Complete(ctx context.Context, key, handle string, etags []media.PartETag) error {
	parts := make([]minio.CompletePart, len(etags))
	for i, e := range etags {
		parts[i] = minio.CompletePart{PartNumber: e.PartNumber, ETag: e.ETag}
	}
	_, err := m.core.CompleteMultipartUpload(ctx, m.bucket, key, handle, parts, minio.PutObjectOptions{})
	return err
}

func (m *MinIO) Stat(ctx context.Context, key string) (int64, error) {
	info, err := m.client.StatObject(ctx, m.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, err
	}
	return info.Size, nil
}

func (m *MinIO) Hash(ctx context.Context, key string) ([]byte, error) {
	obj, err := m.client.GetObject(ctx, m.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = obj.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, obj); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func (m *MinIO) PresignGet(ctx context.Context, key string, expires time.Duration) (string, error) {
	u, err := m.client.PresignedGetObject(ctx, m.bucket, key, expires, url.Values{})
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (m *MinIO) Abort(ctx context.Context, key, handle string) error {
	return m.core.AbortMultipartUpload(ctx, m.bucket, key, handle)
}

func (m *MinIO) Remove(ctx context.Context, key string) error {
	return m.client.RemoveObject(ctx, m.bucket, key, minio.RemoveObjectOptions{})
}

var _ media.Objects = (*MinIO)(nil)
