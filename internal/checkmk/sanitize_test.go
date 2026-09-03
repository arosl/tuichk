package checkmk

import "testing"

func TestClean(t *testing.T) {
	cases := map[string]string{
		"plain text":               "plain text",
		"a\x1b]52;c;aGVsbG8=\x07b": "a]52;c;aGVsbG8=b",
		"x\x1b[2Jy":                "x[2Jy",
		"tab\tsep":                 "tab sep",
		"keep\nnewline":            "keep\nnewline",
		"del\x7fchar":              "delchar",
		"c1 31m csi":              "c1 31m csi",
		"unicode ✓ ▸ ok":           "unicode ✓ ▸ ok",
		"cr\r\nlf":                 "cr\nlf",
	}
	for in, want := range cases {
		if got := clean(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
}
