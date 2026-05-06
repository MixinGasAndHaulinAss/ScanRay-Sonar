package notify

import (
	"strings"
	"testing"
)

func TestSMTPConfigValid(t *testing.T) {
	if (SMTPConfig{Host: "h", Port: 25, From: "f"}).Valid() != true {
		t.Fatal("expected valid")
	}
	if (SMTPConfig{Host: "", Port: 25, From: "f"}).Valid() {
		t.Fatal("host required")
	}
	if (SMTPConfig{Host: "h", Port: 0, From: "f"}).Valid() {
		t.Fatal("port required")
	}
	if (SMTPConfig{Host: "h", Port: 25, From: ""}).Valid() {
		t.Fatal("from required")
	}
}

func TestBuildPlainMessage(t *testing.T) {
	raw := buildPlainMessage("from@example.com", []string{"a@x.org", "b@y.org"}, "Hello", "Body line")
	s := string(raw)
	if !strings.Contains(s, "Subject: Hello") || !strings.Contains(s, "From: from@example.com") {
		t.Fatalf("missing headers: %q", s)
	}
	if !strings.Contains(s, "To: a@x.org") || !strings.Contains(s, "To: b@y.org") {
		t.Fatalf("missing recipients: %q", s)
	}
	if !strings.HasSuffix(strings.TrimSpace(s), "Body line") {
		t.Fatalf("missing body: %q", s)
	}
}
