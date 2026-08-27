package dump_test

import (
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/dump"
)

// Postgres 16.10+/17.6+/18 emit \restrict and \unrestrict from pg_dump by
// default. They are psql meta-commands, so a baseline carrying them fails to
// apply over the wire protocol with `syntax error at or near "\"`.
func TestStripPsqlMetaCommands_RemovesRestrict(t *testing.T) {
	in := strings.Join([]string{
		"--",
		"-- PostgreSQL database dump",
		"--",
		"",
		`\restrict 1uYNqOvUnmIaYkfzMTbiPC77KU3sYnhJ3vg2BJ56ELY4LUh43l`,
		"",
		"CREATE TABLE public.b (total_jobs integer);",
		`\unrestrict 1uYNqOvUnmIaYkfzMTbiPC77KU3sYnhJ3vg2BJ56ELY4LUh43l`,
	}, "\n")

	got := dump.StripPsqlMetaCommands(in)

	if strings.Contains(got, `\restrict`) || strings.Contains(got, `\unrestrict`) {
		t.Errorf("meta-commands survived:\n%s", got)
	}
	if !strings.Contains(got, "CREATE TABLE public.b") {
		t.Errorf("real SQL was dropped:\n%s", got)
	}
}

// A backslash-led line inside a dollar-quoted function body is data, not a
// meta-command. Dropping it would silently corrupt the routine.
func TestStripPsqlMetaCommands_PreservesDollarQuotedBody(t *testing.T) {
	in := strings.Join([]string{
		`\restrict abc`,
		"CREATE FUNCTION f() RETURNS text AS $$",
		`\this is not a meta-command`,
		"SELECT 1;",
		"$$ LANGUAGE sql;",
		`\unrestrict abc`,
	}, "\n")

	got := dump.StripPsqlMetaCommands(in)

	if !strings.Contains(got, `\this is not a meta-command`) {
		t.Errorf("dollar-quoted body line was stripped:\n%s", got)
	}
	if strings.Contains(got, `\restrict`) || strings.Contains(got, `\unrestrict`) {
		t.Errorf("meta-commands survived:\n%s", got)
	}
}

// A tagged body ($func$) is only closed by its own tag, so a bare $$ inside
// it must not be treated as the end of the body.
func TestStripPsqlMetaCommands_TaggedDollarQuote(t *testing.T) {
	in := strings.Join([]string{
		"CREATE FUNCTION f() RETURNS text AS $func$",
		"-- a bare $$ inside the body",
		`\still inside`,
		"$func$ LANGUAGE sql;",
		`\restrict after`,
	}, "\n")

	got := dump.StripPsqlMetaCommands(in)

	if !strings.Contains(got, `\still inside`) {
		t.Errorf("line inside $func$ body was stripped:\n%s", got)
	}
	if strings.Contains(got, `\restrict`) {
		t.Errorf("meta-command after the body survived:\n%s", got)
	}
}

func TestStripPostgresSessionState_AlsoStripsMetaCommands(t *testing.T) {
	in := "SELECT pg_catalog.set_config('search_path', '', false);\n" +
		"\\restrict tok\n" +
		"CREATE TABLE t (id int);\n"

	got := dump.StripPostgresSessionState(in)

	for _, unwanted := range []string{"set_config", `\restrict`} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%q survived:\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "CREATE TABLE t") {
		t.Errorf("real SQL was dropped:\n%s", got)
	}
}

// transaction_timeout is a Postgres 17 GUC. A dump taken with pg_dump 17+
// against an older server carries it, and applying that to a 16 server
// fails with `unrecognized configuration parameter "transaction_timeout"`.
func TestStripPostgresSessionState_StripsTimeoutGUCs(t *testing.T) {
	in := strings.Join([]string{
		"SET statement_timeout = 0;",
		"SET lock_timeout = 0;",
		"SET idle_in_transaction_session_timeout = 0;",
		"SET transaction_timeout = 0;",
		"SET client_encoding = 'UTF8';",
		"SET check_function_bodies = false;",
		"CREATE TABLE t (id int);",
	}, "\n")

	got := dump.StripPostgresSessionState(in)

	for _, unwanted := range []string{"statement_timeout", "lock_timeout", "transaction_timeout"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%q survived:\n%s", unwanted, got)
		}
	}
	// These change restore behaviour and must survive — dropping
	// check_function_bodies breaks functions that reference objects
	// created later in the same dump.
	for _, wanted := range []string{"check_function_bodies", "client_encoding", "CREATE TABLE t"} {
		if !strings.Contains(got, wanted) {
			t.Errorf("%q was dropped:\n%s", wanted, got)
		}
	}
}
