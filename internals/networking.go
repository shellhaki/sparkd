package internals

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type NetworkPlan struct {
	CellIP    string
	HostPort  int
	InnerPort int
}

func PlanCellNetwork(existing []Cell, requestedPort int) (NetworkPlan, error) {
	usedOctets := map[int]bool{1: true}
	usedPorts := map[int]bool{}

	for _, cell := range existing {
		if cell.State == CellStateDeleted {
			continue
		}
		if cell.CellIP != "" {
			parts := strings.Split(cell.CellIP, ".")
			if len(parts) == 4 {
				if octet, err := strconv.Atoi(parts[3]); err == nil {
					usedOctets[octet] = true
				}
			}
		}
		if cell.HostPort > 0 {
			usedPorts[cell.HostPort] = true
		}
	}

	hostPort := requestedPort
	if hostPort == 0 {
		hostPort = 3001
		for usedPorts[hostPort] {
			hostPort++
		}
	} else if usedPorts[hostPort] {
		return NetworkPlan{}, fmt.Errorf("host port %d is already used by another cell", hostPort)
	}

	octet := 2
	for usedOctets[octet] && octet < 255 {
		octet++
	}
	if octet >= 255 {
		return NetworkPlan{}, fmt.Errorf("no free cell IPs left")
	}

	return NetworkPlan{
		CellIP:    fmt.Sprintf("10.42.0.%d", octet),
		HostPort:  hostPort,
		InnerPort: 5432,
	}, nil
}

func SetupCellNetwork(cfg Config, pid int, cell Cell) error {
	vethHost := fmt.Sprintf("sp_%s_h", shortLinkName(cell.Name))
	vethGuest := fmt.Sprintf("sp_%s_g", shortLinkName(cell.Name))

	if err := exec.Command("ip", "link", "show", cfg.BridgeName).Run(); err != nil {
		if err := runLoggedCommand("ip", "link", "add", "name", cfg.BridgeName, "type", "bridge"); err != nil {
			return err
		}
		if err := runLoggedCommand("ip", "addr", "add", cfg.BridgeCIDR, "dev", cfg.BridgeName); err != nil {
			return err
		}
		if err := runLoggedCommand("ip", "link", "set", "dev", cfg.BridgeName, "up"); err != nil {
			return err
		}
		if err := runLoggedCommand("sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
			return err
		}
		if err := runLoggedCommand("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", "10.42.0.0/24", "!", "-o", cfg.BridgeName, "-j", "MASQUERADE"); err != nil {
			return err
		}
	}

	_ = exec.Command("ip", "link", "delete", vethHost).Run()

	if err := runLoggedCommand("ip", "link", "add", vethHost, "type", "veth", "peer", "name", vethGuest); err != nil {
		return err
	}
	if err := runLoggedCommand("ip", "link", "set", vethHost, "master", cfg.BridgeName); err != nil {
		return err
	}
	if err := runLoggedCommand("ip", "link", "set", vethHost, "up"); err != nil {
		return err
	}
	if err := runLoggedCommand("ip", "link", "set", vethGuest, "netns", fmt.Sprintf("%d", pid)); err != nil {
		return err
	}

	nsCmd := fmt.Sprintf(
		"ip link set %s name eth0 && "+
			"ip addr add %s/24 dev eth0 && "+
			"ip link set eth0 up && "+
			"ip link set lo up && "+
			"ip route add default via %s",
		vethGuest,
		cell.CellIP,
		cfg.BridgeIP,
	)
	if err := runLoggedCommand("nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "sh", "-c", nsCmd); err != nil {
		return err
	}

	_ = exec.Command("iptables", "-t", "nat", "-D", "PREROUTING",
		"-p", "tcp", "--dport", fmt.Sprintf("%d", cell.HostPort),
		"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", cell.CellIP, cell.InnerPort)).Run()

	return runLoggedCommand("iptables", "-t", "nat", "-A", "PREROUTING",
		"-p", "tcp", "--dport", fmt.Sprintf("%d", cell.HostPort),
		"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", cell.CellIP, cell.InnerPort))
}

func DeleteCellNetwork(cell Cell) {
	if cell.HostPort > 0 && cell.CellIP != "" && cell.InnerPort > 0 {
		_ = exec.Command("iptables", "-t", "nat", "-D", "PREROUTING",
			"-p", "tcp", "--dport", fmt.Sprintf("%d", cell.HostPort),
			"-j", "DNAT", "--to-destination", fmt.Sprintf("%s:%d", cell.CellIP, cell.InnerPort)).Run()
	}
	if cell.Name != "" {
		_ = exec.Command("ip", "link", "delete", fmt.Sprintf("sp_%s_h", shortLinkName(cell.Name))).Run()
	}
}

func shortLinkName(name string) string {
	clean := strings.ReplaceAll(sanitizeCellName(name), "-", "")
	if len(clean) > 8 {
		return clean[:8]
	}
	if clean == "" {
		return shortID()
	}
	return clean
}
