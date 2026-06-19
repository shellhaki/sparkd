package internals

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

var (
	baseRootfs    = "/home/haki/rootfs"
	containersDir = "/home/haki/containers"
)

func EnsureBaseRootFs() {
	if _, err := os.Stat(baseRootfs); err == nil {
		fmt.Println("Base rootFs already exists")
		return
	}

	fmt.Print("adding base rootfs now")

	Must(os.MkdirAll(baseRootfs, 0755))

	cmd := exec.Command("bash", "-c",
		"curl -L https://dl-2.alpinelinux.org/alpine/latest-stable/releases/x86_64/alpine-minirootfs-3.24.1-x86_64.tar.gz | tar -xz -C "+baseRootfs,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	Must(cmd.Run())

	fmt.Println("base filesystem installed at:", baseRootfs)
}

func EnsureContainerDir() {
	Must(os.MkdirAll(containersDir, 0755))
}

func CreateContainer(name string) string {
	containerRoot := filepath.Join(containersDir, name)
	containerRootfs := filepath.Join(containerRoot, "rootfs")

	if _, err := os.Stat(containerRoot); err == nil {
		Must(os.RemoveAll(containerRoot))
	}

	fmt.Println("creating container,", name)
	fmt.Println("Rootfs path:", containerRootfs)

	Must(os.MkdirAll(containerRootfs, 0755))
	copyDir(baseRootfs, containerRootfs)
	return containerRootfs
}

func copyDir(src, dst string) {
	entries, err := os.ReadDir(src)
	Must(err)

	for _, e := range entries {
		if e.Name() == "proc" || e.Name() == "sys" ||
			e.Name() == "dev" || e.Name() == "run" {
			continue
		}

		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())

		if e.IsDir() {
			Must(os.MkdirAll(dstPath, 0755))
			copyDir(srcPath, dstPath)
		} else {
			copyFile(srcPath, dstPath)
		}
	}
}

func copyFile(src, dst string) {
	info, err := os.Lstat(src)
	Must(err)

	if _, err := os.Lstat(dst); err == nil {
		Must(os.Remove(dst))
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		Must(err)
		Must(os.Symlink(target, dst))
		return
	}

	in, err := os.Open(src)
	Must(err)
	defer in.Close()

	out, err := os.Create(dst)
	Must(err)
	defer out.Close()

	_, err = io.Copy(out, in)
	Must(err)

	Must(out.Chmod(info.Mode()))
}

func ContainerChild(rootfs string) {
	fmt.Println("[*] Entering container:", rootfs)

	Must(syscall.Sethostname([]byte("mini-container")))

	Must(syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, ""))
	Must(syscall.Mount(rootfs, rootfs, "", syscall.MS_BIND|syscall.MS_REC, ""))

	Must(syscall.Chroot(rootfs))
	Must(os.Chdir("/"))

	Must(os.MkdirAll("/proc", 0755))
	Must(syscall.Mount("proc", "/proc", "proc", 0, ""))

	Must(os.MkdirAll("/tmp", 0755))
	Must(syscall.Mount("tmpfs", "/tmp", "tmpfs", 0, ""))

	Must(os.MkdirAll("/etc", 0755))
	Must(os.WriteFile("/etc/resolv.conf", []byte("nameserver 1.1.1.1\nnameserver 8.8.8.8\n"), 0644))

	fmt.Println("[*] Container ready")

	cmd := exec.Command("/bin/sh")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	Must(cmd.Run())
}
