package judge

import (
	"testing"
	"time"
)

func ac(id int64) TestOutcome { return TestOutcome{TestCaseID: id, Verdict: VerdictAC, TimeMs: 5} }

func TestOutcomeValidate(t *testing.T) {
	good := []Outcome{
		{Verdict: VerdictAC, PassedTests: 3, TotalTests: 3, Tests: []TestOutcome{ac(1), ac(2), ac(3)}},
		{Verdict: VerdictCE, PassedTests: 0, TotalTests: 3},
		// 10/15 TLE style: stops at the first failing test.
		{Verdict: VerdictTLE, PassedTests: 1, TotalTests: 3, Tests: []TestOutcome{ac(1), {TestCaseID: 2, Verdict: VerdictTLE}}},
		{Verdict: VerdictWA, PassedTests: 0, TotalTests: 3, Tests: []TestOutcome{{TestCaseID: 1, Verdict: VerdictWA}}},
	}
	for i, o := range good {
		if err := o.Validate(3); err != nil {
			t.Errorf("good[%d]: %v", i, err)
		}
	}
	bad := map[string]Outcome{
		"total mismatch":       {Verdict: VerdictAC, PassedTests: 2, TotalTests: 2, Tests: []TestOutcome{ac(1), ac(2)}},
		"AC missing test":      {Verdict: VerdictAC, PassedTests: 3, TotalTests: 3, Tests: []TestOutcome{ac(1), ac(2)}},
		"CE with tests":        {Verdict: VerdictCE, TotalTests: 3, Tests: []TestOutcome{ac(1)}},
		"CE with passed":       {Verdict: VerdictCE, PassedTests: 1, TotalTests: 3},
		"ran past first fail":  {Verdict: VerdictWA, PassedTests: 1, TotalTests: 3, Tests: []TestOutcome{ac(1), {TestCaseID: 2, Verdict: VerdictWA}, ac(3)}},
		"verdict != last test": {Verdict: VerdictWA, PassedTests: 1, TotalTests: 3, Tests: []TestOutcome{ac(1), {TestCaseID: 2, Verdict: VerdictRE}}},
		"failure before last":  {Verdict: VerdictWA, PassedTests: 1, TotalTests: 3, Tests: []TestOutcome{{TestCaseID: 1, Verdict: VerdictRE}, {TestCaseID: 2, Verdict: VerdictWA}}},
		"WA with all passed":   {Verdict: VerdictWA, PassedTests: 3, TotalTests: 3, Tests: []TestOutcome{ac(1), ac(2), ac(3)}},
		"unknown verdict":      {Verdict: "OK", PassedTests: 3, TotalTests: 3, Tests: []TestOutcome{ac(1), ac(2), ac(3)}},
		"negative time":        {Verdict: VerdictAC, PassedTests: 1, TotalTests: 1, Tests: []TestOutcome{{TestCaseID: 1, Verdict: VerdictAC, TimeMs: -1}}},
		"no tests in snapshot": {Verdict: VerdictCE, TotalTests: 0},
	}
	for name, o := range bad {
		total := 3
		if name == "negative time" {
			total = 1
		}
		if name == "no tests in snapshot" {
			total = 0
		}
		if err := o.Validate(total); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRetryPolicyDelay(t *testing.T) {
	p := DefaultRetry
	want := []time.Duration{5 * time.Second, 5 * time.Second, 30 * time.Second, 2 * time.Minute, 2 * time.Minute}
	for i, attempt := range []int{0, 1, 2, 3, 9} {
		if got := p.Delay(attempt); got != want[i] {
			t.Errorf("Delay(%d) = %v, want %v", attempt, got, want[i])
		}
	}
	if p.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", p.MaxAttempts)
	}
	if (RetryPolicy{MaxAttempts: 1}).Delay(1) != 0 {
		t.Error("empty backoff should be 0")
	}
}

func TestDisabledJudgerDisablesClaiming(t *testing.T) {
	if NewService(nil, DisabledJudger{}, DefaultRetry, time.Minute).Enabled() {
		t.Error("service with DisabledJudger must not claim")
	}
}
