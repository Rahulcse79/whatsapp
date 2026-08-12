package adapters

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whatsapp-v2/server/internal/media"
)

// StickerStore implements media.StickerStore over sticker_packs / stickers /
// user_sticker_packs (migration 000012). The catalog is shared public content;
// user_sticker_packs is the per-user install set.
type StickerStore struct{ pool *pgxpool.Pool }

func NewStickerStore(pool *pgxpool.Pool) *StickerStore { return &StickerStore{pool: pool} }

const selectPackCols = `id, title, author, tray_key, animated`

func (s *StickerStore) ListPacks(ctx context.Context) ([]media.StickerPack, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+selectPackCols+` FROM sticker_packs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPacks(rows)
}

func (s *StickerStore) GetPack(ctx context.Context, packID string) (media.StickerPack, error) {
	var p media.StickerPack
	err := s.pool.QueryRow(ctx, `SELECT `+selectPackCols+` FROM sticker_packs WHERE id = $1`, packID).
		Scan(&p.ID, &p.Title, &p.Author, &p.TrayKey, &p.Animated)
	if errors.Is(err, pgx.ErrNoRows) {
		return media.StickerPack{}, media.ErrNotFound
	}
	if err != nil {
		return media.StickerPack{}, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, emoji, object_key FROM stickers WHERE pack_id = $1 ORDER BY position, id`, packID)
	if err != nil {
		return media.StickerPack{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var st media.Sticker
		if err := rows.Scan(&st.ID, &st.Emoji, &st.ObjectKey); err != nil {
			return media.StickerPack{}, err
		}
		p.Stickers = append(p.Stickers, st)
	}
	return p, rows.Err()
}

func (s *StickerStore) PackExists(ctx context.Context, packID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sticker_packs WHERE id = $1)`, packID).Scan(&exists)
	return exists, err
}

func (s *StickerStore) Install(ctx context.Context, userID, packID string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_sticker_packs (user_id, pack_id) VALUES ($1, $2)
		 ON CONFLICT (user_id, pack_id) DO NOTHING`, userID, packID)
	return err
}

func (s *StickerStore) Uninstall(ctx context.Context, userID, packID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM user_sticker_packs WHERE user_id = $1 AND pack_id = $2`, userID, packID)
	return err
}

func (s *StickerStore) ListInstalled(ctx context.Context, userID string) ([]media.StickerPack, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT p.id, p.title, p.author, p.tray_key, p.animated
		 FROM sticker_packs p
		 JOIN user_sticker_packs u ON u.pack_id = p.id
		 WHERE u.user_id = $1
		 ORDER BY u.installed_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPacks(rows)
}

func scanPacks(rows pgx.Rows) ([]media.StickerPack, error) {
	var out []media.StickerPack
	for rows.Next() {
		var p media.StickerPack
		if err := rows.Scan(&p.ID, &p.Title, &p.Author, &p.TrayKey, &p.Animated); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

var _ media.StickerStore = (*StickerStore)(nil)
