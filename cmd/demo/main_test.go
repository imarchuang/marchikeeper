package main

import "testing"

func TestDemoStandbyBecomesLeader(t *testing.T) {
	if err := run(); err != nil {
		t.Fatal(err)
	}
}
