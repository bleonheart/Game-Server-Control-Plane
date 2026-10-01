package steamcmd

import "testing"

func TestProgressBufferCapsOutputAndReportsProgress(t *testing.T) {
	var progress string
	buffer := &progressBuffer{limit: 8, progress: func(value string) { progress = value }}
	if _, err := buffer.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if progress != "abc" {
		t.Fatalf("unexpected progress %q", progress)
	}
	if _, err := buffer.Write([]byte("defghijkl")); err != nil {
		t.Fatal(err)
	}
	if got := buffer.String(); got != "efghijkl" {
		t.Fatalf("unexpected capped output %q", got)
	}
	if progress != "efghijkl" {
		t.Fatalf("unexpected final progress %q", progress)
	}
}
