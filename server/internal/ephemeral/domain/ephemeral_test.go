package domain

import "testing"

func TestValidateTTL(t *testing.T) {
	for _, ok := range []int{0, 30, TTL24Hour, TTL7Day, TTL90Day} {
		if err := ValidateTTL(ok); err != nil {
			t.Errorf("ValidateTTL(%d) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []int{-1, MaxTTL + 1} {
		if err := ValidateTTL(bad); err == nil {
			t.Errorf("ValidateTTL(%d) = nil, want error", bad)
		}
	}
}

func TestLabelFor(t *testing.T) {
	cases := map[int]string{TTLOff: "off", TTL24Hour: "24h", TTL7Day: "7d", TTL90Day: "90d", 42: "custom"}
	for secs, want := range cases {
		if got := LabelFor(secs); got != want {
			t.Errorf("LabelFor(%d) = %q, want %q", secs, got, want)
		}
	}
}
