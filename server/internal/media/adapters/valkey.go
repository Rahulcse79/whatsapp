package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/whatsapp-v2/server/internal/media"
)

// ValkeySessions holds pending multipart-upload state (the MinIO handle + the
// expected size/hash) with a 24h TTL. media_objects has no upload-handle column,
// so the transient session lives here.
type ValkeySessions struct{ client *redis.Client }

func NewValkeySessions(client *redis.Client) *ValkeySessions { return &ValkeySessions{client: client} }

func sessionKey(uploadID string) string { return "media_upload:" + uploadID }

func (s *ValkeySessions) Save(ctx context.Context, sess media.UploadSession, ttl time.Duration) error {
	b, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, sessionKey(sess.UploadID), b, ttl).Err()
}

func (s *ValkeySessions) Load(ctx context.Context, uploadID string) (media.UploadSession, error) {
	b, err := s.client.Get(ctx, sessionKey(uploadID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return media.UploadSession{}, media.ErrNotFound
	}
	if err != nil {
		return media.UploadSession{}, err
	}
	var sess media.UploadSession
	if err := json.Unmarshal(b, &sess); err != nil {
		return media.UploadSession{}, err
	}
	return sess, nil
}

func (s *ValkeySessions) Delete(ctx context.Context, uploadID string) error {
	return s.client.Del(ctx, sessionKey(uploadID)).Err()
}
