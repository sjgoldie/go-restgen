// Command install-skill installs go-restgen AI coding agent rules into the
// current directory. Run this from your project root.
//
// Supported agents: claude (Claude Code), cursor (Cursor IDE).
//
// Usage:
//
//	cd /path/to/your/project
//	go run github.com/sjgoldie/go-restgen/cmd/install-skill@latest
//	go run github.com/sjgoldie/go-restgen/cmd/install-skill@latest --agent=cursor
//	go run github.com/sjgoldie/go-restgen/cmd/install-skill@latest --agent=all
package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed skill/SKILL.md skill/patterns.md
var claudeFiles embed.FS

//go:embed cursor/go-restgen.mdc
var cursorFiles embed.FS

func main() {
	agent := flag.String("agent", "claude", "target agent: claude, cursor, or all")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var installed []string
	switch *agent {
	case "claude":
		installed, err = installClaude(root)
	case "cursor":
		installed, err = installCursor(root)
	case "all":
		installed, err = installAll(root)
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown agent %q (use claude, cursor, or all)\n", *agent)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for _, path := range installed {
		fmt.Printf("  wrote %s\n", path)
	}
	fmt.Println("\ngo-restgen rules installed. Your AI agent will now auto-detect the framework in this project.")
}

func installClaude(root string) ([]string, error) {
	destDir := filepath.Join(root, ".claude", "skills", "go-restgen")
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating %s: %w", destDir, err)
	}

	var written []string
	for _, name := range []string{"SKILL.md", "patterns.md"} {
		data, err := claudeFiles.ReadFile("skill/" + name)
		if err != nil {
			return written, fmt.Errorf("reading embedded %s: %w", name, err)
		}
		dest := filepath.Join(destDir, name)
		if err := os.WriteFile(dest, data, 0o600); err != nil {
			return written, fmt.Errorf("writing %s: %w", dest, err)
		}
		written = append(written, dest)
	}
	return written, nil
}

func installCursor(root string) ([]string, error) {
	destDir := filepath.Join(root, ".cursor", "rules")
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating %s: %w", destDir, err)
	}

	data, err := cursorFiles.ReadFile("cursor/go-restgen.mdc")
	if err != nil {
		return nil, fmt.Errorf("reading embedded go-restgen.mdc: %w", err)
	}
	dest := filepath.Join(destDir, "go-restgen.mdc")
	if err := os.WriteFile(dest, data, 0o600); err != nil {
		return nil, fmt.Errorf("writing %s: %w", dest, err)
	}
	return []string{dest}, nil
}

func installAll(root string) ([]string, error) {
	claude, err := installClaude(root)
	if err != nil {
		return claude, err
	}
	cursor, err := installCursor(root)
	if err != nil {
		return append(claude, cursor...), err
	}
	return append(claude, cursor...), nil
}
