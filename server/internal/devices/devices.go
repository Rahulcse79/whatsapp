// Package devices owns the multi-device registry: listing, renaming, atomic
// revocation, and the QR-based device-linking flow (init → approve → complete).
// A user has 1 primary + up to 4 linked devices; the primary signs every
// linked device's certificate (e2ee-design §5).
package devices

import "time"

// Max devices per user: 1 primary + 4 linked (FR-AUTH-05).
const MaxDevicesPerUser = 5

// LinkTTL bounds how long a QR link stays valid.
const LinkTTL = 5 * time.Minute

// Platform codes mirror the devices.platform column.
const (
	PlatformIOS     int16 = 0
	PlatformAndroid int16 = 1
	PlatformWeb     int16 = 2
)

// Device is the registry row.
type Device struct {
	ID           string
	UserID       string
	IsPrimary    bool
	Platform     int16
	Name         string
	RegisteredAt time.Time
	LastActiveAt time.Time
	Revoked      bool
}

// View is the client-facing device shape (GET /v1/devices).
type View struct {
	ID         string `json:"id"`
	IsPrimary  bool   `json:"is_primary"`
	Platform   string `json:"platform"`
	Name       string `json:"name"`
	LastActive int64  `json:"last_active_ms,omitempty"`
}

// LinkState tracks a pending QR link.
type LinkState int16

const (
	LinkPending  LinkState = 0
	LinkApproved LinkState = 1
	LinkConsumed LinkState = 2
)

// Link is the QR handoff row.
type Link struct {
	Token       string
	Platform    int16
	Name        string
	IdentityKey []byte
	State       LinkState
	ApprovedBy  string
	UserID      string
	DeviceID    string
	Cert        []byte
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

func platformName(p int16) string {
	switch p {
	case PlatformIOS:
		return "ios"
	case PlatformAndroid:
		return "android"
	case PlatformWeb:
		return "web"
	default:
		return "unknown"
	}
}

func platformCode(name string) (int16, bool) {
	switch name {
	case "ios":
		return PlatformIOS, true
	case "android":
		return PlatformAndroid, true
	case "web":
		return PlatformWeb, true
	}
	return 0, false
}
