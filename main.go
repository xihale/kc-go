package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"kc-go/pkg/auth"
	"kc-go/pkg/ddns"
	"kc-go/pkg/monitor"
	"kc-go/pkg/network"
)

func main() {
	os.Exit(runCLI(os.Args[1:]))
}

func runCLI(args []string) int {
	if len(args) == 0 || isFlagArg(args[0]) {
		for _, a := range args {
			if a == "-h" || a == "--help" {
				printUsage()
				return 0
			}
		}
		return runCommand(args)
	}

	switch args[0] {
	case "run", "start", "daemon":
		return runCommand(args[1:])
	case "stop":
		return stopCommand()
	case "status":
		return statusCommand()
	case "install":
		return installCommand(args[1:])
	case "uninstall", "remove":
		return uninstallCommand(args[1:])
	case "log", "logs":
		return logCommand(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return 0
	default:
		printUsage()
		return 2
	}
}

func runCommand(args []string) int {
	alreadyDaemon := false
	filteredArgs := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--_daemon" {
			alreadyDaemon = true
		} else {
			filteredArgs = append(filteredArgs, a)
		}
	}
	args = filteredArgs

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPathFlag := fs.String("c", "", "path to config file")
	foreground := fs.Bool("f", false, "run in foreground (do not daemonize)")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if pid, err := readPID(); err == nil && pid > 0 && isProcessAlive(pid) {
		fmt.Fprintf(os.Stderr, "%s is already running (pid %d)\n", ServiceName, pid)
		return 1
	}

	configPath := ResolveConfigPath(*configPathFlag)
	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Printf("[ERROR] Failed to load config %s: %v", configPath, err)
		return 1
	}

	if !*foreground && !alreadyDaemon {
		if err := daemonize(); err != nil {
			fmt.Fprintf(os.Stderr, "daemonize failed: %v\n", err)
			return 1
		}
	}

	closer, err := SetupLogging(cfg.Service.LogFile)
	if err != nil {
		log.Printf("[ERROR] Failed to prepare log file %s: %v", cfg.Service.LogFile, err)
		return 1
	}
	defer closer.Close()

	if err := writePID(); err != nil {
		log.Printf("[WARN] Failed to write PID file: %v", err)
	}
	defer removePID()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		log.Println("[INFO] Received signal, shutting down...")
		cancel()
	}()

	log.Printf("[INFO] Starting %s with config %s", ServiceName, configPath)
	runService(ctx, cfg)
	return 0
}

func stopCommand() int {
	pid, err := readPID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s is not running\n", ServiceName)
		return 1
	}
	if !isProcessAlive(pid) {
		removePID()
		fmt.Fprintf(os.Stderr, "%s is not running (stale PID %d removed)\n", ServiceName, pid)
		return 1
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "failed to kill pid %d: %v\n", pid, err)
		return 1
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			removePID()
			fmt.Fprintf(os.Stderr, "stopped %s (pid %d)\n", ServiceName, pid)
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Fprintf(os.Stderr, "timeout waiting for pid %d to exit\n", pid)
	return 1
}

func statusCommand() int {
	pid, err := readPID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s is not running\n", ServiceName)
		return 1
	}
	if !isProcessAlive(pid) {
		removePID()
		fmt.Fprintf(os.Stderr, "%s is not running (stale PID %d removed)\n", ServiceName, pid)
		return 1
	}
	fmt.Fprintf(os.Stderr, "%s is running (pid %d)\n", ServiceName, pid)
	return 0
}

func daemonize() error {
	binPath, err := os.Executable()
	if err != nil {
		return err
	}
	childArgs := append(os.Args, "--_daemon")
	attr := os.ProcAttr{
		Files: []*os.File{nil, nil, nil},
	}
	proc, err := os.StartProcess(binPath, childArgs, &attr)
	if err != nil {
		return err
	}

	time.Sleep(200 * time.Millisecond)
	if !isProcessAlive(proc.Pid) {
		return fmt.Errorf("child process exited immediately")
	}

	fmt.Fprintf(os.Stderr, "%s started (pid %d)\n", ServiceName, proc.Pid)
	proc.Release()
	os.Exit(0)
	return nil
}

