package main

import (
	"context"
	"math/big"
	"strings"
	"testing"
)

// mintToken builds a classic GitHub token with a CORRECT checksum, from an
// independent implementation (math/big, not the shift-and-mask loop in
// secrets.go). It is built at run time so no token-shaped literal sits in the
// source, and it was never issued by anyone.
func mintToken(prefix, entropy string) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	sum := big.NewInt(int64(crc32IEEE(entropy)))
	base := big.NewInt(62)
	digits := ""
	for sum.Sign() > 0 {
		var r big.Int
		sum.DivMod(sum, base, &r)
		digits = string(alphabet[r.Int64()]) + digits
	}
	for len(digits) < 6 {
		digits = "0" + digits
	}
	return prefix + entropy + digits
}

func crc32IEEE(s string) uint32 {
	var table [256]uint32
	for i := range table {
		c := uint32(i)
		for k := 0; k < 8; k++ {
			if c&1 == 1 {
				c = 0xEDB88320 ^ (c >> 1)
			} else {
				c >>= 1
			}
		}
		table[i] = c
	}
	crc := ^uint32(0)
	for i := 0; i < len(s); i++ {
		crc = table[byte(crc)^s[i]] ^ (crc >> 8)
	}
	return ^crc
}

const tokenEntropy = "Kq8Zm3Xv9Lp2Rt7Wy4Nc6Sd1Fh5Jg0" // 31 characters: trimmed below
var tokenBody = tokenEntropy[:30]

func githubSecretSeverities(t *testing.T, line string) []string {
	t.Helper()
	root := initRepoWithFiles(t, map[string]string{"README.md": line + "\n"})
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":github_pat_classic") {
			got = append(got, f.Severity)
		}
	}
	return got
}

// The suppression is only safe if a token that DOES verify is still an error.
func TestGitHubToken_ValidChecksumIsStillAnError(t *testing.T) {
	for _, p := range []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_"} {
		tok := mintToken(p, tokenBody)
		if len(tok) != 40 {
			t.Fatalf("test token has the wrong shape: %d", len(tok))
		}
		if !githubTokenChecksumValid(tok) {
			t.Errorf("%s: the independent implementation and the scanner disagree", p)
		}
		if got := githubSecretSeverities(t, "token: "+tok); len(got) != 1 || got[0] != SeverityError {
			t.Errorf("%s: a token with a valid checksum must stay an error, got %v", p, got)
		}
	}
}

// Every one-character change breaks the checksum: in the body, in the checksum,
// at either end. Those are examples, and are listed at info rather than dropped.
func TestGitHubToken_AnyMutationIsDowngradedNotDropped(t *testing.T) {
	good := mintToken("ghp_", tokenBody)
	for i := 4; i < len(good); i++ {
		b := []byte(good)
		if b[i] == 'A' {
			b[i] = 'B'
		} else {
			b[i] = 'A'
		}
		mut := string(b)
		if githubTokenChecksumValid(mut) {
			t.Errorf("mutation at %d still verifies: %s", i, mut[:6])
		}
		if got := githubSecretSeverities(t, mut); len(got) != 1 || got[0] != SeverityInfo {
			t.Errorf("mutation at %d: want one info finding, got %v", i, got)
		}
	}
}

// The line is judged as a whole: an example before a real token must not hide it,
// and the worst severity wins.
func TestGitHubToken_ExampleBeforeRealTokenDoesNotHideIt(t *testing.T) {
	good := mintToken("ghp_", tokenBody)
	fake := good[:len(good)-1] + map[bool]string{true: "A", false: "B"}[good[len(good)-1] != 'A']
	for _, line := range []string{
		"e.g. " + fake + " and then " + good,
		good + " (not " + fake + ")",
	} {
		if got := githubSecretSeverities(t, line); len(got) != 1 || got[0] != SeverityError {
			t.Errorf("want one error for %q, got %v", line[:20], got)
		}
	}
}

// Shapes the check does not know are never touched.
func TestGitHubToken_OtherShapesAreUntouched(t *testing.T) {
	for _, tok := range []string{"short", "ghp_" + strings.Repeat("a", 5), "xyz_" + tokenBody + "000000", "ghx_" + tokenBody + "000000", "xhp_" + tokenBody + "000000", "GHP_" + tokenBody + "000000", "ghp_" + tokenBody + "00000"} {
		if !githubTokenChecksumValid(tok) {
			t.Errorf("%q is not the classic shape and must be treated as possibly real", tok)
		}
	}
	fg := "github_pat_" + pseudoRandom(82)
	if got := firesSecretID(t, fg, "github_pat_fg"); !got {
		t.Errorf("a fine-grained token must still be reported as before")
	}
}

func firesSecretID(t *testing.T, line, id string) bool {
	t.Helper()
	root := initRepoWithFiles(t, map[string]string{"README.md": line + "\n"})
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":"+id) && f.Severity == SeverityError {
			return true
		}
	}
	return false
}

// pseudoRandom returns n varied alphanumeric characters (enough entropy to pass the
// scanner's own filters), deterministically.
func pseudoRandom(n int) string {
	const a = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	x := uint32(2463534242)
	b := make([]byte, n)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = a[x%uint32(len(a))]
	}
	return string(b)
}
