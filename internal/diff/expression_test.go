package diff

import "testing"

// Postgres renders a stored CHECK expression differently across major
// versions: 16 gives "((total_jobs >= 0))" where 17 gives
// "(total_jobs >= 0)" for the identical constraint. Comparing the two
// raw produced a finding for every CHECK in the schema.
func TestEqualExpressions_IgnoresRenderingDifferences(t *testing.T) {
	equal := [][2]string{
		{"(total_jobs >= 0)", "((total_jobs >= 0))"},
		{"((completed_jobs + failed_jobs) <= total_jobs)", "(((completed_jobs + failed_jobs) <= total_jobs))"},
		{"(status = ANY (ARRAY['a'::text, 'b'::text]))", "((status = ANY (ARRAY['a'::text, 'b'::text])))"},
		{"(a >= 0)", "( a  >=  0 )"},
		{"(x > 1)", "(x > 1)"},
	}
	for _, pair := range equal {
		if !equalExpressions(pair[0], pair[1]) {
			t.Errorf("equalExpressions(%q, %q) = false, want true", pair[0], pair[1])
		}
	}
}

// Normalisation must only absorb formatting. A real change to an operator,
// a literal, or an identifier still has to be reported.
func TestEqualExpressions_KeepsRealDifferences(t *testing.T) {
	different := [][2]string{
		{"(total_jobs >= 0)", "(total_jobs > 0)"},
		{"(total_jobs >= 0)", "(total_jobs >= 1)"},
		{"(total_jobs >= 0)", "(failed_jobs >= 0)"},
		{"(status = ANY (ARRAY['a'::text]))", "(status = ANY (ARRAY['b'::text]))"},
	}
	for _, pair := range different {
		if equalExpressions(pair[0], pair[1]) {
			t.Errorf("equalExpressions(%q, %q) = true, want false", pair[0], pair[1])
		}
	}
}

// "(a) AND (b)" opens and closes a paren before the end, so the leading and
// trailing ones are not a wrapper — stripping them would corrupt the
// expression and could make two different constraints compare equal.
func TestNormalizeExpression_DoesNotStripNonWrappingParens(t *testing.T) {
	cases := map[string]string{
		"((a) AND (b))":  "(a) AND (b)",
		"(a) AND (b)":    "(a) AND (b)",
		"((a))":          "a",
		"(name ~ '(x)')": "name ~ '(x)'",
	}
	for in, want := range cases {
		if got := normalizeExpression(in); got != want {
			t.Errorf("normalizeExpression(%q) = %q, want %q", in, got, want)
		}
	}
}

// A parenthesis inside a string literal is data. Counting it would
// mis-balance the scan and could strip a genuine wrapper or refuse a
// legitimate one.
func TestOuterParensBalanced_IgnoresStringLiterals(t *testing.T) {
	cases := map[string]bool{
		"(name ~ '(unclosed')":    true,
		"(a = ')' AND b = '(')":   true,
		"(a) AND (b)":             false,
		"(a >= 0)":                true,
		"(x = 'it''s' AND y = 1)": true,
	}
	for in, want := range cases {
		if got := outerParensBalanced(in); got != want {
			t.Errorf("outerParensBalanced(%q) = %v, want %v", in, got, want)
		}
	}
}
