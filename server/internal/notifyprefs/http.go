package notifyprefs

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/notifyprefs/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// prefsDTO is the notification-prefs wire shape: individual channel booleans
// (friendlier than a raw bitmask) plus the quiet-hours window and sound/vibrate.
type prefsDTO struct {
	Push       bool `json:"push"`
	Email      bool `json:"email"`
	SMS        bool `json:"sms"`
	Desktop    bool `json:"desktop"`
	QuietStart int  `json:"quiet_start_min"` // -1 = off
	QuietEnd   int  `json:"quiet_end_min"`   // -1 = off
	Sound      bool `json:"sound"`
	Vibrate    bool `json:"vibrate"`
}

func toDTO(p domain.Prefs) prefsDTO {
	return prefsDTO{
		Push:       p.Has(domain.ChannelPush),
		Email:      p.Has(domain.ChannelEmail),
		SMS:        p.Has(domain.ChannelSMS),
		Desktop:    p.Has(domain.ChannelDesktop),
		QuietStart: p.QuietStart,
		QuietEnd:   p.QuietEnd,
		Sound:      p.Sound,
		Vibrate:    p.Vibrate,
	}
}

func (d prefsDTO) toDomain() domain.Prefs {
	var ch domain.Channel
	if d.Push {
		ch |= domain.ChannelPush
	}
	if d.Email {
		ch |= domain.ChannelEmail
	}
	if d.SMS {
		ch |= domain.ChannelSMS
	}
	if d.Desktop {
		ch |= domain.ChannelDesktop
	}
	qs, qe := d.QuietStart, d.QuietEnd
	if qs < 0 || qe < 0 { // off is all-or-nothing
		qs, qe = -1, -1
	}
	return domain.Prefs{Channels: ch, QuietStart: qs, QuietEnd: qe, Sound: d.Sound, Vibrate: d.Vibrate}
}

// Routes mounts the notification-preferences surface (T14.01). Bearer-gated; a
// caller only ever reads/writes its own preferences.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("GET /v1/notifications/prefs", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		p, err := s.GetPrefs(r.Context(), ident)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, toDTO(p))
	})

	mux.HandleFunc("PUT /v1/notifications/prefs", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body prefsDTO
		if !decode(w, r, &body) {
			return
		}
		if err := s.SetPrefs(r.Context(), ident, body.toDomain()); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("PUT /v1/conversations/{id}/snooze", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			UntilMS int64 `json:"until_ms"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.SetSnooze(r.Context(), ident, r.PathValue("id"), time.UnixMilli(body.UntilMS)); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/conversations/{id}/snooze", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.ClearSnooze(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /v1/notifications/scheduled", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		list, err := s.ListScheduled(r.Context(), ident)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"scheduled": list})
	})

	mux.HandleFunc("POST /v1/notifications/scheduled", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			ConversationID string `json:"conversation_id"`
			Title          string `json:"title"`
			DueAtMS        int64  `json:"due_at_ms"`
		}
		if !decode(w, r, &body) {
			return
		}
		created, err := s.ScheduleNotification(r.Context(), ident, body.ConversationID, body.Title, time.UnixMilli(body.DueAtMS))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, created)
	})

	mux.HandleFunc("DELETE /v1/notifications/scheduled/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.CancelScheduled(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	if err := dec.Decode(v); err != nil {
		httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_JSON", "invalid JSON body"))
		return false
	}
	return true
}
