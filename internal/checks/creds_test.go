package checks

import "testing"

func TestRejectSecretParams(t *testing.T) {
	if err := RejectSecretParams(map[string]any{"host": "db", "credentialId": "x"}); err != nil {
		t.Fatal(err)
	}
	if err := RejectSecretParams(map[string]any{"password": "secret"}); err == nil {
		t.Fatal("expected error for password")
	}
}

func TestValidateReadOnlySQL(t *testing.T) {
	if err := validateReadOnlySQL("SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if err := validateReadOnlySQL("WITH x AS (SELECT 1) SELECT * FROM x"); err != nil {
		t.Fatal(err)
	}
	if err := validateReadOnlySQL("DELETE FROM t"); err == nil {
		t.Fatal("expected reject")
	}
	if err := validateReadOnlySQL("SELECT 1; DROP TABLE t"); err == nil {
		t.Fatal("expected reject multi")
	}
}

func TestIsCentralOnly(t *testing.T) {
	if !IsCentralOnly("sql_query") || IsCentralOnly("icmp") {
		t.Fatal("central-only mapping wrong")
	}
}
