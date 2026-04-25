package auth

import "testing"

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := VerifyPassword("correct-horse-battery-staple", hash); err != nil {
		t.Fatalf("verify good: %v", err)
	}
	if err := VerifyPassword("wrong", hash); err != ErrPasswordMismatch {
		t.Fatalf("verify bad: want ErrPasswordMismatch, got %v", err)
	}
}

func TestPasswordInvalidEncoded(t *testing.T) {
	if err := VerifyPassword("x", "not-a-phc-string"); err != ErrInvalidHash {
		t.Fatalf("want ErrInvalidHash, got %v", err)
	}
}

func TestRoleAtLeast(t *testing.T) {
	cases := []struct {
		have, need Role
		want       bool
	}{
		{RoleSuperAdmin, RoleReadOnly, true},
		{RoleSiteAdmin, RoleSuperAdmin, false},
		{RoleTech, RoleTech, true},
		{RoleReadOnly, RoleSiteAdmin, false},
	}
	for _, c := range cases {
		if got := c.have.AtLeast(c.need); got != c.want {
			t.Errorf("%s.AtLeast(%s) = %v, want %v", c.have, c.need, got, c.want)
		}
	}
}
