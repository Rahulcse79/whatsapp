package keys

// Prekey material is PUBLIC only — private keys never leave the device
// (e2ee-design §1). This context stores and distributes public bundles.

// SignedPrekey is a device's medium-term signed prekey.
type SignedPrekey struct {
	KeyID     int32  `json:"key_id"`
	Pubkey    []byte `json:"pubkey"`
	Signature []byte `json:"signature"`
}

// OneTimePrekey is consumed on each new session setup (X3DH).
type OneTimePrekey struct {
	KeyID  int32  `json:"key_id"`
	Pubkey []byte `json:"pubkey"`
}

// DeviceBundle is what a peer needs to open a session with one device.
// OneTimePrekey is nil when the device's pool is exhausted — X3DH still
// works with reduced initial forward secrecy, and the client is nudged to
// replenish (low-water hint, wired at T0.10).
type DeviceBundle struct {
	DeviceID      string         `json:"device_id"`
	IdentityKey   []byte         `json:"identity_key"`
	SignedPrekey  SignedPrekey   `json:"signed_prekey"`
	OneTimePrekey *OneTimePrekey `json:"one_time_prekey,omitempty"`
}

// MaxOneTimePrekeysPerUpload caps a single publish (matches the client's
// batch size in auth-users-api.md).
const MaxOneTimePrekeysPerUpload = 100

// LowWaterMark is the available-prekey count below which the server hints
// the device to replenish.
const LowWaterMark = 20
