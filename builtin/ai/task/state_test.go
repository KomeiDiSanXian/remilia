package task

import "testing"

func TestTransitionTableCoversAllStatuses(t *testing.T) {
	for _, s := range allStatuses {
		if !s.Valid() {
			t.Fatalf("status %q should be valid", s)
		}
		if _, ok := taskTransitions[s]; !ok {
			t.Fatalf("status %q has no transition entry", s)
		}
	}
	if len(taskTransitions) != len(allStatuses) {
		t.Fatalf("transition table has %d entries, want %d", len(taskTransitions), len(allStatuses))
	}
}

func TestCanTransitionLegalEdges(t *testing.T) {
	for from, targets := range taskTransitions {
		for _, to := range targets {
			if !CanTransition(from, to) {
				t.Fatalf("edge %s -> %s should be legal", from, to)
			}
		}
	}
}

func TestTerminalStatusesHaveNoOutgoingEdges(t *testing.T) {
	for _, s := range allStatuses {
		if !s.IsTerminal() {
			continue
		}
		if len(taskTransitions[s]) != 0 {
			t.Fatalf("terminal status %q must not have outgoing edges: %v", s, taskTransitions[s])
		}
	}
}

func TestIllegalTransitions(t *testing.T) {
	cases := []struct {
		from, to Status
	}{
		{StatusCancelled, StatusSucceeded},
		{StatusCancelled, StatusRunning},
		{StatusSucceeded, StatusRunning},
		{StatusFailed, StatusRunning},
		{StatusRunning, StatusRunning},
		{StatusRunning, StatusPlanning},
		{Status("bogus"), StatusRunning},
	}
	for _, c := range cases {
		if CanTransition(c.from, c.to) {
			t.Fatalf("transition %s -> %s must be illegal", c.from, c.to)
		}
	}
}

func TestEveryActiveStatusCanReachTerminal(t *testing.T) {
	// 取消与失败必须随时可达；否则"停止"会在某些状态下卡住。
	for _, s := range allStatuses {
		if s.IsTerminal() {
			continue
		}
		if !CanTransition(s, StatusCancelled) {
			t.Fatalf("active status %q cannot be cancelled", s)
		}
		if !CanTransition(s, StatusFailed) {
			t.Fatalf("active status %q cannot fail", s)
		}
	}
}

func TestStatusTerminalAndValid(t *testing.T) {
	terminal := map[Status]bool{
		StatusSucceeded: true, StatusFailed: true, StatusCancelled: true,
	}
	for _, s := range allStatuses {
		if s.IsTerminal() != terminal[s] {
			t.Fatalf("IsTerminal(%q) = %v", s, s.IsTerminal())
		}
	}
	if Status("bogus").Valid() {
		t.Fatal("unknown status should not be valid")
	}
}

func TestParseStatus(t *testing.T) {
	for _, s := range allStatuses {
		got, err := ParseStatus(string(s))
		if err != nil || got != s {
			t.Fatalf("ParseStatus(%q) = (%q, %v)", s, got, err)
		}
	}
	if _, err := ParseStatus("bogus"); err == nil {
		t.Fatal("ParseStatus should reject unknown values")
	}
}

func TestStepStatusValidAndTerminal(t *testing.T) {
	terminal := map[StepStatus]bool{
		StepSucceeded: true, StepFailed: true, StepSkipped: true, StepCancelled: true,
	}
	for _, s := range allStepStatuses {
		if !s.Valid() {
			t.Fatalf("step status %q should be valid", s)
		}
		if s.IsTerminal() != terminal[s] {
			t.Fatalf("IsTerminal(%q) = %v", s, s.IsTerminal())
		}
	}
	if StepStatus("bogus").Valid() {
		t.Fatal("unknown step status should not be valid")
	}
}
