package network

import (
	"crypto/rand"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func GetDefaultInterface() (string, error) {
	out, err := exec.Command("sh", "-c", "ip route | grep default | awk '{print $5}' | head -1").Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		ifaces, _ := net.Interfaces()
		for _, i := range ifaces {
			if i.Flags&net.FlagUp != 0 && i.Flags&net.FlagLoopback == 0 {
				return i.Name, nil
			}
		}
		return "", fmt.Errorf("no active interface found")
	}
	return strings.TrimSpace(string(out)), nil
}

func GenerateRandomMAC() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random MAC: %w", err)
	}
	buf[0] = (buf[0] & 0xfe) | 0x02
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		buf[0], buf[1], buf[2], buf[3], buf[4], buf[5]), nil
}

func ChangeMAC(iface, newMAC string) error {
	if err := exec.Command("ip", "link", "set", "dev", iface, "down").Run(); err != nil {
		return fmt.Errorf("bring down %s: %w", iface, err)
	}
	if err := exec.Command("ip", "link", "set", "dev", iface, "address", newMAC).Run(); err != nil {
		_ = exec.Command("ip", "link", "set", "dev", iface, "up").Run()
		return fmt.Errorf("change MAC on %s: %w", iface, err)
	}
	if err := exec.Command("ip", "link", "set", "dev", iface, "up").Run(); err != nil {
		return fmt.Errorf("bring up %s: %w", iface, err)
	}
	return nil
}

func RenewDHCP(iface string, attempts, timeoutSec int) error {
	if attempts < 1 {
		attempts = 1
	}
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	return exec.Command(
		"udhcpc",
		"-i", iface,
		"-n",
		"-q",
		"-R",
		"-t", strconv.Itoa(attempts),
		"-T", strconv.Itoa(timeoutSec),
	).Run()
}

func GetInterfaceIP(ifaceName string, isIPv6 bool) (string, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return "", err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if isIPv6 {
				if ipnet.IP.To4() == nil && ipnet.IP.To16() != nil && ipnet.IP.IsGlobalUnicast() {
					return ipnet.IP.String(), nil
				}
			} else {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String(), nil
				}
			}
		}
	}
	return "", fmt.Errorf("no address found for %s (IPv6: %v)", ifaceName, isIPv6)
}

func WaitForIP(ifaceName string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ip, err := GetInterfaceIP(ifaceName, false)
		if err == nil && ip != "" {
			return ip, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", fmt.Errorf("timeout waiting for IP on %s", ifaceName)
}
