package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows/registry"
)

// getRegistryTypeDescription 返回注册表值类型的字符串描述
func getRegistryTypeDescription(valType uint32) string {
	switch valType {
	case registry.NONE:
		return "NONE"
	case registry.SZ:
		return "SZ (String)"
	case registry.EXPAND_SZ:
		return "EXPAND_SZ (Expandable String)"
	case registry.BINARY:
		return "BINARY (Binary Data)"
	case registry.DWORD:
		return "DWORD (32-bit Number)"
	case registry.DWORD_BIG_ENDIAN:
		return "DWORD_BIG_ENDIAN (32-bit Big Endian)"
	case registry.LINK:
		return "LINK (Symbolic Link)"
	case registry.MULTI_SZ:
		return "MULTI_SZ (Multiple String)"
	case registry.RESOURCE_LIST:
		return "RESOURCE_LIST"
	case registry.FULL_RESOURCE_DESCRIPTOR:
		return "FULL_RESOURCE_DESCRIPTOR"
	case registry.RESOURCE_REQUIREMENTS_LIST:
		return "RESOURCE_REQUIREMENTS_LIST"
	case registry.QWORD:
		return "QWORD (64-bit Number)"
	default:
		return fmt.Sprintf("UNKNOWN (%d)", valType)
	}
}

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
		// 对于字符串类型，确保两边都是字符串类型再比较
		var actualStr, expectStr string

		// 处理实际值
		switch a := actual.(type) {
		case string:
			actualStr = a
		case []byte:
			actualStr = string(a)
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			// 如果是数字类型，转换为字符串
			actualStr = fmt.Sprintf("%d", a)
		case float32, float64:
			// 对于浮点数，保留小数部分
			actualStr = fmt.Sprintf("%v", a)
		default:
			// 其他类型，使用通用格式化
			actualStr = fmt.Sprintf("%v", a)
			logrus.Warnf("Unexpected actual value type for string comparison: %T", a)
		}

		// 处理期望值
		switch e := expect.(type) {
		case string:
			expectStr = e
		case []byte:
			expectStr = string(e)
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			// 如果是数字类型，转换为字符串
			expectStr = fmt.Sprintf("%d", e)
		case float32, float64:
			// 对于浮点数，保留小数部分
			expectStr = fmt.Sprintf("%v", e)
		default:
			// 其他类型，使用通用格式化
			expectStr = fmt.Sprintf("%v", e)
			logrus.Warnf("Unexpected expected value type for string comparison: %T", e)
		}

		// 增强日志输出
		logrus.Debugf("String comparison after conversion - Actual: '%s', Expected: '%s'", actualStr, expectStr)
		return actualStr == expectStr

	case "dword":
		// 转换actual为uint32
		actualNum, err := convertToUint32(actual)
		if err != nil {
			logrus.Warnf("Failed to convert actual value to uint32: %v", err)
			return false
		}
		// 转换expect为uint32
		expectNum, err := convertToUint32(expect)
		if err != nil {
			logrus.Warnf("Failed to convert expected value to uint32: %v", err)
			return false
		}
		return actualNum == expectNum

	case "qword":
		// 转换actual为uint64
		actualNum, err := convertToUint64(actual)
		if err != nil {
			logrus.Warnf("Failed to convert actual value to uint64: %v", err)
			return false
		}
		// 转换expect为uint64
		expectNum, err := convertToUint64(expect)
		if err != nil {
			logrus.Warnf("Failed to convert expected value to uint64: %v", err)
			return false
		}
		return actualNum == expectNum

	case "binary":
		// 处理二进制数据
		var actualBytes, expectBytes []byte
		var ok bool

		logrus.Debugf("Binary comparison - Converting actual value type: %T", actual)
		if actualBytes, ok = actual.([]byte); !ok {
			if str, ok := actual.(string); ok {
				actualBytes = []byte(str)
				logrus.Debugf("Converted actual string to bytes, length: %d", len(actualBytes))
			} else {
				logrus.Debugf("Failed to convert actual value to binary: %v (%T)", actual, actual)
				return false
			}
		}

		logrus.Debugf("Binary comparison - Converting expected value type: %T", expect)
		if expectBytes, ok = expect.([]byte); !ok {
			if str, ok := expect.(string); ok {
				expectBytes = []byte(str)
				logrus.Debugf("Converted expected string to bytes, length: %d", len(expectBytes))
			} else {
				logrus.Debugf("Failed to convert expected value to binary: %v (%T)", expect, expect)
				return false
			}
		}

		result := bytes.Equal(actualBytes, expectBytes)
		logrus.Debugf("Binary comparison result: %v (Actual length: %d, Expected length: %d)",
			result, len(actualBytes), len(expectBytes))
		return result

	case "multi_string":
		// 处理多字符串
		var actualStrings, expectStrings []string

		// 转换actual到字符串数组
		switch v := actual.(type) {
		case []string:
			actualStrings = v
		case string:
			actualStrings = []string{v}
		case []interface{}:
			actualStrings = make([]string, len(v))
			for i, item := range v {
				actualStrings[i] = fmt.Sprintf("%v", item)
			}
		default:
			return false
		}

		// 转换expect到字符串数组
		switch v := expect.(type) {
		case []string:
			expectStrings = v
		case string:
			expectStrings = []string{v}
		case []interface{}:
			expectStrings = make([]string, len(v))
			for i, item := range v {
				expectStrings[i] = fmt.Sprintf("%v", item)
			}
		default:
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

// convertToUint32 尝试将任意值转换为uint32
func convertToUint32(val interface{}) (uint32, error) {
	logrus.Debugf("Converting to uint32 - Input: %v (%T)", val, val)

	if val == nil {
		logrus.Debug("Cannot convert nil to uint32")
		return 0, fmt.Errorf("cannot convert nil to uint32")
	}

	switch v := val.(type) {
	case uint32:
		logrus.Debugf("Direct uint32 value: %d", v)
		return v, nil
	case int:
		logrus.Debugf("Converting from int: %d to uint32", v)
		return uint32(v), nil
	case int32:
		logrus.Debugf("Converting from int32: %d to uint32", v)
		return uint32(v), nil
	case int64:
		logrus.Debugf("Converting from int64: %d to uint32", v)
		return uint32(v), nil
	case uint:
		logrus.Debugf("Converting from uint: %d to uint32", v)
		return uint32(v), nil
	case uint64:
		logrus.Debugf("Converting from uint64: %d to uint32", v)
		return uint32(v), nil
	case float32:
		logrus.Debugf("Converting from float32: %f to uint32", v)
		return uint32(v), nil
	case float64:
		logrus.Debugf("Converting from float64: %f to uint32", v)
		return uint32(v), nil
	case string:
		logrus.Debugf("Converting from string: '%s' to uint32", v)
		var num uint64
		if _, err := fmt.Sscanf(v, "%d", &num); err == nil {
			result := uint32(num)
			logrus.Debugf("Successfully converted string to uint32: %d", result)
			return result, nil
		}
		logrus.Debugf("Failed to convert string '%s' to uint32", v)
		return 0, fmt.Errorf("cannot convert string '%s' to uint32", v)
	default:
		logrus.Debugf("Attempting to convert %T using reflection", val)
		// 尝试使用反射
		rv := reflect.ValueOf(val)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			result := uint32(rv.Int())
			logrus.Debugf("Converted from reflected integer: %d", result)
			return result, nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			result := uint32(rv.Uint())
			logrus.Debugf("Converted from reflected unsigned integer: %d", result)
			return result, nil
		case reflect.Float32, reflect.Float64:
			result := uint32(rv.Float())
			logrus.Debugf("Converted from reflected float: %d", result)
			return result, nil
		}
		logrus.Debugf("Failed to convert type %T to uint32", val)
		return 0, fmt.Errorf("cannot convert %T to uint32", val)
	}
}