func writePID() error {
	return os.WriteFile(DefaultPIDPath, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func removePID() {
	_ = os.Remove(DefaultPIDPath)
}

func readPID() (int, error) {
	data, err := os.ReadFile(DefaultPIDPath)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

func isProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return !errors.Is(err, syscall.ESRCH)
}

func runService(ctx context.Context, cfg *Config) {
	var lastDDNSUpdate time.Time

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		res, code, redirectLocation, err := monitor.CheckConnectivity(cfg.Check.URL)
		if res == monitor.ResultSuccess {
			log.Printf("[SUCCESS] Network is up (HTTP %d)", code)
			if shouldUpdateDDNS(lastDDNSUpdate) {
				handleDDNS(cfg)
				lastDDNSUpdate = time.Now()
			}
		} else {
			if res == monitor.ResultPortal {
				log.Printf("[INFO] Portal detected (HTTP %d). Attempting login...", code)
			} else {
				log.Printf("[WARN] Network down (HTTP %d/Err: %v). Reconnecting...", code, err)
			}
			reconnect(ctx, cfg, redirectLocation)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(cfg.Check.Interval) * time.Second):
		}
	}
}

func reconnect(ctx context.Context, cfg *Config, redirectLocation string) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	iface, err := network.GetDefaultInterface()
	if err != nil {
		log.Printf("[ERROR] Cannot find default interface: %v", err)
		return
	}

	newMAC, err := network.GenerateRandomMAC()
	if err != nil {
		log.Printf("[ERROR] MAC generation failed: %v", err)
		return
	}
	log.Printf("[ACTION] Changing MAC of %s to %s", iface, newMAC)
	if err := network.ChangeMAC(iface, newMAC); err != nil {
		log.Printf("[ERROR] MAC change failed: %v", err)
		return
	}

	log.Println("[INFO] Requesting DHCP lease...")
	if err := network.RenewDHCP(iface, 2, 1); err != nil {
		log.Printf("[ERROR] DHCP request failed: %v", err)
		return
	}

	select {
	case <-ctx.Done():
		return
	default:
	}

	log.Println("[INFO] Waiting for IP...")
	ip, err := network.WaitForIP(iface, 3*time.Second)
	if err != nil {
		log.Printf("[ERROR] Failed to get IP: %v", err)
		return
	}
	log.Printf("[INFO] New IP obtained: %s", ip)

	portalLocation := redirectLocation
	if portalLocation == "" {
		res, _, detectedRedirect, err := monitor.CheckConnectivity(cfg.Check.URL)
		switch res {
		case monitor.ResultSuccess:
			log.Println("[INFO] Network recovered before portal login.")
			return
		case monitor.ResultPortal:
			portalLocation = detectedRedirect
		default:
			if err != nil {
				log.Printf("[WARN] Pre-login probe failed: %v", err)
			}
		}
	}

	select {
	case <-ctx.Done():
		return
	default:
	}

	log.Println("[ACTION] Performing portal login...")
	if err := auth.LoginWithRetry(cfg.Account.User, cfg.Account.Password, ip, portalLocation, cfg.Portal.BaseURL, cfg.Portal.ACIP, 3); err != nil {
		log.Printf("[ERROR] Login failed: %v", err)
	}
}

func handleDDNS(cfg *Config) {
	if cfg.Cloudflare.Token == "" {
		return
	}

	iface, err := network.GetDefaultInterface()
	if err != nil {
		log.Printf("[DDNS ERROR] Cannot resolve default interface: %v", err)
		return
	}

	for _, domain := range cfg.Cloudflare.Domains {
		isIPv6 := domain.Type == "AAAA"
		ip, err := network.GetInterfaceIP(iface, isIPv6)
		if err != nil {
			log.Printf("[DDNS ERROR] Cannot get %s address: %v", domain.Type, err)
			continue
		}

		updated, err := ddns.UpdateRecord(cfg.Cloudflare.Token, cfg.Cloudflare.ZoneID, domain.Name, domain.Type, ip)
		if err != nil {
			log.Printf("[DDNS ERROR] Failed to update %s: %v", domain.Name, err)
		} else if updated {
			log.Printf("[DDNS SUCCESS] Updated %s to %s", domain.Name, ip)
		} else {
			log.Printf("[DDNS INFO] %s already points to %s", domain.Name, ip)
		}
	}
}

func shouldUpdateDDNS(lastUpdate time.Time) bool {
	return lastUpdate.IsZero() || time.Since(lastUpdate) >= time.Hour
}

func isFlagArg(value string) bool {
	return len(value) > 0 && value[0] == '-'
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  %s run [-c PATH] [-f]\n", ServiceName)
	fmt.Fprintf(os.Stderr, "  %s stop\n", ServiceName)
	fmt.Fprintf(os.Stderr, "  %s status\n", ServiceName)
	fmt.Fprintf(os.Stderr, "  %s install [-c PATH]\n", ServiceName)
	fmt.Fprintf(os.Stderr, "  %s uninstall [-p]\n", ServiceName)
	fmt.Fprintf(os.Stderr, "  %s log [-c PATH] [-n LINES]\n", ServiceName)
}
