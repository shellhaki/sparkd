package main

import (
	"fmt"
	"os"
	"os/exec"
	"shellhaki/sparkd/internals"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: sudo ./main <container-name>")
		os.Exit(1)
	}
	name := os.Args[1]
	internals.EnsureBaseRootFs()
	internals.EnsureContainerDir()
	containerPath := internals.CreateContainer(name)

	rPipe, wPipe, err := os.Pipe()
	internals.Must(err)

	cmd := exec.Command("/proc/self/exe", "child", containerPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID |
			syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWIPC |
			syscall.CLONE_NEWNET,
	}

	cmd.ExtraFiles = []*os.File{rPipe}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	internals.Must(cmd.Start())

	rPipe.Close()

	pid := cmd.Process.Pid
	internals.SetupNetwork(pid, name)

	wPipe.Close()

	internals.Must(cmd.Wait())
}

func init() {
	if len(os.Args) > 1 && os.Args[1] == "child" {
		internals.ContainerChild(os.Args[2])
		os.Exit(0)
	}
}
