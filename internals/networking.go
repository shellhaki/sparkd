package internals

import (
	"fmt"
	"os/exec"
)

func SetupNetwork(pid int, name string) {
	vethHost := fmt.Sprintf("veth_%s", name)
	vethGuest := fmt.Sprintf("veth_%s_g", name)

	if err := exec.Command("ip", "link", "show", "br0").Run(); err != nil {
		Must(exec.Command("ip", "link", "add", "name", "br0", "type", "bridge").Run())
		Must(exec.Command("ip", "addr", "add", "10.0.0.1/24", "dev", "br0").Run())
		Must(exec.Command("ip", "link", "set", "dev", "br0", "up").Run())

		Must(exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1").Run())
		Must(exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", "10.0.0.0/24", "!", "-o", "br0", "-j", "MASQUERADE").Run())
	}

	Must(exec.Command("ip", "link", "add", vethHost, "type", "veth", "peer", "name", vethGuest).Run())
	Must(exec.Command("ip", "link", "set", vethHost, "master", "br0").Run())
	Must(exec.Command("ip", "link", "set", vethHost, "up").Run())

	Must(exec.Command("ip", "link", "set", vethGuest, "netns", fmt.Sprintf("%d", pid)).Run())

	nsCmd := fmt.Sprintf(
		"ip link set %s name eth0 && "+
			"ip addr add 10.0.0.2/24 dev eth0 && "+
			"ip link set eth0 up && "+
			"ip link set lo up && "+
			"ip route add default via 10.0.0.1",
		vethGuest,
	)
	Must(exec.Command("nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "sh", "-c", nsCmd).Run())
}
