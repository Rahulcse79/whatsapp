package flags

import (
	"fmt"
	"testing"
)

func TestRuleEvalPrecedence(t *testing.T) {
	cases := []struct {
		name string
		rule Rule
		want bool
	}{
		{"disabled beats full rollout", Rule{Enabled: false, Rollout: 100}, false},
		{"enabled + 100% is on", Rule{Enabled: true, Rollout: 100}, true},
		{"enabled + 0% is off", Rule{Enabled: true, Rollout: 0}, false},
		{"deny beats rollout", Rule{Enabled: true, Rollout: 100, Deny: []string{"u"}}, false},
		{"allow beats 0% rollout", Rule{Enabled: true, Rollout: 0, Allow: []string{"u"}}, true},
		{"deny beats allow", Rule{Enabled: true, Allow: []string{"u"}, Deny: []string{"u"}}, false},
	}
	for _, c := range cases {
		if got := c.rule.Eval("feature", "u"); got != c.want {
			t.Errorf("%s: Eval = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestRolloutMonotonicAndProportional: a rollout only ever grows its cohort (a
// subject on at N stays on for every M>N), and ~N% of subjects are on at N.
func TestRolloutMonotonicAndProportional(t *testing.T) {
	subjects := make([]string, 2000)
	for i := range subjects {
		subjects[i] = fmt.Sprintf("user-%d", i)
	}
	on := func(rollout int, s string) bool { return Rule{Enabled: true, Rollout: rollout}.Eval("feat", s) }

	// Monotonic: nobody drops out as the rollout widens.
	for _, s := range subjects {
		for r := 0; r < 100; r++ {
			if on(r, s) && !on(r+1, s) {
				t.Fatalf("%s left the cohort going %d%%→%d%%", s, r, r+1)
			}
		}
	}

	// Proportional: ~50% on at 50% (loose bound tolerating hash variance).
	count := 0
	for _, s := range subjects {
		if on(50, s) {
			count++
		}
	}
	if count < len(subjects)*40/100 || count > len(subjects)*60/100 {
		t.Errorf("at 50%% rollout, %d/%d on — outside 40–60%%", count, len(subjects))
	}
}

// TestFlagScopingIndependentCohorts: two flags at the same percentage must not
// select the identical cohort — the flag name is folded into the bucket hash.
func TestFlagScopingIndependentCohorts(t *testing.T) {
	diff := 0
	for i := 0; i < 2000; i++ {
		s := fmt.Sprintf("user-%d", i)
		a := Rule{Enabled: true, Rollout: 50}.Eval("flag-a", s)
		b := Rule{Enabled: true, Rollout: 50}.Eval("flag-b", s)
		if a != b {
			diff++
		}
	}
	if diff == 0 {
		t.Error("flag name not folded into the cohort — every flag targets the same users")
	}
}
