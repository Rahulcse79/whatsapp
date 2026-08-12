package media

import (
	"context"
	"sort"
	"testing"
)

type fakeStickerStore struct {
	packs     map[string]StickerPack
	installed map[string]map[string]bool // userID → packID set
	err       error
}

func newFakeStickerStore() *fakeStickerStore {
	return &fakeStickerStore{packs: map[string]StickerPack{}, installed: map[string]map[string]bool{}}
}

func (s *fakeStickerStore) ListPacks(_ context.Context) ([]StickerPack, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]StickerPack, 0, len(s.packs))
	for _, p := range s.packs {
		p.Stickers = nil // catalog omits contents
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *fakeStickerStore) GetPack(_ context.Context, packID string) (StickerPack, error) {
	p, ok := s.packs[packID]
	if !ok {
		return StickerPack{}, ErrNotFound
	}
	return p, nil
}

func (s *fakeStickerStore) PackExists(_ context.Context, packID string) (bool, error) {
	_, ok := s.packs[packID]
	return ok, nil
}

func (s *fakeStickerStore) Install(_ context.Context, userID, packID string) error {
	if s.installed[userID] == nil {
		s.installed[userID] = map[string]bool{}
	}
	s.installed[userID][packID] = true
	return nil
}

func (s *fakeStickerStore) Uninstall(_ context.Context, userID, packID string) error {
	delete(s.installed[userID], packID)
	return nil
}

func (s *fakeStickerStore) ListInstalled(_ context.Context, userID string) ([]StickerPack, error) {
	var out []StickerPack
	for id := range s.installed[userID] {
		p := s.packs[id]
		p.Stickers = nil
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func seededStickers() *fakeStickerStore {
	st := newFakeStickerStore()
	st.packs["classic"] = StickerPack{ID: "classic", Title: "Classic", Stickers: []Sticker{{ID: "c1", Emoji: "👍", ObjectKey: "stickers/classic/01.webp"}}}
	st.packs["cats"] = StickerPack{ID: "cats", Title: "Cats", Animated: true}
	return st
}

func TestStickers_ListsCatalogWithoutContents(t *testing.T) {
	s := NewStickerService(seededStickers())
	packs, err := s.Packs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 2 || packs[0].ID != "cats" || packs[1].ID != "classic" {
		t.Fatalf("bad catalog: %+v", packs)
	}
	if packs[1].Stickers != nil {
		t.Fatal("catalog list must omit sticker contents")
	}
}

func TestStickers_PackDetailAndNotFound(t *testing.T) {
	s := NewStickerService(seededStickers())

	pack, err := s.Pack(context.Background(), "classic")
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Stickers) != 1 || pack.Stickers[0].Emoji != "👍" {
		t.Fatalf("pack detail missing stickers: %+v", pack)
	}

	if got := code(t, mustPackErr(s.Pack(context.Background(), "nope"))); got != "STICKER_PACK_NOT_FOUND" {
		t.Fatalf("unknown pack code = %q, want STICKER_PACK_NOT_FOUND", got)
	}
	if got := code(t, mustPackErr(s.Pack(context.Background(), ""))); got != "VALIDATION_PACK" {
		t.Fatalf("empty id code = %q, want VALIDATION_PACK", got)
	}
}

func TestStickers_InstallUnknownIs404(t *testing.T) {
	s := NewStickerService(seededStickers())
	if got := code(t, s.Install(context.Background(), ident("u1"), "nope")); got != "STICKER_PACK_NOT_FOUND" {
		t.Fatalf("code = %q, want STICKER_PACK_NOT_FOUND", got)
	}
}

func TestStickers_InstallIdempotentThenUninstall(t *testing.T) {
	store := seededStickers()
	s := NewStickerService(store)
	ctx := context.Background()

	if err := s.Install(ctx, ident("u1"), "classic"); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(ctx, ident("u1"), "classic"); err != nil { // idempotent
		t.Fatal(err)
	}
	installed, err := s.Installed(ctx, ident("u1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 1 || installed[0].ID != "classic" {
		t.Fatalf("installed = %+v, want [classic]", installed)
	}

	if err := s.Uninstall(ctx, ident("u1"), "classic"); err != nil {
		t.Fatal(err)
	}
	if err := s.Uninstall(ctx, ident("u1"), "classic"); err != nil { // idempotent
		t.Fatal(err)
	}
	installed, _ = s.Installed(ctx, ident("u1"))
	if len(installed) != 0 {
		t.Fatalf("installed after uninstall = %+v, want empty", installed)
	}
}

func mustPackErr(_ StickerPack, err error) error { return err }
