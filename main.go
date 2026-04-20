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

const (
	fastReconnectMaxAttempts = 3
	fastDHCPAttempts         = 1
	fastDHCPTimeoutSec       = 1
	fastWaitForIPTimeout     = 1500 * time.Millisecond
	fastLoginRetries         = 2
	fastLoginRetryDelay      = 200 * time.Millisecond
	fastVerifyAttempts       = 2
	fastVerifyDelay          = 250 * time.Millisecond
	preRepairVerifyAttempts  = 2
	preRepairVerifyDelay     = 150 * time.Millisecond
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
			verifiedRes, verifiedCode, verifiedRedirect, verified := confirmConnectivityIssue(ctx, cfg.Check.URL, res, code, redirectLocation, err)
			if !verified {
				continue
			}
			if verifiedRes == monitor.ResultPortal {
				log.Printf("[INFO] Portal detected (HTTP %d). Attempting login...", verifiedCode)
			} else {
				log.Printf("[WARN] Network down (HTTP %d/Err: %v). Reconnecting...", verifiedCode, err)
			}
			reconnect(ctx, cfg, verifiedRedirect, verifiedRes == monitor.ResultPortal)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(cfg.Check.Interval) * time.Second):
		}
	}
}

func confirmConnectivityIssue(ctx context.Context, url string, initialRes monitor.CheckResult, initialCode int, initialRedirect string, initialErr error) (monitor.CheckResult, int, string, bool) {
	if initialRes == monitor.ResultPortal {
		return initialRes, initialCode, initialRedirect, true
	}

	for attempt := 1; attempt <= preRepairVerifyAttempts; attempt++ {
		if attempt > 1 && !sleepWithContext(ctx, preRepairVerifyDelay) {
			return monitor.ResultFailed, initialCode, initialRedirect, false
		}

		res, code, redirectLocation, err := monitor.CheckConnectivity(url)
		if res == monitor.ResultSuccess {
			log.Printf("[INFO] Probe recovered on verification attempt %d/%d (HTTP %d), skipping repair.", attempt, preRepairVerifyAttempts, code)
			return res, code, redirectLocation, false
		}
		if res == monitor.ResultPortal {
			return res, code, redirectLocation, true
		}
		if attempt == preRepairVerifyAttempts {
			if err != nil {
				return res, code, redirectLocation, true
			}
			return initialRes, code, redirectLocation, true
		}
	}

	return initialRes, initialCode, initialRedirect, initialErr != nil || initialRes != monitor.ResultSuccess
}

func reconnect(ctx context.Context, cfg *Config, redirectLocation string, portalDetected bool) {
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

	if portalDetected && tryPortalRecovery(ctx, cfg, iface, redirectLocation) {
		return
	}

	for attempt := 1; attempt <= fastReconnectMaxAttempts; attempt++ {
		if attempt > 1 {
			log.Printf("[WARN] Fast recovery retry %d/%d", attempt, fastReconnectMaxAttempts)
		}
		if tryFastReconnectAttempt(ctx, cfg, iface, redirectLocation, attempt) {
			return
		}
	}

	log.Printf("[ERROR] Fast recovery failed after %d attempts", fastReconnectMaxAttempts)
}

func tryPortalRecovery(ctx context.Context, cfg *Config, iface, redirectLocation string) bool {
	ip, err := network.GetInterfaceIP(iface, false)
	if err != nil || ip == "" {
		return false
	}

	log.Printf("[ACTION] Portal detected, trying immediate login on %s (%s)", iface, ip)
	if err := auth.LoginWithRetryDelay(cfg.Account.User, cfg.Account.Password, ip, redirectLocation, cfg.Portal.BaseURL, cfg.Portal.ACIP, fastLoginRetries, fastLoginRetryDelay); err != nil {
		log.Printf("[WARN] Immediate portal login failed: %v", err)
		return false
	}

	if verifyConnectivity(ctx, cfg.Check.URL, fastVerifyAttempts, fastVerifyDelay) {
		log.Println("[SUCCESS] Portal login restored connectivity without MAC churn.")
		return true
	}

	log.Println("[WARN] Immediate portal login did not restore connectivity, falling back to fast reconnect.")
	return false
}

