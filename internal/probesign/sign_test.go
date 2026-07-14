package probesign

import "testing"

func TestSignVerifyRoundTrip(t *testing.T) {
	priv, err := PrivateKeyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	h := HashSHA256Hex([]byte("hello-probe"))
	sig := SignHash(priv, h)
	if err := Verify(h, sig); err != nil {
		t.Fatal(err)
	}
	if err := Verify(h, "AAAA"); err == nil {
		t.Fatal("expected verify failure")
	}
}

func TestCompareCalVer(t *testing.T) {
	cases := []struct {
		local, remote string
		want          bool
	}{
		{"2026.7.13.5", "2026.7.13.6", true},
		{"2026.7.13.5", "2026.7.13.5", false},
		{"2026.7.13.5", "2026.7.12.9", false},
		{"dev", "2026.7.13.5", true},
		{"2026.7.13.5", "dev", false},
	}
	for _, c := range cases {
		if got := CompareCalVer(c.local, c.remote); got != c.want {
			t.Errorf("CompareCalVer(%q,%q)=%v want %v", c.local, c.remote, got, c.want)
		}
	}
}
