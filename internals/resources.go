package internals

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func ApplyCellResources(cell Cell) error {
	cgroupRoot := filepath.Join("/sys/fs/cgroup", "sparkd")
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cgroup v2 is required for resource enforcement")
		}
		return err
	}

	if err := os.MkdirAll(cgroupRoot, 0755); err != nil {
		return err
	}
	cellGroup := filepath.Join(cgroupRoot, cell.Name)
	if err := os.MkdirAll(cellGroup, 0755); err != nil {
		return err
	}

	if cell.RAMMB > 0 {
		if err := os.WriteFile(filepath.Join(cellGroup, "memory.max"), []byte(fmt.Sprintf("%d", cell.RAMMB*1024*1024)), 0644); err != nil {
			return err
		}
	}

	if cell.CPUMilli > 0 {
		quota := cell.CPUMilli * 1000
		if err := os.WriteFile(filepath.Join(cellGroup, "cpu.max"), []byte(fmt.Sprintf("%d 100000", quota)), 0644); err != nil {
			return err
		}
	}

	return os.WriteFile(filepath.Join(cellGroup, "cgroup.procs"), []byte(fmt.Sprintf("%d", cell.PID)), 0644)
}

func RemoveCellResources(cell Cell) {
	if cell.Name == "" {
		return
	}
	_ = os.Remove(filepath.Join("/sys/fs/cgroup", "sparkd", cell.Name))
}
