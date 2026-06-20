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
		if err := writeCgroupFile(filepath.Join(cellGroup, "memory.max"), fmt.Sprintf("%d", cell.RAMMB*1024*1024)); err != nil {
			return err
		}
	}

	if cell.CPUMilli > 0 {
		quota := cell.CPUMilli * 1000
		if err := writeCgroupFile(filepath.Join(cellGroup, "cpu.max"), fmt.Sprintf("%d 100000", quota)); err != nil {
			return err
		}
	}

	return writeCgroupFile(filepath.Join(cellGroup, "cgroup.procs"), fmt.Sprintf("%d", cell.PID))
}

func RemoveCellResources(cell Cell) {
	if cell.Name == "" {
		return
	}
	_ = os.Remove(filepath.Join("/sys/fs/cgroup", "sparkd", cell.Name))
}

func writeCgroupFile(path, value string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open cgroup file %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.WriteString(value); err != nil {
		return fmt.Errorf("write cgroup file %s: %w", path, err)
	}
	return nil
}
