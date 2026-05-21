package dialect

import (
	"strings"
	"testing"
)

func TestQuoteIdent_Valid(t *testing.T) {
	q := CommonQuoter
	cases := []struct{ in, want string }{
		{"user", "`user`"},
		{"user_table", "`user_table`"},
		{"_private", "`_private`"},
		{"X123", "`X123`"},
	}
	for _, c := range cases {
		got, err := q.QuoteIdent(c.in)
		if err != nil {
			t.Errorf("QuoteIdent(%q) unexpected err: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("QuoteIdent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQuoteIdent_Invalid(t *testing.T) {
	q := CommonQuoter
	cases := []string{
		"",
		"1user",        // digit start
		"user table",   // space
		"user-table",   // dash
		"user.table",   // dot (use QuoteQualified instead)
		"user;DROP",    // injection
		"user'",        // quote
		"user`",        // backtick
	}
	for _, c := range cases {
		_, err := q.QuoteIdent(c)
		if err == nil {
			t.Errorf("QuoteIdent(%q) should fail, got nil err", c)
		}
	}
}

func TestQuoteQualified(t *testing.T) {
	q := CommonQuoter
	got, err := q.QuoteQualified("public", "user")
	if err != nil {
		t.Fatal(err)
	}
	if got != "`public`.`user`" {
		t.Fatalf("QuoteQualified: got %q, want `public`.`user`", got)
	}

	// empty schema → just QuoteIdent
	got, err = q.QuoteQualified("", "user")
	if err != nil {
		t.Fatal(err)
	}
	if got != "`user`" {
		t.Fatalf("empty schema: got %q", got)
	}

	// invalid schema or name returns err
	if _, err := q.QuoteQualified("schema;DROP", "user"); err == nil {
		t.Fatal("invalid schema should fail")
	}
	if _, err := q.QuoteQualified("public", "1bad"); err == nil {
		t.Fatal("invalid name should fail")
	}
}

func TestQuoteIdentMust_PanicsOnInvalid(t *testing.T) {
	q := CommonQuoter
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid identifier")
		}
	}()
	q.QuoteIdentMust("bad;input")
}

func TestQuoteIdentMust_OkOnValid(t *testing.T) {
	q := CommonQuoter
	got := q.QuoteIdentMust("user")
	if got != "`user`" {
		t.Fatalf("got %q", got)
	}
}

func TestQuoteIdent_ErrorMentionsInvalidIdentifier(t *testing.T) {
	q := CommonQuoter
	_, err := q.QuoteIdent("bad;input")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid identifier") {
		t.Errorf("err message should mention 'invalid identifier', got: %v", err)
	}
}
