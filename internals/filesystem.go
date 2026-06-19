package internals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func EnsureBaseRootFS(cfg Config, progress *ProgressLog) error {
	if _, err := os.Stat(filepath.Join(cfg.BaseRootFS, "bin", "sh")); err == nil {
		progress.Add("base-rootfs", "skipped", "Alpine base rootfs already exists")
		return nil
	}

	progress.Add("base-rootfs", "running", "importing Alpine minirootfs")
	if err := os.MkdirAll(cfg.BaseRootFS, 0755); err != nil {
		return err
	}

	cmd := exec.Command("bash", "-c",
		"curl -L https://dl-2.alpinelinux.org/alpine/latest-stable/releases/x86_64/alpine-minirootfs-3.24.1-x86_64.tar.gz | tar -xz -C "+cfg.BaseRootFS,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	progress.Add("base-rootfs", "done", "Alpine base rootfs imported")
	return nil
}

func ImportDatabase(cfg Config, dbType string, progress *ProgressLog) error {
	dbType = normalizeDBType(dbType)
	if dbType != DBTypePostgres {
		return fmt.Errorf("unsupported database type %q", dbType)
	}

	markerPath := filepath.Join(cfg.StateDir, "imports", "postgres.json")
	if _, err := os.Stat(markerPath); err == nil {
		progress.Add("postgres-import", "skipped", "PostgreSQL already imported into the base rootfs")
		return nil
	}

	progress.Add("postgres-import", "running", "installing PostgreSQL into the base rootfs")
	if err := writeBaseResolver(cfg.BaseRootFS); err != nil {
		return err
	}
	if err := runLoggedCommand("chroot", cfg.BaseRootFS, "apk", "add", "--no-cache", "postgresql"); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		return err
	}

	marker := map[string]any{
		"dbtype":      DBTypePostgres,
		"imported_at": time.Now().UTC(),
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(markerPath, data, 0644); err != nil {
		return err
	}

	progress.Add("postgres-import", "done", "PostgreSQL imported once and ready for future cells")
	return nil
}

func CreateCellRootFS(cfg Config, name string, diskMB int, progress *ProgressLog) (string, error) {
	cellDir := filepath.Join(cfg.CellsDir, name)
	rootfs := filepath.Join(cellDir, "rootfs")
	imagePath := filepath.Join(cellDir, "rootfs.ext4")

	progress.Add("cell-disk", "running", fmt.Sprintf("creating %dMB quota-backed rootfs image", diskMB))
	if err := os.RemoveAll(cellDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(rootfs, 0755); err != nil {
		return "", err
	}
	if diskMB <= 0 {
		diskMB = 256
	}
	if err := runLoggedCommand("truncate", "-s", fmt.Sprintf("%dM", diskMB), imagePath); err != nil {
		return "", err
	}
	if err := runLoggedCommand("mkfs.ext4", "-F", imagePath); err != nil {
		return "", err
	}
	if err := runLoggedCommand("mount", "-o", "loop", imagePath, rootfs); err != nil {
		return "", err
	}

	progress.Add("cell-rootfs", "running", "creating cell rootfs from imported base")
	if err := copyDir(cfg.BaseRootFS, rootfs); err != nil {
		return "", err
	}

	progress.Add("cell-rootfs", "done", "cell rootfs created")
	return rootfs, nil
}

func UnmountCellRootFS(rootfs string) {
	if rootfs == "" {
		return
	}
	_ = exec.Command("umount", rootfs).Run()
}

func EnsureCellRootFSMounted(cell Cell) error {
	if cell.RootFS == "" {
		return fmt.Errorf("cell %q has no rootfs path", cell.Name)
	}
	if err := exec.Command("mountpoint", "-q", cell.RootFS).Run(); err == nil {
		return nil
	}

	imagePath := filepath.Join(filepath.Dir(cell.RootFS), "rootfs.ext4")
	if _, err := os.Stat(imagePath); err != nil {
		return err
	}
	if err := os.MkdirAll(cell.RootFS, 0755); err != nil {
		return err
	}
	return runLoggedCommand("mount", "-o", "loop", imagePath, cell.RootFS)
}

func copyDir(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	_ = os.Chmod(dst, info.Mode().Perm())
	_ = os.Chown(dst, osUserID(info), osGroupID(info))

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.Name() == "proc" || e.Name() == "sys" ||
			e.Name() == "dev" || e.Name() == "run" || e.Name() == "tmp" {
			continue
		}

		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())

		entryInfo, err := os.Lstat(srcPath)
		if err != nil {
			return err
		}

		if entryInfo.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}

		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if _, err := os.Lstat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return err
		}
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.Symlink(target, dst); err != nil {
			return err
		}
		_ = os.Lchown(dst, osUserID(info), osGroupID(info))
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}

	_ = out.Chmod(info.Mode().Perm())
	_ = os.Chown(dst, osUserID(info), osGroupID(info))
	return nil
}

