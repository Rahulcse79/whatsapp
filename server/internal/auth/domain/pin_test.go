package domain

import (
	"crypto/rand"
	"strings"
	"testing"
)

func TestPIN_RoundTrip(t *testing.T) {
	phc, err := HashPIN("482913", rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(phc, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Fatalf("PHC header wrong: %s", phc)
	}
	ok, err := CheckPIN(phc, "482913")
	if err != nil || !ok {
		t.Fatalf("correct pin rejected: ok=%v err=%v", ok, err)
	}
	ok, err = CheckPIN(phc, "482914")
	if err != nil || ok {
		t.Fatalf("wrong pin accepted: ok=%v err=%v", ok, err)
	}
}

func TestPIN_TooShort(t *testing.T) {
	if _, err := HashPIN("123", rand.Reader); err == nil {
		t.Fatal("3-char pin accepted; minimum is 4")
	}
}

func TestPIN_UniqueSalts(t *testing.T) {
	a, _ := HashPIN("482913", rand.Reader)
	b, _ := HashPIN("482913", rand.Reader)
	if a == b {
		t.Fatal("two hashes of the same pin must differ (random salt)")
	}
}

func TestCheckPIN_Malformed(t *testing.T) {
	for _, bad := range []string{
		"",
		"plainhash",
		"$argon2i$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA",       // wrong variant
		"$argon2id$v=18$m=65536,t=3,p=4$c2FsdA$aGFzaA",      // wrong version
		"$argon2id$v=19$m=banana,t=3,p=4$c2FsdA$aGFzaA",     // bad params
		"$argon2id$v=19$m=65536,t=3,p=4$!!notb64!!$aGFzaA",  // bad salt
	} {
		if ok, err := CheckPIN(bad, "482913"); err == nil || ok {
			t.Fatalf("malformed hash %q: ok=%v err=%v — must error", bad, ok, err)
		}
	}
}
