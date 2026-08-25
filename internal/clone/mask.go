// Package clone copies data from one dbtools target to another of the
// same engine, masking sensitive columns by default. See
// docs/superpowers/plans/2026-08-25-clone-prod-to-dev.md for the design.
package clone

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// MaskStrategy names how one column's values are transformed during clone.
type MaskStrategy string

const (
	// MaskRedact replaces string/[]byte values with a fixed placeholder.
	// Non-string values pass through unchanged (there is no safe generic
	// redaction for a number without knowing its semantics).
	MaskRedact MaskStrategy = "redact"
	// MaskEmail replaces a value with a deterministically unique synthetic
	// address (userN@example.invalid), preserving uniqueness for any
	// unique constraint on the column.
	MaskEmail MaskStrategy = "email"
	// MaskHash replaces a value with a 12-hex-char SHA-256 prefix of its
	// string representation — deterministic (the same input always maps
	// to the same output, useful when the value is referenced elsewhere)
	// but non-reversible.
	MaskHash MaskStrategy = "hash"
)

// builtinSensitiveColumns is the default-deny list: these column names
// (case-insensitive, exact match) are masked even with no [clone.mask]
// entry, unless --no-mask is passed. This is deliberately small and
// literal — dbtools has no way to classify arbitrary schemas, so it only
// protects the columns experience says show up by these exact names.
var builtinSensitiveColumns = map[string]MaskStrategy{
	"email":    MaskEmail,
	"phone":    MaskRedact,
	"ssn":      MaskRedact,
	"password": MaskRedact,
}

// maskPlanFor builds the column -> strategy plan for one table's columns.
// An explicit [clone.mask] entry always wins over a built-in default;
// configured strategy names are used verbatim (an unrecognized strategy
// name is handled by applyMask, which passes the value through unchanged
// for any strategy it doesn't recognize).
func maskPlanFor(colNames []string, configured map[string]string) map[string]MaskStrategy {
	normConfig := make(map[string]string, len(configured))
	for k, v := range configured {
		normConfig[strings.ToLower(k)] = v
	}
	plan := make(map[string]MaskStrategy)
	for _, name := range colNames {
		lower := strings.ToLower(name)
		if strat, ok := normConfig[lower]; ok {
			plan[name] = MaskStrategy(strat)
			continue
		}
		if strat, ok := builtinSensitiveColumns[lower]; ok {
			plan[name] = strat
		}
	}
	return plan
}

// applyMask transforms value according to strategy. A nil value (a real
// SQL NULL) always passes through unchanged — masking never invents data
// for a column that was genuinely absent. counters holds per-column
// running state (used by MaskEmail to number synthetic addresses); it is
// shared across every call for one table's clone and must be created
// fresh per table.
func applyMask(strategy MaskStrategy, value any, counters map[string]int, colName string) any {
	if value == nil {
		return nil
	}
	switch strategy {
	case MaskRedact:
		switch value.(type) {
		case string:
			return "[REDACTED]"
		case []byte:
			return []byte("[REDACTED]")
		default:
			return value
		}
	case MaskEmail:
		counters[colName]++
		return fmt.Sprintf("user%d@example.invalid", counters[colName])
	case MaskHash:
		s := fmt.Sprintf("%v", value)
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])[:12]
	default:
		return value
	}
}
