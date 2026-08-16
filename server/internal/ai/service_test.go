package ai

import (
	"context"
	"testing"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/flags"
)

type fakeFlags struct{ allow bool }

func (f fakeFlags) Allowed(_ context.Context, _ flags.KillSwitch, _ string) bool { return f.allow }

func TestConfigHonoursKillSwitch(t *testing.T) {
	on := NewService(fakeFlags{allow: true}).Config(context.Background(), auth.Identity{UserID: "u"})
	if !on.Enabled || on.ServerEndpointAvailable {
		t.Fatalf("kill-switch off → enabled, no server endpoint: %+v", on)
	}
	off := NewService(fakeFlags{allow: false}).Config(context.Background(), auth.Identity{UserID: "u"})
	if off.Enabled {
		t.Fatalf("kill-switch tripped → AI disabled: %+v", off)
	}
}
