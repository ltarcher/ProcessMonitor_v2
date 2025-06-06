package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v3/process"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

// LogRotator handles log file rotation
type LogRotator struct {
	filename    string
	maxSize     int64 // Maximum size in bytes
	currentFile *os.File
}

func NewLogRotator(filename string, maxSize int64) *LogRotator {
	return &LogRotator{
		filename: filename,
		maxSize:  maxSize,
	}
}

func (lr *LogRotator) Write(p []byte) (n int, err error) {
	// Check if we need to rotate
	if lr.currentFile != nil {
		if stat, err := lr.currentFile.Stat(); err == nil {
			if stat.Size()+int64(len(p)) > lr.maxSize {
				lr.rotate()
			}
		}
	}

	// Open file if not already open
	if lr.currentFile == nil {
		lr.currentFile, err = os.OpenFile(lr.filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return 0, err
		}
	}

	// Write to both file and console
	n, err = lr.currentFile.Write(p)
	if err == nil {
		fmt.Print(string(p)) // Also print to console
	}
	return n, err
}

func (lr *LogRotator) rotate() {
	if lr.currentFile != nil {
		lr.currentFile.Close()
		lr.currentFile = nil
	}

	// Create backup filename with timestamp
	now := time.Now()
	backupName := fmt.Sprintf("%s.%s", lr.filename, now.Format("2006-01-02_15-04-05"))

	// Rename current log file to backup
	if err := os.Rename(lr.filename, backupName); err != nil {
		logrus.Errorf("Failed to rotate log file: %v", err)
		return
	}

	logrus.Infof("Log file rotated to: %s", backupName)
}

func (lr *LogRotator) Close() error {
	if lr.currentFile != nil {
		return lr.currentFile.Close()
	}
	return nil
}

// MonthlyCleanup removes log files older than 1 month
func (lr *LogRotator) MonthlyCleanup() {
	dir := filepath.Dir(lr.filename)
	baseName := filepath.Base(lr.filename)

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		logrus.Errorf("Failed to read log directory: %v", err)
		return
	}

	cutoff := time.Now().AddDate(0, -1, 0) // 1 month ago

	for _, file := range files {
		if strings.HasPrefix(file.Name(), baseName+".") && !file.IsDir() {
			if file.ModTime().Before(cutoff) {
				fullPath := filepath.Join(dir, file.Name())
				if err := os.Remove(fullPath); err != nil {
					logrus.Errorf("Failed to remove old log file %s: %v", fullPath, err)
				} else {
					logrus.Infof("Removed old log file: %s", fullPath)
				}
			}
		}
	}
}

// ConsoleHook sends logs to console as well as file
type ConsoleHook struct{}

func (hook *ConsoleHook) Fire(entry *logrus.Entry) error {
	// This hook is no longer needed as LogRotator handles console output
	return nil
}

