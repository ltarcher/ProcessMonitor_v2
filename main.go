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
	"sync"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v3/process"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
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
	Processes        []ProcessConfig   `yaml:"processes"`
	RegistryMonitors []RegistryMonitor `yaml:"registry_monitors"`
}

// ProcessConfig represents the configuration for a single process
type ProcessConfig struct {
	Name             string   `yaml:"name"`
	Enable           bool     `yaml:"enable"` // 新增：是否启用此监控配置
	Args             []string `yaml:"args"`
	RestartCommand   string   `yaml:"restart_command"` // 重启时使用的程序路径
	WorkDir          string   `yaml:"work_dir"`        // 程序的工作目录
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
func startProcess(config ProcessConfig, isRestart bool) (*exec.Cmd, error) {
	// 检查进程是否已经在运行
	running, err := isProcessRunning(config.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to check if process is running: %v", err)
	}
	if running {
		return nil, fmt.Errorf("process %s is already running", config.Name)
	}

	// 检查排斥进程列表
	if hasExclude, foundProcesses := checkExcludeProcesses(config.ExcludeProcesses); hasExclude {
		logrus.Warnf("Found exclude processes %v, skipping start of %s", foundProcesses, config.Name)
		return nil, fmt.Errorf("exclude processes found: %v", foundProcesses)
	}

	var cmd *exec.Cmd

	if isRestart {
		// 如果是重启
		logrus.Infof("restart process: %s", config.Name)
	}

	// 确定使用哪个程序路径
	processName := config.Name
	if config.RestartCommand != "" {
		processName = config.RestartCommand
		logrus.Infof("Using restart command for process: %s", processName)
	}

	// Handle relative paths by adding "./" prefix if needed
	if !filepath.IsAbs(processName) && !strings.HasPrefix(processName, "./") && !strings.HasPrefix(processName, ".\\") {
		if runtime.GOOS == "windows" {
			processName = ".\\" + processName
		} else {
			processName = "./" + processName
		}
	}

	cmd = exec.Command(processName, config.Args...)

	// 设置工作目录（如果指定）
	if config.WorkDir != "" {
		cmd.Dir = config.WorkDir
		logrus.Infof("Setting working directory for %s: %s", config.Name, config.WorkDir)
	}

	// Set process attributes to prevent automatic termination when parent exits
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		}
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
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

	// Check if process is already running before initial start
	running, err := isProcessRunning(config.Name)
	if err != nil {
		logrus.Errorf("Failed to check if process %s is running: %v", config.Name, err)
	} else if running {
		logrus.Infof("Process %s is already running, skipping initial start", config.Name)
	} else {
		// Start the process initially only if it's not already running
		logrus.Infof("Starting initial process: %s", config.Name)
		cmd, err := startProcess(config, false) // 初始启动，isRestart = false
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
					// 即使 ProcessState 显示进程在运行，也通过名称再次检查
					running, _ := isProcessRunning(config.Name)
					if !running {
						logrus.Warnf("Process %s (PID: %d) was manually closed", config.Name, currentCmd.Process.Pid)
						needRestart = true
					} else {
						processRunning = true
						logrus.Debugf("Process %s (PID: %d) is running", config.Name, currentCmd.Process.Pid)
					}
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
				cmd, err := startProcess(config, true) // 重启进程，isRestart = true
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

// loadConfig loads the configuration from the specified file
func loadConfig(configFile string) (Config, error) {
	var config Config

	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return config, fmt.Errorf("error reading config file: %v", err)
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("error parsing config: %v", err)
	}

	return config, nil
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
	config, err := loadConfig(*configFile)
	if err != nil {
		logrus.Fatalf("Error loading config: %v", err)
	}

	// 向后兼容处理：如果没有指定 enable 字段，默认为 true
	for i := range config.Processes {
		if !config.Processes[i].Enable {
			config.Processes[i].Enable = true
		}
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

	// WaitGroup for registry monitors
	var wg sync.WaitGroup

	// Start monitoring each process
	for _, processConfig := range config.Processes {
		// 检查是否启用此配置
		if !processConfig.Enable {
			logrus.Infof("Skipping disabled process monitor: %s", processConfig.Name)
			continue
		}
		go monitorProcess(processConfig, ctx)
	}

	// Start registry monitoring (Windows only)
	if runtime.GOOS == "windows" && len(config.RegistryMonitors) > 0 {
		logrus.Infof("Starting registry monitoring for %d registry keys", len(config.RegistryMonitors))
		for _, regConfig := range config.RegistryMonitors {
			wg.Add(1)
			go MonitorRegistry(regConfig, ctx, &wg)
		}
	}

	// Wait for termination signal
	<-sigs
	logrus.Info("Received shutdown signal, stopping all processes...")
	cancel()

	// Give processes time to shutdown gracefully
	time.Sleep(2 * time.Second)
	logrus.Info("Process monitor shutdown complete")
}
