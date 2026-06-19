package main

import (
	"encoding/json"
	"fmt"
	"os"

	"shellhaki/sparkd/internals"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "cell-child" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "sparkd cell-child requires a rootfs path")
			os.Exit(2)
		}
		if err := internals.RunCellChild(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	cfg := internals.LoadConfig()

	command := "daemon"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	switch command {
	case "daemon":
		daemon, err := internals.NewDaemon(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := daemon.ListenAndServe(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "import":
		progress := internals.NewProgressLog()
		if err := internals.EnsureBaseRootFS(cfg, progress); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := internals.ImportDatabase(cfg, "pg", progress); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"status":   "ok",
			"progress": progress.Events(),
		})
	default:
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  sudo ./main daemon")
		fmt.Fprintln(os.Stderr, "  sudo ./main import")
		os.Exit(2)
	}
}