func RunCellChild(rootfs string) error {
	fmt.Println("[cell] entering rootfs:", rootfs)

	if err := syscall.Sethostname([]byte("sparkd-cell")); err != nil {
		return err
	}

	if err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		return err
	}
	if err := syscall.Mount(rootfs, rootfs, "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return err
	}
	if err := syscall.Chroot(rootfs); err != nil {
		return err
	}
	if err := os.Chdir("/"); err != nil {
		return err
	}

	if err := os.MkdirAll("/proc", 0755); err != nil {
		return err
	}
	if err := syscall.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		return err
	}

	if err := mountTmpfs("/tmp", "mode=1777", 01777); err != nil {
		return err
	}
	if err := mountTmpfs("/run", "mode=0755", 0755); err != nil {
		return err
	}
	if err := setupDev(); err != nil {
		return err
	}

	if err := os.MkdirAll("/etc", 0755); err != nil {
		return err
	}
	if err := os.WriteFile("/etc/resolv.conf", []byte("nameserver 1.1.1.1\nnameserver 8.8.8.8\n"), 0644); err != nil {
		return err
	}

	waitForNetworkSetup()

	dbType := normalizeDBType(os.Getenv("SPARKD_DB_TYPE"))
	if dbType == DBTypePostgres {
		if err := configurePostgresCell(); err != nil {
			return err
		}
	}

	fmt.Println("SPARKD_READY")
	waitForCellShutdown()
	return nil
}

func mountTmpfs(target, options string, mode os.FileMode) error {
	if err := os.MkdirAll(target, mode); err != nil {
		return err
	}
	if err := syscall.Mount("tmpfs", target, "tmpfs", 0, options); err != nil {
		return err
	}
	_ = os.Chmod(target, mode)
	return nil
}

func setupDev() error {
	if err := mountTmpfs("/dev", "mode=0755", 0755); err != nil {
		return err
	}
	for _, device := range []struct {
		path         string
		major, minor int
		mode         uint32
	}{
		{"/dev/null", 1, 3, 0666},
		{"/dev/zero", 1, 5, 0666},
		{"/dev/full", 1, 7, 0666},
		{"/dev/random", 1, 8, 0666},
		{"/dev/urandom", 1, 9, 0666},
		{"/dev/tty", 5, 0, 0666},
	} {
		if err := createCharDevice(device.path, device.major, device.minor, device.mode); err != nil {
			return err
		}
	}

	if err := os.MkdirAll("/dev/shm", 0777); err != nil {
		return err
	}
	if err := syscall.Mount("tmpfs", "/dev/shm", "tmpfs", 0, "mode=1777,size=64m"); err != nil {
		return err
	}
	_ = os.Chmod("/dev/shm", 01777)
	return nil
}

func createCharDevice(path string, major, minor int, mode uint32) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		devID := (major << 8) | minor
		if err := syscall.Mknod(path, syscall.S_IFCHR|mode, devID); err != nil {
			return err
		}
	}
	_ = os.Chmod(path, os.FileMode(mode))
	return nil
}

func waitForNetworkSetup() {
	syncPipe := os.NewFile(3, "sync_pipe")
	if syncPipe == nil {
		return
	}
	defer syncPipe.Close()

	buf := make([]byte, 1)
	_, _ = syncPipe.Read(buf)
}

