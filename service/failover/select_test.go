package failover

import "testing"

func health(up, down int) MemberHealth {
	return MemberHealth{ConsecutiveUp: up, ConsecutiveDown: down}
}

func TestSelectMember(t *testing.T) {
	tests := []struct {
		name     string
		input    DecisionInput
		target   string
		switches bool
		allDown  bool
		reason   string
	}{
		{"sticky primary", DecisionInput{[]string{"a", "b"}, map[string]MemberHealth{"a": health(2, 0)}, "a", 2, "direct"}, "a", false, false, "sticky"},
		{"failover", DecisionInput{[]string{"a", "b"}, map[string]MemberHealth{"a": health(0, 1), "b": health(1, 0)}, "a", 2, "direct"}, "b", true, false, "failover"},
		{"recovery waits", DecisionInput{[]string{"a", "b"}, map[string]MemberHealth{"a": health(1, 0), "b": health(2, 0)}, "b", 2, "direct"}, "b", false, false, "sticky"},
		{"confirmed failback", DecisionInput{[]string{"a", "b"}, map[string]MemberHealth{"a": health(2, 0), "b": health(2, 0)}, "b", 2, "direct"}, "a", true, false, "failback"},
		{"all down direct", DecisionInput{[]string{"a", "b"}, map[string]MemberHealth{}, "a", 2, "direct"}, "direct", true, true, "all_down_direct"},
		{"all down hold", DecisionInput{[]string{"a", "b"}, map[string]MemberHealth{}, "a", 2, ""}, "a", false, true, "all_down_hold"},
		{"cold start", DecisionInput{[]string{"a", "b"}, map[string]MemberHealth{"a": health(1, 0)}, "", 2, "direct"}, "a", true, false, "priority"},
		{"return from direct", DecisionInput{[]string{"a", "b"}, map[string]MemberHealth{"a": health(1, 0)}, "direct", 2, "direct"}, "a", true, false, "failback"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := SelectMember(test.input)
			if got.Target != test.target || got.ShouldSwitch != test.switches || got.AllDown != test.allDown || got.Reason != test.reason {
				t.Fatalf("got %+v", got)
			}
		})
	}
}
