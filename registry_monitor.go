package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows/registry"
)

// RegistryValueConfig 表示单个注册表值的监控配置
type RegistryValueConfig struct {
	Name        string      `yaml:"name"`         // 值名称
	Type        string      `yaml:"type"`         // 值类型 (string, dword, qword, binary, expand_string, multi_string)
	ExpectValue interface{} `yaml:"expect_value"` // 期望值
}

// RegistryMonitor represents the configuration for a registry key monitor
type RegistryMonitor struct {
	Name            string                `yaml:"name"`              // 监控名称
	RootKey         string                `yaml:"root_key"`          // 根键名称 (HKEY_LOCAL_MACHINE, HKEY_CURRENT_USER, etc.)
	Path            string                `yaml:"path"`              // 注册表路径
	Values          []RegistryValueConfig `yaml:"values"`            // 要监控的值配置
	CheckInterval   int                   `yaml:"check_interval"`    // 检查间隔（秒）
	ExecuteOnChange bool                  `yaml:"execute_on_change"` // 值变化时是否执行命令
	Command         string                `yaml:"command"`           // 值变化时执行的命令
	Args            []string              `yaml:"args"`              // 命令参数
	WorkDir         string                `yaml:"work_dir"`          // 工作目录
}

// getRegistryValueType 将字符串类型转换为 windows registry 值类型
func getRegistryValueType(typeName string) (uint32, error) {
	switch strings.ToLower(typeName) {
	case "string":
		return registry.SZ, nil
	case "expand_string":
		return registry.EXPAND_SZ, nil
	case "binary":
		return registry.BINARY, nil
	case "dword":
		return registry.DWORD, nil
	case "multi_string":
		return registry.MULTI_SZ, nil
	case "qword":
		return registry.QWORD, nil
	default:
		return 0, fmt.Errorf("unknown registry value type: %s", typeName)
	}
}

// compareValues 比较注册表值与期望值
func compareValues(actual interface{}, expect interface{}, valueType string) bool {
	// 如果没有设置期望值，则不进行比较
	if expect == nil {
		return true
	}

	// 根据值类型进行比较
	switch strings.ToLower(valueType) {
	case "string", "expand_string":
		actualStr, ok1 := actual.(string)
		expectStr, ok2 := expect.(string)
		if !ok1 || !ok2 {
			return false
		}
		return actualStr == expectStr

	case "dword", "qword":
		// 处理数值类型
		actualNum, ok1 := actual.(uint64)
		if !ok1 {
			if temp, ok := actual.(uint32); ok {
				actualNum = uint64(temp)
			} else {
				return false
			}
		}

		var expectNum uint64
		switch v := expect.(type) {
		case int:
			expectNum = uint64(v)
		case int64:
			expectNum = uint64(v)
		case uint:
			expectNum = uint64(v)
		case uint64:
			expectNum = v
		default:
			return false
		}
		return actualNum == expectNum

	case "binary":
		actualBytes, ok1 := actual.([]byte)
		expectBytes, ok2 := expect.([]byte)
		if !ok1 || !ok2 {
			return false
		}
		return bytes.Equal(actualBytes, expectBytes)

	case "multi_string":
		actualStrings, ok1 := actual.([]string)
		expectStrings, ok2 := expect.([]string)
		if !ok1 || !ok2 {
			return false
		}
		if len(actualStrings) != len(expectStrings) {
			return false
		}
		for i := range actualStrings {
			if actualStrings[i] != expectStrings[i] {
				return false
			}
		}
		return true

	default:
		return false
	}
}

// getRootKey 将字符串根键名称转换为 registry.Key
func getRootKey(rootKeyName string) (registry.Key, error) {
	switch rootKeyName {
	case "HKEY_CLASSES_ROOT", "HKCR":
		return registry.CLASSES_ROOT, nil
	case "HKEY_CURRENT_USER", "HKCU":
		return registry.CURRENT_USER, nil
	case "HKEY_LOCAL_MACHINE", "HKLM":
		return registry.LOCAL_MACHINE, nil
	case "HKEY_USERS", "HKU":
		return registry.USERS, nil
	case "HKEY_CURRENT_CONFIG", "HKCC":
		return registry.CURRENT_CONFIG, nil
	default:
		return 0, fmt.Errorf("unknown root key: %s", rootKeyName)
	}
}

