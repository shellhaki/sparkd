package internals

import (
	"os"
	"path/filepath"
)

type Config struct {
	StateDir   string
	BaseRootFS string
	CellsDir   string
	StorePath  string
	ListenAddr string

	BridgeName string
	BridgeCIDR string
	BridgeIP   string
}

func LoadConfig() Config {
	stateDir := getenv("SPARKD_STATE_DIR", "/var/lib/sparkd")
	listenAddr := getenv("SPARKD_ADDR", "0.0.0.0:8721")

	return Config{
		StateDir:   stateDir,
		BaseRootFS: filepath.Join(stateDir, "base", "rootfs"),
		CellsDir:   filepath.Join(stateDir, "cells"),
		StorePath:  filepath.Join(stateDir, "cells.json"),
		ListenAddr: listenAddr,

		BridgeName: "sparkd0",
		BridgeCIDR: "10.42.0.1/24",
		BridgeIP:   "10.42.0.1",
	}
}

func (c Config) EnsureDirs() error {
	if err := os.MkdirAll(c.StateDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.BaseRootFS), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(c.CellsDir, 0755); err != nil {
		return err
	}
	return nil
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
