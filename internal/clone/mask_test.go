package clone

import "testing"

func TestMaskPlanFor_BuiltinSensitiveColumns(t *testing.T) {
	plan := maskPlanFor([]string{"id", "email", "phone", "ssn", "password", "notes"}, nil)
	want := map[string]MaskStrategy{
		"email":    MaskEmail,
		"phone":    MaskRedact,
		"ssn":      MaskRedact,
		"password": MaskRedact,
	}
	if len(plan) != len(want) {
		t.Fatalf("maskPlanFor() = %v, want %v", plan, want)
	}
	for col, strat := range want {
		if plan[col] != strat {
			t.Errorf("plan[%q] = %q, want %q", col, plan[col], strat)
		}
	}
}

func TestMaskPlanFor_ConfiguredOverridesBuiltin(t *testing.T) {
	plan := maskPlanFor([]string{"email"}, map[string]string{"email": "hash"})
	if plan["email"] != MaskHash {
		t.Errorf("plan[email] = %q, want hash (config must win over the email built-in)", plan["email"])
	}
}

func TestMaskPlanFor_ConfiguredAddsNonBuiltinColumn(t *testing.T) {
	plan := maskPlanFor([]string{"customerName"}, map[string]string{"customerName": "redact"})
	if plan["customerName"] != MaskRedact {
		t.Errorf("plan[customerName] = %q, want redact", plan["customerName"])
	}
}

func TestMaskPlanFor_CaseInsensitiveConfigKey(t *testing.T) {
	plan := maskPlanFor([]string{"Email"}, map[string]string{"email": "hash"})
	if plan["Email"] != MaskHash {
		t.Errorf("plan[Email] = %q, want hash (config keys match case-insensitively)", plan["Email"])
	}
}

func TestApplyMask_NilPassesThrough(t *testing.T) {
	if got := applyMask(MaskRedact, nil, map[string]int{}, "email"); got != nil {
		t.Errorf("applyMask(nil) = %v, want nil (never invent data for a real NULL)", got)
	}
}

func TestApplyMask_RedactString(t *testing.T) {
	if got := applyMask(MaskRedact, "real@example.com", map[string]int{}, "email"); got != "[REDACTED]" {
		t.Errorf("applyMask(redact, string) = %v, want [REDACTED]", got)
	}
}

func TestApplyMask_RedactBytes(t *testing.T) {
	got := applyMask(MaskRedact, []byte("secret"), map[string]int{}, "notes")
	b, ok := got.([]byte)
	if !ok || string(b) != "[REDACTED]" {
		t.Errorf("applyMask(redact, []byte) = %v, want []byte(\"[REDACTED]\")", got)
	}
}

func TestApplyMask_RedactNonStringPassesThrough(t *testing.T) {
	// Redacting a number has no safe generic representation — a
	// misconfigured mask on e.g. a credit_limit column must not corrupt
	// the row's type (which would break the INSERT), so it passes through.
	if got := applyMask(MaskRedact, int64(42), map[string]int{}, "credit_limit"); got != int64(42) {
		t.Errorf("applyMask(redact, int64) = %v, want unchanged 42", got)
	}
}

func TestApplyMask_EmailIsDeterministicallyUniquePerRow(t *testing.T) {
	counters := map[string]int{}
	first := applyMask(MaskEmail, "a@example.com", counters, "email")
	second := applyMask(MaskEmail, "b@example.com", counters, "email")
	if first == second {
		t.Fatalf("applyMask(email) produced the same value twice: %v", first)
	}
	if first != "user1@example.invalid" || second != "user2@example.invalid" {
		t.Errorf("applyMask(email) = %v, %v; want user1@example.invalid, user2@example.invalid", first, second)
	}
}

func TestApplyMask_HashIsDeterministicAndTwelveChars(t *testing.T) {
	counters := map[string]int{}
	got1 := applyMask(MaskHash, "same-value", counters, "external_id")
	got2 := applyMask(MaskHash, "same-value", counters, "external_id")
	if got1 != got2 {
		t.Errorf("applyMask(hash) = %v then %v, want identical (deterministic)", got1, got2)
	}
	s, ok := got1.(string)
	if !ok || len(s) != 12 {
		t.Errorf("applyMask(hash) = %v, want a 12-char hex string", got1)
	}
}