// MonitorRegistry 监控注册表键值的变化
func MonitorRegistry(config RegistryMonitor, ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	logrus.Infof("Starting registry monitor for %s\\%s", config.RootKey, config.Path)

	// 获取根键
	rootKey, err := getRootKey(config.RootKey)
	if err != nil {
		logrus.Errorf("Invalid root key %s: %v", config.RootKey, err)
		return
	}

	// 初始值映射
	valueMap := make(map[string]interface{})
	valueTypeMap := make(map[string]string)

	// 初始化值映射
	k, err := registry.OpenKey(rootKey, config.Path, registry.QUERY_VALUE)
	if err != nil {
		logrus.Errorf("Failed to open registry key %s\\%s: %v", config.RootKey, config.Path, err)
		return
	}
	defer k.Close()

	// 读取初始值
	for _, valueConfig := range config.Values {
		// 获取期望的值类型
		expectedType, err := getRegistryValueType(valueConfig.Type)
		if err != nil {
			logrus.Errorf("Invalid value type for %s: %v", valueConfig.Name, err)
			continue
		}

		// 读取值和类型
		val, valType, err := k.GetValue(valueConfig.Name, nil)
		if err != nil {
			logrus.Warnf("Failed to read registry value %s: %v", valueConfig.Name, err)
			continue
		}

		// 检查类型是否匹配
		if uint32(valType) != expectedType {
			logrus.Warnf("Value type mismatch for %s: expected %d, got %d",
				valueConfig.Name, expectedType, valType)
			continue
		}
		valueMap[valueConfig.Name] = val
		valueTypeMap[valueConfig.Name] = valueConfig.Type
		logrus.Debugf("Initial registry value %s = %v", valueConfig.Name, val)
	}

	ticker := time.NewTicker(time.Duration(config.CheckInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 重新打开键以获取最新值
			k, err := registry.OpenKey(rootKey, config.Path, registry.QUERY_VALUE)
			if err != nil {
				logrus.Errorf("Failed to open registry key %s\\%s: %v", config.RootKey, config.Path, err)
				continue
			}

			changed := false
			changedValues := make([]string, 0)
			hasExpectValueMismatch := false

			// 检查每个值是否有变化
			for _, valueConfig := range config.Values {
				// 获取期望的值类型
				expectedType, err := getRegistryValueType(valueConfig.Type)
				if err != nil {
					logrus.Errorf("Invalid value type for %s: %v", valueConfig.Name, err)
					continue
				}

				// 读取值和类型
				val, valType, err := k.GetValue(valueConfig.Name, nil)
				if err != nil {
					logrus.Warnf("Failed to read registry value %s: %v", valueConfig.Name, err)
					continue
				}

				// 检查类型是否匹配
				if uint32(valType) != expectedType {
					logrus.Warnf("Value type mismatch for %s: expected %d, got %d",
						valueConfig.Name, expectedType, valType)
					continue
				}

				// 比较值
				oldVal, exists := valueMap[valueConfig.Name]
				if !exists || !compareValues(oldVal, val, valueConfig.Type) {
					logrus.Infof("Registry value changed: %s\\%s\\%s: %v -> %v",
						config.RootKey, config.Path, valueConfig.Name, oldVal, val)
					valueMap[valueConfig.Name] = val
					changed = true
					changedValues = append(changedValues, valueConfig.Name)

					// 检查是否与期望值匹配
					if valueConfig.ExpectValue != nil {
						if !compareValues(val, valueConfig.ExpectValue, valueConfig.Type) {
							logrus.Warnf("Value %s does not match expected value. Got: %v, Expected: %v",
								valueConfig.Name, val, valueConfig.ExpectValue)
							hasExpectValueMismatch = true
						} else {
							logrus.Infof("Value %s matches expected value: %v", valueConfig.Name, val)
						}
					}
				}
			}

			k.Close()

			// 如果有值变化且配置了执行命令的开关，则执行命令
			if changed && config.ExecuteOnChange && config.Command != "" {
				logrus.Infof("Executing command due to registry change: %s %v", config.Command, config.Args)

				// 创建命令
				cmd := exec.Command(config.Command, config.Args...)

				// 设置工作目录
				if config.WorkDir != "" {
					cmd.Dir = config.WorkDir
				}

				// 设置环境变量，传递变化的值名称和期望值匹配状态
				cmd.Env = append(os.Environ(),
					fmt.Sprintf("CHANGED_VALUES=%s", strings.Join(changedValues, ",")),
					fmt.Sprintf("EXPECT_VALUE_MATCH=%t", !hasExpectValueMismatch),
				)

				// 执行命令
				if err := cmd.Start(); err != nil {
					logrus.Errorf("Failed to execute command: %v", err)
				} else {
					// 不等待命令完成，让它在后台运行
					go func() {
						if err := cmd.Wait(); err != nil {
							logrus.Errorf("Command execution failed: %v", err)
						}
					}()
				}
			}

		case <-ctx.Done():
			logrus.Infof("Stopping registry monitor for %s\\%s", config.RootKey, config.Path)
			return
		}
	}
}
