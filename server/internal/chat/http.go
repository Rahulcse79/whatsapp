package chat

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

// DirectConversations creates (idempotently) the 1:1 conversation for two users
// and returns its id. Both users resolve the SAME conversation via a symmetric
// direct_key, so A→B and B→A land in one place. Satisfied by the chat pg store.
type DirectConversations interface {
	GetOrCreateDirect(ctx context.Context, userA, userB string) (string, error)
}

// Routes mounts the client-facing chat REST surface. The message hot path itself
// rides the WS gateway (gRPC AcceptMessage); this endpoint just hands the client
// the shared conversation_id to send into — the piece that lets two accounts
// actually reach each other (Milestone 1).
func Routes(mux *http.ServeMux, store DirectConversations, v auth.TokenVerifier) {
	mux.HandleFunc("POST /v1/conversations/direct", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			PeerUserID string `json:"peer_user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.Error(w, r, http.StatusBadRequest, httpx.ErrorObj{Code: "VALIDATION_BODY", Message: "invalid JSON body"})
			return
		}
		if _, err := id.Parse(body.PeerUserID); err != nil {
			httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_PEER", "peer_user_id must be a UUID"))
			return
		}
		if body.PeerUserID == ident.UserID {
			httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_SELF", "cannot start a direct conversation with yourself"))
			return
		}
		convID, err := store.GetOrCreateDirect(r.Context(), ident.UserID, body.PeerUserID)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"conversation_id": convID})
	})
}
