package runtime

import "testing"

func TestParseStats(t *testing.T) {
	cpu, memory := parseStats("12.50%|512MiB / 1GiB\n")
	if cpu != 12.5 {
		t.Fatalf("unexpected cpu: %f", cpu)
	}
	if memory != 512*(1<<20) {
		t.Fatalf("unexpected memory: %d", memory)
	}
}
