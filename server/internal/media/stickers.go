package media

import (
	"context"
	"errors"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Sticker packs (FR-MED-05). Stickers and GIFs are *public* catalog content, not
// E2EE user media — a sticker's object key points at a shared asset in object
// storage, not at the encrypted media_objects a message carries. Local packs are
// the offline fallback the air-gap profile keeps when the GIF proxy is disabled.

// Sticker is one image in a pack (object_key is a shared, non-E2EE asset key).
type Sticker struct {
	ID        string `json:"id"`
	Emoji     string `json:"emoji"`
	ObjectKey string `json:"object_key"`
}

// StickerPack is a catalog pack. Stickers is populated for pack detail and
// omitted from list responses (catalog stays small).
type StickerPack struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Author   string    `json:"author"`
	TrayKey  string    `json:"tray_object_key"`
	Animated bool      `json:"animated"`
	Stickers []Sticker `json:"stickers,omitempty"`
}

// StickerStore persists the pack catalog and each user's installed set.
type StickerStore interface {
	ListPacks(ctx context.Context) ([]StickerPack, error)
	GetPack(ctx context.Context, packID string) (StickerPack, error) // ErrNotFound when unknown
	PackExists(ctx context.Context, packID string) (bool, error)
	Install(ctx context.Context, userID, packID string) error // idempotent
	Uninstall(ctx context.Context, userID, packID string) error
	ListInstalled(ctx context.Context, userID string) ([]StickerPack, error)
}

// StickerService serves the pack catalog and per-user install list.
type StickerService struct {
	store StickerStore
}

func NewStickerService(store StickerStore) *StickerService {
	return &StickerService{store: store}
}

// Packs lists the available catalog (without per-pack sticker contents).
func (s *StickerService) Packs(ctx context.Context) ([]StickerPack, error) {
	packs, err := s.store.ListPacks(ctx)
	if err != nil {
		return nil, httpx.Transient()
	}
	return packs, nil
}

// Pack returns one pack with its stickers, or 404.
func (s *StickerService) Pack(ctx context.Context, packID string) (StickerPack, error) {
	if packID == "" {
		return StickerPack{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_PACK", "pack id required")
	}
	pack, err := s.store.GetPack(ctx, packID)
	if errors.Is(err, ErrNotFound) {
		return StickerPack{}, httpx.Reject(http.StatusNotFound, "STICKER_PACK_NOT_FOUND", "no such sticker pack")
	}
	if err != nil {
		return StickerPack{}, httpx.Transient()
	}
	return pack, nil
}

// Install adds a pack to the caller's installed set (idempotent). Unknown pack → 404.
func (s *StickerService) Install(ctx context.Context, ident auth.Identity, packID string) error {
	if packID == "" {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_PACK", "pack id required")
	}
	exists, err := s.store.PackExists(ctx, packID)
	if err != nil {
		return httpx.Transient()
	}
	if !exists {
		return httpx.Reject(http.StatusNotFound, "STICKER_PACK_NOT_FOUND", "no such sticker pack")
	}
	if err := s.store.Install(ctx, ident.UserID, packID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Uninstall removes a pack from the caller's set (idempotent — a no-op if absent).
func (s *StickerService) Uninstall(ctx context.Context, ident auth.Identity, packID string) error {
	if packID == "" {
		return httpx.Reject(http.StatusBadRequest, "VALIDATION_PACK", "pack id required")
	}
	if err := s.store.Uninstall(ctx, ident.UserID, packID); err != nil {
		return httpx.Transient()
	}
	return nil
}

// Installed lists the caller's installed packs.
func (s *StickerService) Installed(ctx context.Context, ident auth.Identity) ([]StickerPack, error) {
	packs, err := s.store.ListInstalled(ctx, ident.UserID)
	if err != nil {
		return nil, httpx.Transient()
	}
	return packs, nil
}
