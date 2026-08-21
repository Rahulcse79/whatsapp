package domain

import "testing"

func TestValidateHandle(t *testing.T) {
	for _, ok := range []string{"news_bot", "abc", "a1_b2"} {
		if err := ValidateHandle(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"ab", "Bad_Caps", "has space", "way_too_long_handle_way_too_long_x", "hy-phen"} {
		if err := ValidateHandle(bad); err == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestValidateWebhookURL(t *testing.T) {
	if err := ValidateWebhookURL("https://bot.example/hook"); err != nil {
		t.Fatalf("valid https rejected: %v", err)
	}
	for _, bad := range []string{"http://bot.example/hook", "ftp://x", "notaurl", ""} {
		if err := ValidateWebhookURL(bad); err == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestValidateInteractive(t *testing.T) {
	if err := ValidateInteractive("pick", 2); err != nil {
		t.Fatalf("valid interactive rejected: %v", err)
	}
	if err := ValidateInteractive("", 2); err != ErrBadText {
		t.Fatal("blank text should fail")
	}
	if err := ValidateInteractive("hi", 0); err != ErrBadButtons {
		t.Fatal("no buttons should fail")
	}
	if err := ValidateInteractive("hi", 4); err != ErrBadButtons {
		t.Fatal("too many buttons should fail")
	}
}

func TestHMACSignVerify(t *testing.T) {
	secret := []byte("s3cr3t")
	payload := []byte(`{"type":"message","text":"hello"}`)
	sig := Sign(secret, payload)
	if !VerifySignature(secret, payload, sig) {
		t.Fatal("valid signature should verify")
	}
	if VerifySignature(secret, payload, sig+"00") {
		t.Fatal("tampered signature should fail")
	}
	if VerifySignature([]byte("wrong"), payload, sig) {
		t.Fatal("wrong secret should fail")
	}
	if VerifySignature(secret, []byte("tampered"), sig) {
		t.Fatal("tampered payload should fail")
	}
}