func configurePostgresCell() error {
	dbName := getenv("SPARKD_DB_NAME", "sparkd")
	user := getenv("SPARKD_DB_USER", "postgres")
	password := getenv("SPARKD_DB_PASSWORD", "")

	fmt.Println("[postgres] preparing runtime directories")
	if err := os.MkdirAll("/run/postgresql", 0775); err != nil {
		return err
	}
	if err := runLoggedCommand("chown", "postgres:postgres", "/run/postgresql"); err != nil {
		return err
	}
	_ = os.Chmod("/run/postgresql", 0775)

	if err := os.MkdirAll("/var/lib/postgresql/data", 0700); err != nil {
		return err
	}
	if err := runLoggedCommand("chown", "-R", "postgres:postgres", "/var/lib/postgresql"); err != nil {
		return err
	}

	if _, err := os.Stat("/var/lib/postgresql/data/PG_VERSION"); os.IsNotExist(err) {
		fmt.Println("[postgres] initializing database cluster")
		if err := runLoggedCommand("su", "-", "postgres", "-c", "initdb -D /var/lib/postgresql/data -A trust"); err != nil {
			return err
		}
	}

	if err := appendLines("/var/lib/postgresql/data/postgresql.conf",
		"listen_addresses = '*'",
		"unix_socket_directories = '/run/postgresql'",
		"password_encryption = 'scram-sha-256'",
	); err != nil {
		return err
	}

	if err := appendLines("/var/lib/postgresql/data/pg_hba.conf",
		"local all all trust",
		"host all all 0.0.0.0/0 scram-sha-256",
		"host all all ::/0 scram-sha-256",
	); err != nil {
		return err
	}

	fmt.Println("[postgres] starting database server")
	if err := runLoggedCommand("su", "-", "postgres", "-c", "pg_ctl -w -t 30 -D /var/lib/postgresql/data -l /tmp/postgres.log start"); err != nil {
		logContent, _ := os.ReadFile("/tmp/postgres.log")
		fmt.Printf("[-] Postgres Internal Log:\n%s\n", string(logContent))
		return err
	}

	if password != "" {
		fmt.Println("[postgres] applying credentials")
		sqlPath := "/tmp/sparkd-init.sql"
		sql := fmt.Sprintf(
			"ALTER USER %s WITH PASSWORD %s;\nCREATE DATABASE %s OWNER %s;\n",
			postgresIdentifier(user),
			postgresLiteral(password),
			postgresIdentifier(dbName),
			postgresIdentifier(user),
		)
		if err := os.WriteFile(sqlPath, []byte(sql), 0600); err != nil {
			return err
		}
		if err := runLoggedCommand("su", "-", "postgres", "-c", "psql -h /run/postgresql -U postgres -d postgres -v ON_ERROR_STOP=1 -f "+sqlPath); err != nil {
			return err
		}
	}

	return nil
}

func waitForCellShutdown() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	fmt.Println("[cell] shutdown requested")
	_ = runLoggedCommand("su", "-", "postgres", "-c", "pg_ctl -D /var/lib/postgresql/data -m fast stop")
}

func appendLines(path string, lines ...string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, line := range lines {
		if _, err := fmt.Fprintln(f, line); err != nil {
			return err
		}
	}

	return nil
}

func writeBaseResolver(rootfs string) error {
	etc := filepath.Join(rootfs, "etc")
	if err := os.MkdirAll(etc, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(etc, "resolv.conf"), []byte("nameserver 1.1.1.1\nnameserver 8.8.8.8\n"), 0644)
}

func postgresIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func postgresLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func runLoggedCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		fmt.Printf("[-] Command failed: %s %s\n%s\n", name, strings.Join(args, " "), output.String())
		return err
	}

	if strings.TrimSpace(output.String()) != "" {
		fmt.Print(output.String())
	}
	return nil
}

func osUserID(info os.FileInfo) int {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(stat.Uid)
	}
	return os.Getuid()
}

func osGroupID(info os.FileInfo) int {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(stat.Gid)
	}
	return os.Getgid()
}
