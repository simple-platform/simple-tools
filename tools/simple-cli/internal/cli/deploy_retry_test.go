package cli

import "testing"

func TestAlreadyInstalledRe(t *testing.T) {
	msg := "Version `1.3.0-dev.21` of application `com.bnv.employee_hub` is already installed"
	m := alreadyInstalledRe.FindStringSubmatch(msg)
	if m == nil || m[1] != "1.3.0-dev.21" {
		t.Fatalf("failed to extract version from %q, got %v", msg, m)
	}
	if alreadyInstalledRe.FindStringSubmatch("some other error") != nil {
		t.Fatal("matched an unrelated error")
	}
}
