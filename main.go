package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
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
)

// ConsoleHook sends logs to console as well as file
type ConsoleHook struct{}

func (hook *ConsoleHook) Fire(entry *logrus.Entry) error {
	line, err := entry.String()
	if err != nil {
		return err
	}
	fmt.Print(line)
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
	Name          string   `yaml:"name"`
	Args          []string `yaml:"args"`
	Ports         []int    `yaml:"ports"`
	HealthChecks  []string `yaml:"health_checks"`
	CheckInterval int      `yaml:"check_interval"`
	RestartDelay  int      `yaml:"restart_delay"`
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	return cmd, err
}

// restartProcess restarts a process with delay
func restartProcess(config ProcessConfig) (*exec.Cmd, error) {
	// Check if process is running and kill it if necessary
	procs, _ := process.Processes()
	for _, p := range procs {
		exe, _ := p.Exe()
		cmdline, _ := p.Cmdline()
		processName := filepath.Base(config.Name)
		if strings.Contains(exe, processName) || strings.Contains(cmdline, processName) {
			logrus.Infof("Killing existing process: %s (PID: %d)", config.Name, p.Pid)
			p.Kill()
		}
	}

	// Wait for the restart delay
	if config.RestartDelay > 0 {
		logrus.Infof("Waiting %d seconds before restart", config.RestartDelay)
		time.Sleep(time.Duration(config.RestartDelay) * time.Second)
	}

	// Start the process
	logrus.Infof("Starting process: %s", config.Name)
	return startProcess(config)
}

// monitorProcess monitors a process and restarts it if necessary
func monitorProcess(config ProcessConfig, ctx context.Context) {
	ticker := time.NewTicker(time.Duration(config.CheckInterval) * time.Second)
	defer ticker.Stop()

	var currentCmd *exec.Cmd

	// Start the process initially
	logrus.Infof("Starting initial process: %s", config.Name)
	cmd, err := startProcess(config)
	if err != nil {
		logrus.Errorf("Failed to start initial process %s: %v", config.Name, err)
	} else {
		currentCmd = cmd
	}

	for {
		select {
		case <-ticker.C:
			needRestart := false
			
			// Check process status
			running, _ := isProcessRunning(config.Name)
			if !running {
				logrus.Warnf("Process %s is not running", config.Name)
				needRestart = true
			}
			
			// Check ports if configured
			if !needRestart && len(config.Ports) > 0 {
				for _, port := range config.Ports {
					if !isPortInUse(port) {
						logrus.Warnf("Port %d is not in use for process %s", port, config.Name)
						needRestart = true
						break
					}
				}
			}
			
			// Check health checks if configured
			if !needRestart && len(config.HealthChecks) > 0 {
				for _, check := range config.HealthChecks {
					if !isHealthCheckOK(check) {
						logrus.Warnf("Health check failed for %s: %s", config.Name, check)
						needRestart = true
						break
					}
				}
			}

			// If process needs restart
			if needRestart {
				logrus.Warnf("Process %s failed health checks. Restarting...", config.Name)
				cmd, err := restartProcess(config)
				if err != nil {
					logrus.Errorf("Failed to restart process %s: %v", config.Name, err)
				} else {
					currentCmd = cmd
				}
			} else {
				logrus.Debugf("Process %s is healthy", config.Name)
			}

		case <-ctx.Done():
			if currentCmd != nil && currentCmd.Process != nil {
				logrus.Infof("Stopping process %s", config.Name)
				currentCmd.Process.Kill()
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

	// Set up logging
	logFile, err := os.OpenFile("processmonitor.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		logrus.Fatalf("Error opening log file: %v", err)
	}
	
	logrus.SetOutput(logFile)
	logrus.SetLevel(logrus.InfoLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	
	// Also log to console
	logrus.AddHook(&ConsoleHook{})

	logrus.Infof("Starting Process Monitor v1.0")
	logrus.Infof("Monitoring %d processes", len(config.Processes))

	// Set up context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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