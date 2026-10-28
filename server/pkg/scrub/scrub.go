// Package scrub removes personal and song data from error text before it is sent
// to crash reporting (D-35, §4.4): song, artist, playlist and group names (which
// error messages quote), file paths, emails, URLs and tokens. The TypeScript twin
// is packages/core/src/crash/scrub.ts; both run the cases in
// packages/core/src/crash/scrub-cases.json.
package scrub

import "regexp"

var rules = []struct {
	re   *regexp.Regexp
	with string
}{
	// URLs first: they contain paths and sometimes emails.
	{regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'<>]+`), "[url]"},
	{regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`), "[email]"},
	// Quoted names: "…", '…', “…”, ‘…’.
	{regexp.MustCompile(`"[^"\n]*"|'[^'\n]*'|“[^”\n]*”|‘[^’\n]*’`), "[text]"},
	// Paths, which may contain spaces: Windows drive paths, and Unix paths of two
	// or more parts or under ~. They run up to a colon, a quote or the end.
	{regexp.MustCompile(`[A-Za-z]:\\[^:"'\n]*[^\s:"'\n]`), "[path]"},
	{regexp.MustCompile(`(?:~|/[^\s/:"']+)(?:/[^/:"'\n]*[^\s/:"'\n])+`), "[path]"},
	// Tokens and hashes: long base64url or hex runs (IDs with dashes are kept).
	{regexp.MustCompile(`\b[A-Za-z0-9_]{24,}\b`), "[token]"},
}

// Text scrubs one message.
func Text(s string) string {
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.with)
	}
	return s
}
