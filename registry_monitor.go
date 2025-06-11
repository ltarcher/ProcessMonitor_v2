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
	Enable          bool                  `yaml:"enable"`            // 是否启用此监控配置（可选，默认为true）
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
	logrus.Debugf("Converting registry type string: %s", typeName)
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
	logrus.Debugf("Comparing values - Type: %s, Actual: %v (%T), Expected: %v (%T)",
		valueType, actual, actual, expect, expect)

	// 如果没有设置期望值，则不进行比较
	if expect == nil {
		return true
	}

	// 根据值类型进行比较
	switch strings.ToLower(valueType) {
	case "string", "expand_string":
		// 强制转换为字符串比较
		actualStr := fmt.Sprintf("%v", actual)
		expectStr := fmt.Sprintf("%v", expect)

		// 记录比较详情
		logrus.Infof("String comparison (forced) - Actual: %s, Expected: %s", actualStr, expectStr)
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

// setRegistryValue 根据类型设置注册表值
func setRegistryValue(k registry.Key, name string, valueType string, value interface{}) error {
	logrus.Debugf("Setting registry value - Name: %s, Type: %s, Value: %v (%T)",
		name, valueType, value, value)
	switch strings.ToLower(valueType) {
	case "string":
		strValue, ok := value.(string)
		if !ok {
			return fmt.Errorf("value for string type must be string, got %T", value)
		}
		return k.SetStringValue(name, strValue)

	case "expand_string":
		strValue, ok := value.(string)
		if !ok {
			return fmt.Errorf("value is not a string")
		}
		return k.SetExpandStringValue(name, strValue)

	case "binary":
		var byteValue []byte
		switch v := value.(type) {
		case []byte:
			byteValue = v
		case string:
			byteValue = []byte(v)
		default:
			return fmt.Errorf("value cannot be converted to binary")
		}
		return k.SetBinaryValue(name, byteValue)

	case "dword":
		var dwordValue uint32
		switch v := value.(type) {
		case int:
			dwordValue = uint32(v)
		case int64:
			dwordValue = uint32(v)
		case uint:
			dwordValue = uint32(v)
		case uint32:
			dwordValue = v
		case uint64:
			dwordValue = uint32(v)
		case float64:
			dwordValue = uint32(v)
		default:
			return fmt.Errorf("value cannot be converted to DWORD")
		}
		return k.SetDWordValue(name, dwordValue)

	case "qword":
		var qwordValue uint64
		switch v := value.(type) {
		case int:
			qwordValue = uint64(v)
		case int64:
			qwordValue = uint64(v)
		case uint:
			qwordValue = uint64(v)
		case uint32:
			qwordValue = uint64(v)
		case uint64:
			qwordValue = v
		case float64:
			qwordValue = uint64(v)
		default:
			return fmt.Errorf("value cannot be converted to QWORD")
		}
		return k.SetQWordValue(name, qwordValue)

	case "multi_string":
		var strValues []string
		switch v := value.(type) {
		case []string:
			strValues = v
		case string:
			strValues = []string{v}
		case []interface{}:
			strValues = make([]string, len(v))
			for i, item := range v {
				if str, ok := item.(string); ok {
					strValues[i] = str
				} else {
					return fmt.Errorf("multi_string array contains non-string value")
				}
			}
		default:
			return fmt.Errorf("value cannot be converted to multi-string")
		}
		return k.SetStringsValue(name, strValues)

	default:
		return fmt.Errorf("unsupported registry value type: %s", valueType)
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

	// 初始化值映射，添加写入权限
	k, err := registry.OpenKey(rootKey, config.Path, registry.QUERY_VALUE|registry.SET_VALUE)
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
		logrus.Debugf("Reading registry value: %s\\%s\\%s", config.RootKey, config.Path, valueConfig.Name)
		val, valType, err := k.GetValue(valueConfig.Name, nil)
		if err != nil {
			// 如果值不存在且有期望值，则设置期望值
			if err == registry.ErrNotExist && valueConfig.ExpectValue != nil {
				logrus.Infof("Value %s does not exist, setting expected value", valueConfig.Name)
				if setErr := setRegistryValue(k, valueConfig.Name, valueConfig.Type, valueConfig.ExpectValue); setErr != nil {
					logrus.Errorf("Failed to set expected value for %s: %v", valueConfig.Name, setErr)
					continue
				}
				valueMap[valueConfig.Name] = valueConfig.ExpectValue
				valueTypeMap[valueConfig.Name] = valueConfig.Type
				logrus.Infof("Successfully set expected value for %s", valueConfig.Name)
				continue
			}

			logrus.Warnf("Failed to read registry value %s: %v", valueConfig.Name, err)
			continue
		}

		// 检查类型是否匹配
		if uint32(valType) != expectedType {
			logrus.Warnf("Value type mismatch for %s: expected %d, got %d (value: %v)",
				valueConfig.Name, expectedType, valType, val)
			continue
		}
		valueMap[valueConfig.Name] = val
		valueTypeMap[valueConfig.Name] = valueConfig.Type
		logrus.Infof("Initial registry value %s = %v (type: %s)", valueConfig.Name, val, valueConfig.Type)
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
				logrus.Debugf("Attempting to read registry value %s with expected type %s", valueConfig.Name, valueConfig.Type)
				val, valType, err := k.GetValue(valueConfig.Name, nil)
				if err != nil {
					logrus.Debugf("Failed to read registry value %s: %v", valueConfig.Name, err)
					// 如果值不存在且有期望值，则设置期望值
					if err == registry.ErrNotExist && valueConfig.ExpectValue != nil {
						logrus.Infof("Value %s does not exist during monitoring, setting expected value", valueConfig.Name)
						k.Close() // 关闭只读句柄

						// 重新打开键以获取写入权限
						k, err = registry.OpenKey(rootKey, config.Path, registry.QUERY_VALUE|registry.SET_VALUE)
						if err != nil {
							logrus.Errorf("Failed to open registry key for writing: %v", err)
							continue
						}

						if setErr := setRegistryValue(k, valueConfig.Name, valueConfig.Type, valueConfig.ExpectValue); setErr != nil {
							logrus.Errorf("Failed to set expected value for %s: %v", valueConfig.Name, setErr)
							continue
						}

						// 重新打开键以恢复原来的访问权限
						k.Close()
						k, err = registry.OpenKey(rootKey, config.Path, registry.QUERY_VALUE|registry.NOTIFY)
						if err != nil {
							logrus.Errorf("Failed to reopen registry key after writing: %v", err)
							continue
						}

						valueMap[valueConfig.Name] = valueConfig.ExpectValue
						changed = true
						changedValues = append(changedValues, valueConfig.Name)
						logrus.Infof("Successfully set expected value for %s during monitoring", valueConfig.Name)
						continue
					}

					logrus.Warnf("Failed to read registry value %s: %v", valueConfig.Name, err)
					continue
				}

				// 检查类型是否匹配
				if uint32(valType) != expectedType {
					logrus.Warnf("Value type mismatch for %s: expected %d, got %d",
						valueConfig.Name, expectedType, valType)
					continue
				}
				logrus.Debugf("Raw registry value - Name: %s, Type: %d, Value: %v", valueConfig.Name, valType, val)

				// 比较值
				oldVal, exists := valueMap[valueConfig.Name]
				// 增强日志输出，显示值类型和内容
				logrus.Infof("Registry value changed - Key: %s\\%s\\%s, Type: %s, Old: %v (%T), New: %v (%T)",
					config.RootKey, config.Path, valueConfig.Name, valueConfig.Type,
					oldVal, oldVal, val, val)
				if !exists || !compareValues(oldVal, val, valueConfig.Type) {
					valueMap[valueConfig.Name] = val
					changed = true
					changedValues = append(changedValues, valueConfig.Name)

					// 检查是否与期望值匹配
					if valueConfig.ExpectValue != nil {
						if !compareValues(val, valueConfig.ExpectValue, valueConfig.Type) {
							logrus.Warnf("Value %s does not match expected value. Got: %v (%T), Expected: %v (%T)",
								valueConfig.Name, val, val, valueConfig.ExpectValue, valueConfig.ExpectValue)
							hasExpectValueMismatch = true

							// 立即恢复期望值，带重试机制
							var lastErr error
							for attempt := 1; attempt <= 3; attempt++ {
								k.Close()
								logrus.Debugf("Attempt %d: Opening key for writing with SET_VALUE permission", attempt)
								k, err = registry.OpenKey(rootKey, config.Path, registry.QUERY_VALUE|registry.SET_VALUE)
								if err != nil {
									lastErr = fmt.Errorf("failed to open key for writing (attempt %d): %v", attempt, err)
									logrus.Error(lastErr)
									time.Sleep(100 * time.Millisecond)
									continue
								}

								logrus.Debugf("Attempt %d: Restoring value %s to %v", attempt, valueConfig.Name, valueConfig.ExpectValue)
								if err := setRegistryValue(k, valueConfig.Name, valueConfig.Type, valueConfig.ExpectValue); err != nil {
									lastErr = fmt.Errorf("failed to restore value (attempt %d): %v", attempt, err)
									logrus.Error(lastErr)
									k.Close()
									time.Sleep(100 * time.Millisecond)
									continue
								}

								// 验证恢复是否成功
								val, _, err := k.GetValue(valueConfig.Name, nil)
								if err != nil || !compareValues(val, valueConfig.ExpectValue, valueConfig.Type) {
									lastErr = fmt.Errorf("verification failed after restore (attempt %d): %v", attempt, err)
									logrus.Error(lastErr)
									k.Close()
									time.Sleep(100 * time.Millisecond)
									continue
								}

								// 恢复成功
								valueMap[valueConfig.Name] = valueConfig.ExpectValue
								logrus.Infof("Successfully restored expected value for %s (attempt %d)", valueConfig.Name, attempt)
								lastErr = nil
								break
							}

							if lastErr != nil {
								// 尝试使用ALL_ACCESS作为最后手段
								k.Close()
								logrus.Warnf("Trying ALL_ACCESS as last resort")
								k, err = registry.OpenKey(rootKey, config.Path, registry.ALL_ACCESS)
								if err == nil {
									if err := setRegistryValue(k, valueConfig.Name, valueConfig.Type, valueConfig.ExpectValue); err == nil {
										valueMap[valueConfig.Name] = valueConfig.ExpectValue
										logrus.Infof("Successfully restored with ALL_ACCESS")
										lastErr = nil
									}
								}
							}

							k.Close()
							logrus.Debugf("Reopening key for monitoring")
							k, err = registry.OpenKey(rootKey, config.Path, registry.QUERY_VALUE|registry.NOTIFY)
							if err != nil {
								logrus.Errorf("Failed to reopen registry key after writing: %v", err)
								continue
							}
							logrus.Debugf("Successfully reopened key for monitoring")
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