func tryFastReconnectAttempt(ctx context.Context, cfg *Config, iface, redirectLocation string, attempt int) bool {
	select {
	case <-ctx.Done():
		return false
	default:
	}

	newMAC, err := network.GenerateRandomMAC()
	if err != nil {
		log.Printf("[ERROR] MAC generation failed: %v", err)
		return false
	}
	log.Printf("[ACTION] Fast recovery attempt %d/%d: changing MAC of %s to %s", attempt, fastReconnectMaxAttempts, iface, newMAC)
	if err := network.ChangeMAC(iface, newMAC); err != nil {
		log.Printf("[WARN] MAC change failed on attempt %d/%d: %v", attempt, fastReconnectMaxAttempts, err)
		return false
	}

	log.Println("[INFO] Requesting DHCP lease...")
	if err := network.RenewDHCP(iface, fastDHCPAttempts, fastDHCPTimeoutSec); err != nil {
		log.Printf("[WARN] DHCP request failed on attempt %d/%d: %v", attempt, fastReconnectMaxAttempts, err)
		return false
	}

	select {
	case <-ctx.Done():
		return false
	default:
	}

	log.Println("[INFO] Waiting for IP...")
	ip, err := network.WaitForIP(iface, fastWaitForIPTimeout)
	if err != nil {
		log.Printf("[WARN] Failed to get IP on attempt %d/%d: %v", attempt, fastReconnectMaxAttempts, err)
		return false
	}
	log.Printf("[INFO] New IP obtained: %s", ip)

	portalLocation := redirectLocation
	res, code, detectedRedirect, err := monitor.CheckConnectivity(cfg.Check.URL)
	switch res {
	case monitor.ResultSuccess:
		log.Printf("[SUCCESS] Network recovered immediately after DHCP (HTTP %d)", code)
		return true
	case monitor.ResultPortal:
		if detectedRedirect != "" {
			portalLocation = detectedRedirect
		}
	default:
		if err != nil {
			log.Printf("[WARN] Pre-login probe failed on attempt %d/%d: %v", attempt, fastReconnectMaxAttempts, err)
		}
	}

	select {
	case <-ctx.Done():
		return false
	default:
	}

	log.Println("[ACTION] Performing portal login...")
	if err := auth.LoginWithRetryDelay(cfg.Account.User, cfg.Account.Password, ip, portalLocation, cfg.Portal.BaseURL, cfg.Portal.ACIP, fastLoginRetries, fastLoginRetryDelay); err != nil {
		log.Printf("[WARN] Login failed on attempt %d/%d: %v", attempt, fastReconnectMaxAttempts, err)
		return false
	}

	if verifyConnectivity(ctx, cfg.Check.URL, fastVerifyAttempts, fastVerifyDelay) {
		log.Printf("[SUCCESS] Connectivity restored on fast recovery attempt %d/%d", attempt, fastReconnectMaxAttempts)
		return true
	}

	log.Printf("[WARN] Connectivity still unavailable after fast recovery attempt %d/%d", attempt, fastReconnectMaxAttempts)
	return false
}

func verifyConnectivity(ctx context.Context, url string, attempts int, delay time.Duration) bool {
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 && !sleepWithContext(ctx, delay) {
			return false
		}

		select {
		case <-ctx.Done():
			return false
		default:
		}

		res, code, _, err := monitor.CheckConnectivity(url)
		if res == monitor.ResultSuccess {
			log.Printf("[SUCCESS] Network is up (HTTP %d)", code)
			return true
		}
		if attempt == attempts && err != nil {
			log.Printf("[WARN] Connectivity verification failed: %v", err)
		}
	}
	return false
}

func sleepWithContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
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