// convertToUint64 尝试将任意值转换为uint64
func convertToUint64(val interface{}) (uint64, error) {
	logrus.Debugf("Converting to uint64 - Input: %v (%T)", val, val)

	if val == nil {
		logrus.Debug("Cannot convert nil to uint64")
		return 0, fmt.Errorf("cannot convert nil to uint64")
	}

	switch v := val.(type) {
	case uint64:
		logrus.Debugf("Direct uint64 value: %d", v)
		return v, nil
	case int:
		logrus.Debugf("Converting from int: %d to uint64", v)
		return uint64(v), nil
	case int32:
		logrus.Debugf("Converting from int32: %d to uint64", v)
		return uint64(v), nil
	case int64:
		logrus.Debugf("Converting from int64: %d to uint64", v)
		return uint64(v), nil
	case uint:
		logrus.Debugf("Converting from uint: %d to uint64", v)
		return uint64(v), nil
	case uint32:
		logrus.Debugf("Converting from uint32: %d to uint64", v)
		return uint64(v), nil
	case float32:
		logrus.Debugf("Converting from float32: %f to uint64", v)
		return uint64(v), nil
	case float64:
		logrus.Debugf("Converting from float64: %f to uint64", v)
		return uint64(v), nil
	case string:
		logrus.Debugf("Converting from string: '%s' to uint64", v)
		var num uint64
		if _, err := fmt.Sscanf(v, "%d", &num); err == nil {
			logrus.Debugf("Successfully converted string to uint64: %d", num)
			return num, nil
		}
		logrus.Debugf("Failed to convert string '%s' to uint64", v)
		return 0, fmt.Errorf("cannot convert string '%s' to uint64", v)
	default:
		logrus.Debugf("Attempting to convert %T using reflection", val)
		// 尝试使用反射
		rv := reflect.ValueOf(val)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			result := uint64(rv.Int())
			logrus.Debugf("Converted from reflected integer: %d", result)
			return result, nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			result := rv.Uint()
			logrus.Debugf("Converted from reflected unsigned integer: %d", result)
			return result, nil
		case reflect.Float32, reflect.Float64:
			result := uint64(rv.Float())
			logrus.Debugf("Converted from reflected float: %d", result)
			return result, nil
		}
		logrus.Debugf("Failed to convert type %T to uint64", val)
		return 0, fmt.Errorf("cannot convert %T to uint64", val)
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
		dwordValue, err := convertToUint32(value)
		if err != nil {
			return fmt.Errorf("failed to convert value to DWORD: %v", err)
		}
		return k.SetDWordValue(name, dwordValue)

	case "qword":
		qwordValue, err := convertToUint64(value)
		if err != nil {
			return fmt.Errorf("failed to convert value to QWORD: %v", err)
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

		// 根据配置的类型使用特定的读取方法，而不是通用的GetValue
		var val interface{}
		var valType uint32

		switch strings.ToLower(valueConfig.Type) {
		case "string":
			var strVal string
			strVal, _, err = k.GetStringValue(valueConfig.Name)
			if err == nil {
				val = strVal
				valType = registry.SZ
			}
		case "expand_string":
			var strVal string
			strVal, _, err = k.GetStringValue(valueConfig.Name)
			if err == nil {
				val = strVal
				valType = registry.EXPAND_SZ
			}
		case "dword":
			var dwordVal uint64
			dwordVal, _, err = k.GetIntegerValue(valueConfig.Name)
			if err == nil {
				val = uint32(dwordVal)
				valType = registry.DWORD
			}
		case "qword":
			// 通用GetValue，然后转换
			rawVal, rawType, rawErr := k.GetValue(valueConfig.Name, nil)
			if rawErr == nil && rawType == registry.QWORD {
				if qwordVal, convErr := convertToUint64(rawVal); convErr == nil {
					val = qwordVal
					valType = registry.QWORD
					err = nil
				} else {
					err = convErr
				}
			} else {
				err = rawErr
			}
		case "binary":
			var binVal []byte
			binVal, _, err = k.GetBinaryValue(valueConfig.Name)
			if err == nil {
				val = binVal
				valType = registry.BINARY
			}
		case "multi_string":
			var multiVal []string
			multiVal, _, err = k.GetStringsValue(valueConfig.Name)
			if err == nil {
				val = multiVal
				valType = registry.MULTI_SZ
			}
		default:
			// 对于未知类型，使用通用GetValue
			val, valType, err = k.GetValue(valueConfig.Name, nil)
		}

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
		typeMismatch := uint32(valType) != expectedType
		if typeMismatch {
			logrus.Warnf("Value type mismatch for %s: expected %d, got %d (value: %v)",
				valueConfig.Name, expectedType, valType, val)
		}

		// 根据类型处理值
		switch strings.ToLower(valueConfig.Type) {
		case "string", "expand_string":
			// 字符串类型统一转换为字符串格式
			strVal := fmt.Sprintf("%v", val)
			valueMap[valueConfig.Name] = strVal
			valueTypeMap[valueConfig.Name] = valueConfig.Type
			logrus.Infof("Initial registry value %s = %v (type: %s)", valueConfig.Name, strVal, valueConfig.Type)
			continue
		case "dword":
			// 使用convertToUint32处理DWORD类型
			num, err := convertToUint32(val)
			if err != nil {
				logrus.Warnf("Failed to convert DWORD value %s: %v", valueConfig.Name, err)
				continue
			}
			valueMap[valueConfig.Name] = num
			valueTypeMap[valueConfig.Name] = valueConfig.Type
			logrus.Infof("Initial registry value %s = %v (type: %s)", valueConfig.Name, num, valueConfig.Type)
			continue
		case "qword":
			// 使用convertToUint64处理QWORD类型
			num, err := convertToUint64(val)
			if err != nil {
				logrus.Warnf("Failed to convert QWORD value %s: %v", valueConfig.Name, err)
				continue
			}
			valueMap[valueConfig.Name] = num
			valueTypeMap[valueConfig.Name] = valueConfig.Type
			logrus.Infof("Initial registry value %s = %v (type: %s)", valueConfig.Name, num, valueConfig.Type)
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

				// 根据配置的类型使用特定的读取方法
				var val interface{}
				var valType uint32

				switch strings.ToLower(valueConfig.Type) {
				case "string":
					var strVal string
					strVal, _, err = k.GetStringValue(valueConfig.Name)
					if err == nil {
						val = strVal
						valType = registry.SZ
					}
				case "expand_string":
					var strVal string
					strVal, _, err = k.GetStringValue(valueConfig.Name)
					if err == nil {
						val = strVal
						valType = registry.EXPAND_SZ
					}
				case "dword":
					var dwordVal uint64
					dwordVal, _, err = k.GetIntegerValue(valueConfig.Name)
					if err == nil {
						val = uint32(dwordVal)
						valType = registry.DWORD
					}
				case "qword":
					// 通用GetValue，然后转换
					rawVal, rawType, rawErr := k.GetValue(valueConfig.Name, nil)
					if rawErr == nil && rawType == registry.QWORD {
						if qwordVal, convErr := convertToUint64(rawVal); convErr == nil {
							val = qwordVal
							valType = registry.QWORD
							err = nil
						} else {
							err = convErr
						}
					} else {
						err = rawErr
					}
				case "binary":
					var binVal []byte
					binVal, _, err = k.GetBinaryValue(valueConfig.Name)
					if err == nil {
						val = binVal
						valType = registry.BINARY
					}
				case "multi_string":
					var multiVal []string
					multiVal, _, err = k.GetStringsValue(valueConfig.Name)
					if err == nil {
						val = multiVal
						valType = registry.MULTI_SZ
					}
				default:
					// 对于未知类型，使用通用GetValue
					val, valType, err = k.GetValue(valueConfig.Name, nil)
				}

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
				typeMismatch := uint32(valType) != expectedType
				if typeMismatch {
					logrus.Warnf("Value type mismatch for %s: expected %d, got %d",
						valueConfig.Name, expectedType, valType)
				}

				// 比较值与期望值
				oldVal, exists := valueMap[valueConfig.Name]
				valueMismatch := !exists || !compareValues(oldVal, val, valueConfig.Type)

				// 增强日志输出
				logrus.Infof("Registry value check - Key: %s\\%s\\%s, Type: %s, Old: %v (%T), New: %v (%T), TypeMatch: %v, ValueMatch: %v",
					config.RootKey, config.Path, valueConfig.Name, valueConfig.Type,
					oldVal, oldVal, val, val, !typeMismatch, !valueMismatch)

				// 只要类型或值不匹配，就更新为期望值
				if valueConfig.ExpectValue != nil && (typeMismatch || valueMismatch) {
					hasExpectValueMismatch = true
					changed = true
					changedValues = append(changedValues, valueConfig.Name)

					logrus.Warnf("Value %s does not match expected (TypeMatch: %v, ValueMatch: %v). Got: %v (%T), Expected: %v (%T)",
						valueConfig.Name, !typeMismatch, !valueMismatch,
						val, val, valueConfig.ExpectValue, valueConfig.ExpectValue)

					// 立即恢复期望值，带重试机制
					var lastErr error
					for attempt := 1; attempt <= 3; attempt++ {
						k.Close()
						k, err = registry.OpenKey(rootKey, config.Path, registry.QUERY_VALUE|registry.SET_VALUE)
						if err != nil {
							lastErr = fmt.Errorf("failed to open key for writing (attempt %d): %v", attempt, err)
							logrus.Error(lastErr)
							time.Sleep(100 * time.Millisecond)
							continue
						}

						if err := setRegistryValue(k, valueConfig.Name, valueConfig.Type, valueConfig.ExpectValue); err != nil {
							lastErr = fmt.Errorf("failed to restore value (attempt %d): %v", attempt, err)
							logrus.Error(lastErr)
							k.Close()
							time.Sleep(100 * time.Millisecond)
							continue
						}

						// 验证恢复是否成功
						val, _, err := k.GetValue(valueConfig.Name, nil)
						if err == nil && !typeMismatch && compareValues(val, valueConfig.ExpectValue, valueConfig.Type) {
							valueMap[valueConfig.Name] = valueConfig.ExpectValue
							logrus.Infof("Successfully restored expected value for %s (attempt %d)", valueConfig.Name, attempt)
							lastErr = nil
							break
						}
					}

					if lastErr != nil {
						// 尝试使用ALL_ACCESS作为最后手段
						k.Close()
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
					k, err = registry.OpenKey(rootKey, config.Path, registry.QUERY_VALUE|registry.NOTIFY)
					if err != nil {
						logrus.Errorf("Failed to reopen registry key after writing: %v", err)
						continue
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
