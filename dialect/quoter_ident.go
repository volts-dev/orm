package dialect

import (
	"fmt"
	"regexp"
)

// identRegex matches a valid SQL identifier: [a-zA-Z_][a-zA-Z0-9_]*.
// Length limits (PG ≤ 63, MySQL ≤ 64) are dialect-specific and not enforced here.
var identRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// QuoteIdent quotes an SQL identifier with strict whitelist validation.
//
// Differs from Quote(): Quote() concatenates the input without validation
// (kept for backward compat); QuoteIdent enforces the regex and is the
// recommended entry point after Phase 2.
//
// Identifiers must match [a-zA-Z_][a-zA-Z0-9_]*. For schema-qualified names
// (e.g. "public.user"), use QuoteQualified.
func (q Quoter) QuoteIdent(ident string) (string, error) {
	if !identRegex.MatchString(ident) {
		return "", fmt.Errorf("invalid identifier %q: must match [a-zA-Z_][a-zA-Z0-9_]*", ident)
	}
	return string(q.Prefix) + ident + string(q.Suffix), nil
}

// QuoteIdentMust is the panic-version of QuoteIdent. Use only for identifiers
// that have already been validated upstream (e.g. field names retrieved from
// TModelObject after SyncModel).
func (q Quoter) QuoteIdentMust(ident string) string {
	out, err := q.QuoteIdent(ident)
	if err != nil {
		panic(err)
	}
	return out
}

// QuoteQualified quotes a schema-qualified identifier. Empty schema returns
// the same as QuoteIdent(name). Returns "`schema`.`name`" with each piece
// independently validated.
func (q Quoter) QuoteQualified(schema, name string) (string, error) {
	if schema == "" {
		return q.QuoteIdent(name)
	}
	s, err := q.QuoteIdent(schema)
	if err != nil {
		return "", err
	}
	n, err := q.QuoteIdent(name)
	if err != nil {
		return "", err
	}
	return s + "." + n, nil
}
