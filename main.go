package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"kc-go/pkg/auth"
	"kc-go/pkg/ddns"
	"kc-go/pkg/network"
)

// loginFailureCooldown 是登录失败后的退避时长，避免每秒冲击认证服务器。
const loginFailureCooldown = 10 * time.Second

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

	if pid, st, err := readPID(); err == nil && pid > 0 && isOurProcess(pid, st) {
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

	tz := applyTimezone(cfg.Service.Timezone)

	closer, err := SetupLogging(cfg.Service.LogFile, cfg.Service.LogMaxSize, cfg.Service.LogBackups)
	if err != nil {
		log.Printf("[ERROR] Failed to prepare log file %s: %v", cfg.Service.LogFile, err)
		return 1
	}
	defer closer.Close()

	if tz != "" {
		log.Printf("[INFO] Timezone set to %s", tz)
	}

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
	pid, st, err := readPID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s is not running\n", ServiceName)
		return 1
	}
	if !isOurProcess(pid, st) {
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
	pid, st, err := readPID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s is not running\n", ServiceName)
		return 1
	}
	if !isOurProcess(pid, st) {
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
	pid := os.Getpid()
	st, err := processStartTime(pid)
	if err != nil {
		// 取不到 starttime 时退化为只写 pid，isOurProcess 会用存活检测兜底
		return os.WriteFile(DefaultPIDPath, []byte(strconv.Itoa(pid)), 0644)
	}
	content := fmt.Sprintf("%d:%d", pid, st)
	return os.WriteFile(DefaultPIDPath, []byte(content), 0644)
}

func removePID() {
	_ = os.Remove(DefaultPIDPath)
}

// readPID 解析 PID 文件，格式为 "pid" 或 "pid:starttime"。
func readPID() (pid int, startTime int64, err error) {
	data, err := os.ReadFile(DefaultPIDPath)
	if err != nil {
		return 0, 0, err
	}
	s := string(data)
	if i := strings.IndexByte(s, ':'); i >= 0 {
		pid, err = strconv.Atoi(s[:i])
		if err != nil {
			return 0, 0, err
		}
		startTime, _ = strconv.ParseInt(s[i+1:], 10, 64)
		return pid, startTime, nil
	}
	pid, err = strconv.Atoi(s)
	return pid, 0, err
}

func isProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return !errors.Is(err, syscall.ESRCH)
}

// isOurProcess 判断 PID 文件里记录的进程是否就是当前实例。
// 同时校验进程存活与 starttime：进程退出后即使 PID 被复用，starttime 也不同，避免误判。
func isOurProcess(pid int, startTime int64) bool {
	if !isProcessAlive(pid) {
		return false
	}
	if startTime == 0 {
		// 文件里没有 starttime（取不到或旧格式），只能退化为存活检测
		return true
	}
	st, err := processStartTime(pid)
	if err != nil {
		return true
	}
	return st == startTime
}

// processStartTime 读取 /proc/<pid>/stat 的 starttime（第 22 字段），
// 作为进程身份指纹——在该进程生命周期内不变。
func processStartTime(pid int) (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	// /proc/<pid>/stat 的 comm 字段可能含空格和括号，从最后一个 ')' 之后解析
	end := bytes.LastIndexByte(data, ')')
	if end < 0 || end+1 >= len(data) {
		return 0, fmt.Errorf("parse /proc/%d/stat: no comm", pid)
	}
	fields := strings.Fields(string(data[end+1:]))
	// comm 后第 20 个字段是 starttime（state=1, ppid=2, ... starttime=20）
	if len(fields) < 20 {
		return 0, fmt.Errorf("parse /proc/%d/stat: too few fields", pid)
	}
	return strconv.ParseInt(fields[19], 10, 64)
}

