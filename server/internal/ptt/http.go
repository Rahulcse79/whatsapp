package ptt

import (
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

// Minter mints an audio-only, server-muted PTT join token. Publish permission is
// flipped by the floor grant via the SFU (not baked into the token), so every
// participant joins muted (calls-ptt-api.md: "all mics pre-negotiated
// server-muted"). Satisfied by a calls.TokenMinter adapter.
type Minter interface {
	Mint(room, identity string) (string, error)
}

// Routes mounts the PTT REST surface (calls-ptt-api.md §PTT). The floor itself is
// driven over WS (PttRequest → Service.Acquire/Heartbeat/Release, routed by the
// gateway); these endpoints create/join the audio-only room.
func Routes(mux *http.ServeMux, s *Service, minter Minter, v auth.TokenVerifier) {
	mux.HandleFunc("POST /v1/ptt/rooms", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		roomID := "ptt-" + id.New()
		token, err := minter.Mint(roomID, ident.UserID+":"+ident.DeviceID)
		if err != nil {
			httpx.WriteError(w, r, httpx.Transient())
			return
		}
		httpx.JSON(w, http.StatusCreated, map[string]any{"room_id": roomID, "join_token": token})
	})

	mux.HandleFunc("POST /v1/ptt/rooms/{id}/join", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		room := r.PathValue("id")
		token, err := minter.Mint(room, ident.UserID+":"+ident.DeviceID)
		if err != nil {
			httpx.WriteError(w, r, httpx.Transient())
			return
		}
		state, err := s.State(r.Context(), room)
		if err != nil {
			httpx.WriteError(w, r, httpx.Transient())
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"join_token": token, "current_speaker": state.CurrentSpeaker, "queue_len": state.QueueLen,
		})
	})
}
