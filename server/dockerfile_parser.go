package main

import (
	"strings"
)

// dockerfileInstr is one logical instruction in a Dockerfile after line
// continuations are folded. Line/EndLine are 1-based.
type dockerfileInstr struct {
	Kind     string         // upper-cased directive (FROM, RUN, …)
	Args     string         // raw argument string, single-spaced after fold
	Line     int            // first line where the instruction begins
	EndLine  int            // last line of the instruction (after \-continuations)
	Override *gateOverride  // pending override comment immediately above
}

// gateOverride is the structured form of a `# l0git: ignore <rules> [reason: …]`
// directive. Reused by both dockerfile_lint and compose_lint.
type gateOverride struct {
	RuleIDs []string
	Reason  string
	Line    int // 1-based line of the comment itself
}

func (o *gateOverride) matches(ruleID string) bool {
	if o == nil {
		return false
	}
	for _, id := range o.RuleIDs {
		if id == ruleID || id == "*" {
			return true
		}
	}
	return false
}

// parseGateOverride returns the structured override if commentLine is one,
// or nil otherwise. The grammar is:
//
//	# l0git: ignore <rule_id>[, <rule_id>…] [reason: free text…]
//
// We deliberately use string parsing (not regex) so the directive shape is
// auditable line-by-line; this is the override boundary between "the gate
// fired" and "the gate was deliberately silenced", and that audit must not
// hinge on a regex's edge cases.
func parseGateOverride(commentLine string) *gateOverride {
	line := strings.TrimSpace(commentLine)
	if !strings.HasPrefix(line, "#") {
		return nil
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "#"))
	if !strings.HasPrefix(rest, "l0git:") {
		return nil
	}
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "l0git:"))
	if !strings.HasPrefix(rest, "ignore") {
		return nil
	}
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "ignore"))
	var idsPart, reasonPart string
	if i := strings.Index(rest, "reason:"); i >= 0 {
		idsPart = strings.TrimSpace(rest[:i])
		reasonPart = strings.TrimSpace(rest[i+len("reason:"):])
	} else {
		idsPart = strings.TrimSpace(rest)
	}
	if idsPart == "" {
		return nil
	}
	out := &gateOverride{Reason: reasonPart}
	for _, p := range strings.Split(idsPart, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out.RuleIDs = append(out.RuleIDs, p)
		}
	}
	if len(out.RuleIDs) == 0 {
		return nil
	}
	return out
}

// parseDockerfile turns Dockerfile bytes into a flat list of instructions.
// Comments are dropped (except when they're override directives). Backslash
// line continuations are folded into a single Args string with whitespace
// preserved minimally.
//
// The parser is intentionally permissive: it does NOT validate the
// directive name, argument shape, or reject unknown instructions. Rule
// checks decide what a violation looks like; the parser only normalises
// what's there.
func parseDockerfile(content string) []dockerfileInstr {
	// Normalise CRLF to LF so line counts stay consistent regardless of
	// the editor that produced the file.
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	var instrs []dockerfileInstr
	var pending *gateOverride
	i := 0
	for i < len(lines) {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)
		lineNum := i + 1

		if trimmed == "" {
			// Blank lines clear a pending override — overrides must be
			// directly adjacent to their target instruction.
			pending = nil
			i++
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			if ov := parseGateOverride(trimmed); ov != nil {
				ov.Line = lineNum
				pending = ov
			}
			// Regular comments (including parser directives like
			// `# syntax=…`) are ignored without disturbing pending.
			i++
			continue
		}

		// Instruction line. Fold backslash continuations.
		startLine := lineNum
		acc := raw
		for endsWithContinuation(acc) {
			acc = stripContinuation(acc)
			i++
			if i >= len(lines) {
				break
			}
			acc += " " + strings.TrimSpace(lines[i])
		}
		// Update lineNum to the actual end-line of the (possibly folded)
		// instruction.
		endLine := i + 1

		acc = strings.TrimSpace(acc)
		directive, args := splitDirective(acc)
		if directive == "" {
			// Pathological line we couldn't classify; skip without
			// disturbing pending so the next instruction inherits any
			// override the user wrote.
			i++
			continue
		}

		instr := dockerfileInstr{
			Kind:     strings.ToUpper(directive),
			Args:     args,
			Line:     startLine,
			EndLine:  endLine,
			Override: pending,
		}
		instrs = append(instrs, instr)
		pending = nil
		i++
	}
	return instrs
}

func endsWithContinuation(line string) bool {
	trimmed := strings.TrimRight(line, " \t")
	return strings.HasSuffix(trimmed, "\\")
}

func stripContinuation(line string) string {
	trimmed := strings.TrimRight(line, " \t")
	return strings.TrimSuffix(trimmed, "\\")
}

// splitDirective extracts the first whitespace-delimited token (the
// directive) and returns the trimmed remainder as args. Returns ("", "")
// for unparseable input.
func splitDirective(line string) (string, string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", ""
	}
	idx := strings.IndexAny(line, " \t")
	if idx < 0 {
		return line, ""
	}
	return line[:idx], strings.TrimSpace(line[idx:])
}
