package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func installCommand(args []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	configSource := fs.String("c", "", "seed config file path")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if os.Geteuid() != 0 {
		log.Printf("[ERROR] install must run as root")
		return 1
	}

	executablePath, err := os.Executable()
	if err != nil {
		log.Printf("[ERROR] Cannot resolve current executable: %v", err)
		return 1
	}

	if err := installBinary(executablePath, DefaultBinaryPath); err != nil {
		log.Printf("[ERROR] Install binary failed: %v", err)
		return 1
	}
	if err := ensureConfigFile(*configSource, DefaultConfigPath); err != nil {
		log.Printf("[ERROR] Prepare config failed: %v", err)
		return 1
	}
	logPath := ResolveLogPathFromConfig(DefaultConfigPath)
	if err := ensureLogFile(logPath); err != nil {
		log.Printf("[ERROR] Prepare log file failed: %v", err)
		return 1
	}
	if err := writeInitScript(DefaultInitPath, DefaultBinaryPath, DefaultConfigPath); err != nil {
		log.Printf("[ERROR] Write init script failed: %v", err)
		return 1
	}
	if err := runInitScript("enable"); err != nil {
		log.Printf("[ERROR] Enable service failed: %v", err)
		return 1
	}
	if err := runInitScript("restart"); err != nil {
		log.Printf("[ERROR] Start service failed: %v", err)
		return 1
	}

	fmt.Fprintf(os.Stdout, "[SUCCESS] Installed %s\n", ServiceName)
	fmt.Fprintf(os.Stdout, "[INFO] Config: %s\n", DefaultConfigPath)
	fmt.Fprintf(os.Stdout, "[INFO] Log file: %s\n", logPath)
	return 0
}

func uninstallCommand(args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purge := fs.Bool("p", false, "remove config and log file too")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if os.Geteuid() != 0 {
		log.Printf("[ERROR] uninstall must run as root")
		return 1
	}

	logPath := ResolveLogPathFromConfig(DefaultConfigPath)
	if err := stopAndDisableService(); err != nil {
		log.Printf("[WARN] Service stop/disable had issues: %v", err)
	}

	if err := removeIfExists(DefaultInitPath); err != nil {
		log.Printf("[ERROR] Remove init script failed: %v", err)
		return 1
	}
	if err := removeIfExists(DefaultBinaryPath); err != nil {
		log.Printf("[ERROR] Remove binary failed: %v", err)
		return 1
	}

	if *purge {
		if err := removeIfExists(DefaultConfigPath); err != nil {
			log.Printf("[ERROR] Remove config failed: %v", err)
			return 1
		}
		if err := removeIfExists(logPath); err != nil {
			log.Printf("[ERROR] Remove log file failed: %v", err)
			return 1
		}
		if err := removeDirIfEmpty(DefaultConfigDir); err != nil {
			log.Printf("[WARN] Config directory cleanup skipped: %v", err)
		}
	}

	if *purge {
		fmt.Fprintf(os.Stdout, "[SUCCESS] Uninstalled %s and purged config/log data\n", ServiceName)
	} else {
		fmt.Fprintf(os.Stdout, "[SUCCESS] Uninstalled %s\n", ServiceName)
		fmt.Fprintf(os.Stdout, "[INFO] Kept config: %s\n", DefaultConfigPath)
		fmt.Fprintf(os.Stdout, "[INFO] Kept log file: %s\n", logPath)
	}
	return 0
}

func installBinary(sourcePath, targetPath string) error {
	cleanSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	cleanTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return err
	}
	if cleanSource == cleanTarget {
		return os.Chmod(cleanTarget, 0755)
	}

	return copyFile(cleanSource, cleanTarget, 0755)
}

func ensureConfigFile(sourcePath, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}
	if _, err := os.Stat(targetPath); err == nil {
		log.Printf("[INFO] Keeping existing config %s", targetPath)
		return nil
	}

	seedPath := sourcePath
	if seedPath == "" {
		if _, err := os.Stat("config.yaml"); err == nil {
			seedPath = "config.yaml"
		}
	}

	if seedPath != "" {
		return copyFile(seedPath, targetPath, 0600)
	}

	return writeFileAtomic(targetPath, []byte(DefaultConfigTemplate()), 0600)
}

func ensureLogFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	return file.Close()
}

func writeInitScript(path, binaryPath, configPath string) error {
	content := renderInitScript(binaryPath, configPath)
	return writeFileAtomic(path, []byte(content), 0755)
}

func renderInitScript(binaryPath, configPath string) string {
	return fmt.Sprintf(`#!/bin/sh /etc/rc.common

USE_PROCD=1
START=99
STOP=10

PROG=%q
CONFIG=%q

start_service() {
	[ -x "$PROG" ] || return 1
	[ -f "$CONFIG" ] || return 1

	procd_open_instance
	procd_set_param command "$PROG" run -f -c "$CONFIG"
	procd_set_param respawn 3600 5 0
	procd_close_instance
}
`, binaryPath, configPath)
}

func runInitScript(action string) error {
	cmd := exec.Command(DefaultInitPath, action)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func stopAndDisableService() error {
	if _, err := os.Stat(DefaultInitPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var firstErr error
	if err := runInitScript("stop"); err != nil {
		firstErr = err
	}
	if err := runInitScript("disable"); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func copyFile(sourcePath, targetPath string, mode os.FileMode) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	tempPath := targetPath + ".tmp"
	targetFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}

	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		_ = targetFile.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := targetFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Chmod(tempPath, mode); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return os.Rename(tempPath, targetPath)
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, content, mode); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, mode); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return os.Rename(tempPath, path)
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func removeDirIfEmpty(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("directory %s is not empty", path)
	}
	return os.Remove(path)
}