func runService(ctx context.Context, cfg *Config) {
	var lastIP string
	var loggedIn bool
	var loginCooldown time.Duration // 非零表示上次登录未成功，需等待后再试

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		iface, err := network.GetDefaultInterface()
		if err != nil {
			log.Printf("[ERROR] Cannot find default interface: %v", err)
		} else {
			ip, err := network.GetInterfaceIP(iface, false)
			if err != nil || ip == "" {
				log.Printf("[WARN] No IP on %s: %v", iface, err)
			} else if ip != lastIP {
				if lastIP == "" {
					log.Printf("[INFO] IP acquired: %s", ip)
				} else {
					log.Printf("[INFO] IP changed: %s -> %s", lastIP, ip)
				}
				lastIP = ip
				loggedIn = false
				loginCooldown = 0
			}

			if lastIP != "" && !loggedIn && loginCooldown <= 0 {
				body, err := auth.LoginWithRetryDelay(cfg.Account.User, cfg.Account.Password, lastIP, "", cfg.Portal.BaseURL, cfg.Portal.ACIP, 1, 0)
				if err != nil {
					// 登录失败后退避，避免每秒冲击认证服务器（1秒轮询 + 3秒超时会堆积）
					loginCooldown = loginFailureCooldown
					log.Printf("[WARN] Login error, backing off %s: %v | body: %s", loginCooldown, redact(err.Error()), redact(body))
				} else if auth.IsLoginSuccess(body) {
					// 区分「真正登录成功」与「已经在线」：后者不算新动作，降级为 INFO
					if strings.Contains(body, "已经在线") {
						log.Printf("[INFO] Already online: %s | body: %s", lastIP, body)
					} else {
						log.Printf("[SUCCESS] %s", body)
					}
					loggedIn = true
					handleDDNS(cfg, lastIP)
				} else {
					loginCooldown = loginFailureCooldown
					log.Printf("[INFO] Login unsuccessful, backing off %s: %s", loginCooldown, body)
				}
			}
		}

		// 等待下一轮：正常按 interval，登录失败冷却期内按剩余冷却时间
		wait := time.Duration(cfg.Check.Interval) * time.Second
		if loginCooldown > 0 && loginCooldown > wait {
			wait = loginCooldown
		}
		if loginCooldown > 0 {
			loginCooldown -= wait
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func handleDDNS(cfg *Config, currentIP string) {
	if cfg.Cloudflare.Token == "" {
		return
	}

	// 所有 IPv6 地址都来自同一张网卡，只解析一次
	var iface string
	needV6 := false
	for _, domain := range cfg.Cloudflare.Domains {
		if domain.IPv6 {
			needV6 = true
			break
		}
	}
	if needV6 {
		var err error
		iface, err = network.GetDefaultInterface()
		if err != nil {
			log.Printf("[DDNS ERROR] Cannot resolve default interface: %v", err)
			return
		}
	}

	for _, domain := range cfg.Cloudflare.Domains {
		if domain.IPv4 {
			updated, err := ddns.UpdateRecord(cfg.Cloudflare.Token, cfg.Cloudflare.ZoneID, domain.Name, "A", currentIP)
			reportDDNS(domain.Name, currentIP, updated, err)
		}
		if domain.IPv6 {
			ip, err := network.GetInterfaceIP(iface, true)
			if err != nil {
				log.Printf("[DDNS ERROR] Cannot get %s IPv6 address: %v", domain.Name, err)
				continue
			}
			updated, err := ddns.UpdateRecord(cfg.Cloudflare.Token, cfg.Cloudflare.ZoneID, domain.Name, "AAAA", ip)
			reportDDNS(domain.Name, ip, updated, err)
		}
	}
}

func reportDDNS(name, ip string, updated bool, err error) {
	switch {
	case err != nil:
		log.Printf("[DDNS ERROR] Failed to update %s: %v", name, err)
	case updated:
		log.Printf("[DDNS SUCCESS] Updated %s to %s", name, ip)
	default:
		log.Printf("[DDNS INFO] %s already points to %s", name, ip)
	}
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
