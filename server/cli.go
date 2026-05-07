package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func runCLI(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: lgit <check|list|stats|gates|ignore|resolve|delete|clear|path|version> [args...]")
	}

	cmd, rest := args[0], args[1:]

	switch cmd {
	case "version", "--version", "-v":
		fmt.Println(Version)
		return nil
	case "path":
		p, err := defaultDBPath()
		if err != nil {
			return err
		}
		fmt.Println(p)
		return nil
	case "gates":
		return writeJSON(os.Stdout, gateRegistryMarshallable())
	}

	store, err := OpenStore()
	if err != nil {
		return err
	}
	defer store.Close()
	ctx := context.Background()

	switch cmd {
	case "check":
		// lgit check <project_root> [gate_id]
		if len(rest) < 1 {
			return fmt.Errorf("usage: lgit check <project_root> [gate_id]")
		}
		gateID := ""
		if len(rest) > 1 {
			gateID = rest[1]
		}
		res, err := RunChecks(ctx, store, rest[0], gateID)
		if err != nil {
			return err
		}
		return writeJSON(os.Stdout, res)
	case "list":
		// lgit list [-project=…] [-status=…] [-severity=…] [-gate=…]
		//          [-tag=…] [-query=…] [-sort=…] [-limit=N] [-offset=N]
		filter, err := parseListFlags(rest)
		if err != nil {
			return err
		}
		fs, err := store.List(ctx, filter)
		if err != nil {
			return err
		}
		return writeJSON(os.Stdout, fs)
	case "ignore":
		if len(rest) < 1 {
			return fmt.Errorf("usage: lgit ignore <id>")
		}
		id, err := strconv.ParseInt(rest[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id: %w", err)
		}
		ok, err := store.Ignore(ctx, id)
		if err != nil {
			return err
		}
		return writeJSON(os.Stdout, map[string]any{"ignored": ok})
	case "delete":
		if len(rest) < 1 {
			return fmt.Errorf("usage: lgit delete <id>")
		}
		id, err := strconv.ParseInt(rest[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid id: %w", err)
		}
		ok, err := store.Delete(ctx, id)
		if err != nil {
			return err
		}
		return writeJSON(os.Stdout, map[string]any{"deleted": ok})
	case "clear":
		// lgit clear <project>
		if len(rest) < 1 {
			return fmt.Errorf("usage: lgit clear <project>")
		}
		n, err := store.ClearProject(ctx, rest[0])
		if err != nil {
			return err
		}
		return writeJSON(os.Stdout, map[string]any{"deleted": n})
	case "stats":
		// lgit stats [-project=…]
		project := ""
		for _, a := range rest {
			if k, v, _ := splitFlag(a); k == "project" {
				project = v
			}
		}
		s, err := store.Stats(ctx, project)
		if err != nil {
			return err
		}
		return writeJSON(os.Stdout, s)
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// parseListFlags converts `-key=value` / `-key value` arguments into a
// FindingFilter. Stays close to the stdlib `flag` semantics so users (and
// the extension) can pass options in any order. Status defaults to "open"
// for ergonomics — `lgit list` on its own shows current open findings.
func parseListFlags(args []string) (FindingFilter, error) {
	f := FindingFilter{Status: "open"}
	i := 0
	for i < len(args) {
		token := args[i]
		key, val, hasInline := splitFlag(token)
		if key == "" {
			return f, fmt.Errorf("unexpected positional argument: %q (use -key=value)", token)
		}
		if !hasInline {
			if i+1 >= len(args) {
				return f, fmt.Errorf("flag -%s requires a value", key)
			}
			val = args[i+1]
			i++
		}
		switch key {
		case "project":
			f.Project = val
		case "status":
			if val == "all" {
				f.Status = ""
			} else {
				f.Status = val
			}
		case "severity":
			f.Severity = val
		case "gate":
			f.GateID = val
		case "tag":
			f.Tag = val
		case "query":
			f.Query = val
		case "sort":
			f.Sort = val
		case "limit":
			n, err := strconv.Atoi(val)
			if err != nil {
				return f, fmt.Errorf("invalid -limit %q: %w", val, err)
			}
			f.Limit = n
		case "offset":
			n, err := strconv.Atoi(val)
			if err != nil {
				return f, fmt.Errorf("invalid -offset %q: %w", val, err)
			}
			f.Offset = n
		default:
			return f, fmt.Errorf("unknown flag -%s", key)
		}
		i++
	}
	return f, nil
}

// splitFlag accepts "-key=value", "--key=value", "-key", "--key" forms and
// returns (key, value, valueWasInline). Empty key signals "not a flag".
func splitFlag(token string) (string, string, bool) {
	if !strings.HasPrefix(token, "-") {
		return "", "", false
	}
	stripped := strings.TrimLeft(token, "-")
	if stripped == "" {
		return "", "", false
	}
	if eq := strings.IndexByte(stripped, '='); eq >= 0 {
		return stripped[:eq], stripped[eq+1:], true
	}
	return stripped, "", false
}
