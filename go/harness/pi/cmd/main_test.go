package main

import "testing"

func TestValidateSessionID(t *testing.T) {
	for _, id := range []string{"session-123", "abc_DEF-456"} {
		if err := validateSessionID(id); err != nil {
			t.Errorf("validateSessionID(%q) = %v", id, err)
		}
	}
	for _, id := range []string{"", "session 123", "session\n123"} {
		if err := validateSessionID(id); err == nil {
			t.Errorf("validateSessionID(%q) unexpectedly succeeded", id)
		}
	}
}
