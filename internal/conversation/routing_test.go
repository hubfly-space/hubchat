package conversation

import "testing"

func TestChooseRoutingMember(t *testing.T) {
	members := []routingMember{{id: "mem_a", weight: 1, assigned: 4}, {id: "mem_b", weight: 1, assigned: 1}}
	if got := chooseRoutingMember("least_active", "cnv_1", members); got != "mem_b" {
		t.Fatalf("least-active member = %q, want mem_b", got)
	}
	weighted := []routingMember{{id: "mem_a", weight: 4, assigned: 3}, {id: "mem_b", weight: 1, assigned: 1}}
	if got := chooseRoutingMember("weighted", "cnv_2", weighted); got != "mem_a" {
		t.Fatalf("weighted member = %q, want mem_a", got)
	}
	if got := chooseRoutingMember("round_robin", "cnv_3", members); got != "mem_b" {
		t.Fatalf("round-robin member = %q, want mem_b", got)
	}
}

func TestStableIndexIsDeterministic(t *testing.T) {
	if stableIndex("conversation", 7) != stableIndex("conversation", 7) {
		t.Fatal("stableIndex returned different values for the same input")
	}
	if stableIndex("conversation", 0) != 0 {
		t.Fatal("stableIndex should safely handle an empty member list")
	}
}
