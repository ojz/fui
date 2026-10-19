// fui: a local HTTP server that renders agent-generated HTML in your browser
// and syncs page state back through a sharedLocalStorage.js contract. The
// agent drives it with plain curl; the contract is served at /SKILL.md.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

const version = "1.0.0"

func defaultDBPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "fui", "fui.db")
}

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && args[0][0] != '-' {
		cmd = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	port := fs.Int("port", envInt("FUI_PORT", 6219), "port to listen on (127.0.0.1 only)")
	dbPath := fs.String("db", envStr("FUI_DB", defaultDBPath()), "path to the SQLite database")
	hidden := fs.Bool("hidden", false, "detach from the console (used by autostart)")
	fs.Parse(args)

	switch cmd {
	case "serve":
		if *hidden {
			hideConsole()
		}
		if err := serve(*port, *dbPath); err != nil {
			fmt.Fprintln(os.Stderr, "fui:", err)
			os.Exit(1)
		}
	case "autostart":
		if err := autostartCmd(fs.Args(), *port); err != nil {
			fmt.Fprintln(os.Stderr, "fui:", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println("fui", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Print(`fui - render agent-generated HTML locally, with persistent shared state

Usage:
  fui [serve] [--port N] [--db PATH]   start the server (default command)
  fui autostart install|uninstall|status
  fui version

Environment:
  FUI_PORT   port to listen on (default 6219)
  FUI_DB     SQLite database path
`)
}

func serve(port int, dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer store.Close()

	hub := NewHub()
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	mux := NewWebHandler(store, hub, baseURL)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	fmt.Printf("fui %s listening on %s (db: %s)\n", version, baseURL, dbPath)
	fmt.Printf("  overview: %s/\n  skill:    %s/SKILL.md\n", baseURL, baseURL)
	return http.ListenAndServe(addr, hostCheck(mux))
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}