func (hook *ConsoleHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Config represents the configuration structure
type Config struct {
	Processes []ProcessConfig `yaml:"processes"`
}

// ProcessConfig represents the configuration for a single process
type ProcessConfig struct {
	Name             string   `yaml:"name"`
	Args             []string `yaml:"args"`
	Ports            []int    `yaml:"ports"`
	HealthChecks     []string `yaml:"health_checks"`
	CheckInterval    int      `yaml:"check_interval"`
	RestartDelay     int      `yaml:"restart_delay"`
	KillOnExit       bool     `yaml:"kill_on_exit"`
	ExcludeProcesses []string `yaml:"exclude_processes"` // 进程排斥列表
}

// isProcessRunning checks if a process is running by name
func isProcessRunning(name string) (bool, error) {
	processes, err := process.Processes()
	if err != nil {
		return false, err
	}

	processName := filepath.Base(name)
	for _, p := range processes {
		exe, _ := p.Exe()
		cmdline, _ := p.Cmdline()
		// Check both executable path and command line
		if strings.Contains(exe, processName) || strings.Contains(cmdline, processName) {
			return true, nil
		}
	}
	return false, nil
}

// checkExcludeProcesses 检查排斥进程列表中的进程是否存在
func checkExcludeProcesses(excludeProcesses []string) (bool, []string) {
	if len(excludeProcesses) == 0 {
		return false, nil
	}

	processes, err := process.Processes()
	if err != nil {
		logrus.Errorf("Failed to get process list: %v", err)
		return false, nil
	}

	var foundProcesses []string

	for _, excludeName := range excludeProcesses {
		processName := filepath.Base(excludeName)
		for _, p := range processes {
			exe, _ := p.Exe()
			cmdline, _ := p.Cmdline()
			// Check both executable path and command line
			if strings.Contains(exe, processName) || strings.Contains(cmdline, processName) {
				foundProcesses = append(foundProcesses, excludeName)
				break
			}
		}
	}

	return len(foundProcesses) > 0, foundProcesses
}

// isPortInUse checks if a port is in use
func isPortInUse(port int) bool {
	// Try TCP connection
	tcpAddr := fmt.Sprintf("localhost:%d", port)
	conn, err := net.DialTimeout("tcp", tcpAddr, 2*time.Second)
	if err == nil {
		conn.Close()
		return true
	}
	return false
}

// isHealthCheckOK performs HTTP health check
func isHealthCheckOK(url string) bool {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// startProcess starts a new process
func startProcess(config ProcessConfig) (*exec.Cmd, error) {
	// 检查排斥进程列表
	if hasExclude, foundProcesses := checkExcludeProcesses(config.ExcludeProcesses); hasExclude {
		logrus.Warnf("Found exclude processes %v, skipping start of %s", foundProcesses, config.Name)
		return nil, fmt.Errorf("exclude processes found: %v", foundProcesses)
	}

	var cmd *exec.Cmd

	// Handle relative paths by adding "./" prefix if needed
	processName := config.Name
	if !filepath.IsAbs(processName) && !strings.HasPrefix(processName, "./") && !strings.HasPrefix(processName, ".\\") {
		if runtime.GOOS == "windows" {
			processName = ".\\" + processName
		} else {
			processName = "./" + processName
		}
	}

	cmd = exec.Command(processName, config.Args...)

	// Set process attributes to prevent automatic termination when parent exits
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		}
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	return cmd, err
}

// killExistingProcesses kills any existing processes with the same name
func killExistingProcesses(name string) {
	procs, _ := process.Processes()
	processName := filepath.Base(name)

	for _, p := range procs {
		exe, _ := p.Exe()
		cmdline, _ := p.Cmdline()
		if strings.Contains(exe, processName) || strings.Contains(cmdline, processName) {
			logrus.Infof("Killing existing process: %s (PID: %d)", name, p.Pid)
			p.Kill()
		}
	}
}

// monitorProcess monitors a process and restarts it if necessary
func monitorProcess(config ProcessConfig, ctx context.Context) {
	ticker := time.NewTicker(time.Duration(config.CheckInterval) * time.Second)
	defer ticker.Stop()

	var currentCmd *exec.Cmd
	var isRestarting bool

	// Start the process initially
	logrus.Infof("Starting initial process: %s", config.Name)
	cmd, err := startProcess(config)
	if err != nil {
		if strings.Contains(err.Error(), "exclude processes found") {
			logrus.Infof("Skipping initial start of %s due to exclude processes", config.Name)
		} else {
			logrus.Errorf("Failed to start initial process %s: %v", config.Name, err)
		}
	} else {
		currentCmd = cmd
		// Give the process some time to start up
		time.Sleep(2 * time.Second)
	}

	for {
		select {
		case <-ticker.C:
			// Skip monitoring if currently restarting
			if isRestarting {
				logrus.Debugf("Process %s is currently restarting, skipping check", config.Name)
				continue
			}

			needRestart := false
			processRunning := false

			// Check if current command is still running
			if currentCmd != nil && currentCmd.Process != nil {
				// Check if process is still alive using process state
				processState := currentCmd.ProcessState
				if processState != nil && processState.Exited() {
					logrus.Warnf("Managed process %s (PID: %d) has exited", config.Name, currentCmd.Process.Pid)
					needRestart = true
				} else {
					// Process seems to be running
					processRunning = true
					logrus.Debugf("Process %s (PID: %d) is running", config.Name, currentCmd.Process.Pid)
				}
			} else {
				// No current command, check if process exists by name
				running, _ := isProcessRunning(config.Name)
				if !running {
					logrus.Warnf("Process %s is not running", config.Name)
					needRestart = true
				} else {
					processRunning = true
				}
			}

			// Only check ports and health if process is running
			if processRunning {
				// Check ports if configured
				if len(config.Ports) > 0 {
					allPortsOK := true
					for _, port := range config.Ports {
						if !isPortInUse(port) {
							logrus.Warnf("Port %d is not in use for process %s", port, config.Name)
							allPortsOK = false
							break
						}
					}
					if !allPortsOK {
						needRestart = true
					}
				}

				// Check health checks if configured
				if !needRestart && len(config.HealthChecks) > 0 {
					allHealthOK := true
					for _, check := range config.HealthChecks {
						if !isHealthCheckOK(check) {
							logrus.Warnf("Health check failed for %s: %s", config.Name, check)
							allHealthOK = false
							break
						}
					}
					if !allHealthOK {
						needRestart = true
					}
				}
			}

			// If process needs restart
			if needRestart {
				isRestarting = true
				logrus.Warnf("Process %s needs to be restarted", config.Name)

				// Kill current process if it exists
				if currentCmd != nil && currentCmd.Process != nil {
					logrus.Infof("Terminating current process %s (PID: %d)", config.Name, currentCmd.Process.Pid)
					currentCmd.Process.Kill()
					currentCmd.Wait() // Wait for process to exit
					currentCmd = nil
				}

				// Kill any other instances of the process
				killExistingProcesses(config.Name)

				// Wait for restart delay
				if config.RestartDelay > 0 {
					logrus.Infof("Waiting %d seconds before restart", config.RestartDelay)
					time.Sleep(time.Duration(config.RestartDelay) * time.Second)
				}

				// Start new process
				cmd, err := startProcess(config)
				if err != nil {
					if strings.Contains(err.Error(), "exclude processes found") {
						logrus.Infof("Skipping restart of %s due to exclude processes", config.Name)
					} else {
						logrus.Errorf("Failed to restart process %s: %v", config.Name, err)
					}
					currentCmd = nil
				} else {
					logrus.Infof("Successfully restarted process %s (PID: %d)", config.Name, cmd.Process.Pid)
					currentCmd = cmd
					// Give the new process time to start up
					time.Sleep(2 * time.Second)
				}

				isRestarting = false
			} else if processRunning {
				logrus.Debugf("Process %s is healthy", config.Name)
			}

		case <-ctx.Done():
			if config.KillOnExit && currentCmd != nil && currentCmd.Process != nil {
				logrus.Infof("Stopping process %s (PID: %d)", config.Name, currentCmd.Process.Pid)
				currentCmd.Process.Kill()
				currentCmd.Wait()
			} else if currentCmd != nil && currentCmd.Process != nil {
				logrus.Infof("Leaving process %s (PID: %d) running", config.Name, currentCmd.Process.Pid)
			}
			return
		}
	}
}

// createSelfMonitorScript creates a script to monitor the monitor process itself
func createSelfMonitorScript() error {
	var scriptContent string
	var scriptName string

	if runtime.GOOS == "windows" {
		scriptName = "monitor_watchdog.bat"
		scriptContent = fmt.Sprintf(`@echo off
:loop
tasklist /FI "IMAGENAME eq processmonitor.exe" 2>NUL | find /I /N "processmonitor.exe">NUL
if "%%ERRORLEVEL%%"=="1" (
    echo Process monitor not running, restarting...
    start "" "%s" -config config.yaml
)
timeout /t 30 /nobreak >nul
goto loop`, os.Args[0])
	} else {
		scriptName = "monitor_watchdog.sh"
		scriptContent = fmt.Sprintf(`#!/bin/bash
while true; do
    if ! pgrep -f "processmonitor" > /dev/null; then
        echo "Process monitor not running, restarting..."
        nohup %s -config config.yaml &
    fi
    sleep 30
done`, os.Args[0])
	}

	return ioutil.WriteFile(scriptName, []byte(scriptContent), 0755)
}

func main() {
	// Parse command line flags
	configFile := flag.String("config", "config.yaml", "path to config file")
	createWatchdog := flag.Bool("create-watchdog", false, "create watchdog script for self-monitoring")
	flag.Parse()

	// Create watchdog script if requested
	if *createWatchdog {
		if err := createSelfMonitorScript(); err != nil {
			logrus.Fatalf("Error creating watchdog script: %v", err)
		}
		logrus.Info("Watchdog script created successfully")
		return
	}

	// Load configuration
	data, err := ioutil.ReadFile(*configFile)
	if err != nil {
		logrus.Fatalf("Error reading config file: %v", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		logrus.Fatalf("Error parsing config: %v", err)
	}

	// Set up context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up logging with rotation (100MB limit)
	logRotator := NewLogRotator("processmonitor.log", 100*1024*1024) // 100MB
	defer logRotator.Close()

	logrus.SetOutput(logRotator)
	logrus.SetLevel(logrus.InfoLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Start monthly cleanup routine
	go func() {
		ticker := time.NewTicker(24 * time.Hour) // Check daily
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Check if it's the first day of the month
				now := time.Now()
				if now.Day() == 1 {
					logRotator.MonthlyCleanup()
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	logrus.Infof("Starting Process Monitor v1.0")
	logrus.Infof("Monitoring %d processes", len(config.Processes))

	// Set up signal handling
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// Start monitoring each process
	for _, processConfig := range config.Processes {
		go monitorProcess(processConfig, ctx)
	}

	// Wait for termination signal
	<-sigs
	logrus.Info("Received shutdown signal, stopping all processes...")
	cancel()

	// Give processes time to shutdown gracefully
	time.Sleep(2 * time.Second)
	logrus.Info("Process monitor shutdown complete")
}
