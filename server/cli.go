package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
)

func runCLI(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: lgit <check|list|gates|ignore|resolve|delete|clear|path|version> [args...]")
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
		return writeJSON(os.Stdout, gateRegistry())
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
		// lgit list [project] [status] [limit]
		project := ""
		status := "open"
		limit := 200
		if len(rest) > 0 {
			project = rest[0]
		}
		if len(rest) > 1 {
			status = rest[1]
			if status == "all" {
				status = ""
			}
		}
		if len(rest) > 2 {
			limit, _ = strconv.Atoi(rest[2])
		}
		fs, err := store.List(ctx, project, status, limit)
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
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
