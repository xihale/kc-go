package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
)

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func SetupLogging(logPath string) (io.Closer, error) {
	if logPath == "" {
		log.SetOutput(os.Stdout)
		return nopCloser{}, nil
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return nil, err
	}
	log.SetOutput(file)
	log.SetFlags(log.LstdFlags)
	return file, nil
}

func logCommand(args []string) int {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	configPathFlag := fs.String("c", "", "path to config file")
	lineCount := fs.Int("n", 20, "number of lines to show before following")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	configPath := ResolveConfigPath(*configPathFlag)
	logPath := DefaultLogPath
	if cfg, err := LoadConfig(configPath); err == nil && cfg.Service.LogFile != "" {
		logPath = cfg.Service.LogFile
	}

	if err := TailLog(logPath, *lineCount); err != nil {
		fmt.Fprintf(os.Stderr, "tail log failed: %v\n", err)
		return 1
	}
	return 0
}

func TailLog(path string, lineCount int) error {
	lines, err := readLastLines(path, lineCount)
	if err != nil {
		return err
	}
	for _, line := range lines {
		printColoredLine(line)
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()

	position, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(file)

	for {
		line, err := reader.ReadString('\n')
		if err == nil {
			position += int64(len(line))
			printColoredLine(line)
			continue
		}
		if !errors.Is(err, io.EOF) {
			return err
		}

		stat, statErr := os.Stat(path)
		if statErr != nil {
			return statErr
		}
		if stat.Size() < position {
			_ = file.Close()
			file, err = os.Open(path)
			if err != nil {
				return err
			}
			reader = bufio.NewReader(file)
			position = 0
			continue
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func readLastLines(path string, lineCount int) ([]string, error) {
	if lineCount <= 0 {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	buffer := make([]string, 0, lineCount)
	index := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if len(buffer) < lineCount {
			buffer = append(buffer, line)
			continue
		}
		buffer[index] = line
		index = (index + 1) % lineCount
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(buffer) < lineCount {
		return buffer, nil
	}

	result := make([]string, 0, len(buffer))
	result = append(result, buffer[index:]...)
	result = append(result, buffer[:index]...)
	return result, nil
}

func printColoredLine(line string) {
	trimmed := strings.TrimRight(line, "\n")
	fmt.Println(colorizeLine(trimmed))
}

func colorizeLine(line string) string {
	switch {
	case strings.Contains(line, "[ERROR]"):
		return colorRed + line + colorReset
	case strings.Contains(line, "[WARN]"):
		return colorYellow + line + colorReset
	case strings.Contains(line, "[SUCCESS]"):
		return colorGreen + line + colorReset
	case strings.Contains(line, "[ACTION]"):
		return colorCyan + line + colorReset
	case strings.Contains(line, "[INFO]"):
		return colorBlue + line + colorReset
	default:
		return line
	}
}
