package network

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
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
