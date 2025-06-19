package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows/registry"
)

func TestGetRegistryValueType(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    uint32
		wantErr bool
	}{
		{"string", "string", registry.SZ, false},
		{"expand_string", "expand_string", registry.EXPAND_SZ, false},
		{"binary", "binary", registry.BINARY, false},
		{"dword", "dword", registry.DWORD, false},
		{"qword", "qword", registry.QWORD, false},
		{"multi_string", "multi_string", registry.MULTI_SZ, false},
		{"unknown", "unknown", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getRegistryValueType(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("getRegistryValueType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getRegistryValueType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompareValues(t *testing.T) {
	tests := []struct {
		name      string
		actual    interface{}
		expect    interface{}
		valueType string
		want      bool
	}{
		{"string match", "hello", "hello", "string", true},
		{"string mismatch", "hello", "world", "string", false},
		{"dword match", uint32(42), 42, "dword", true},
		{"dword mismatch", uint32(42), 43, "dword", false},
		{"binary match", []byte{1, 2, 3}, []byte{1, 2, 3}, "binary", true},
		{"binary mismatch", []byte{1, 2, 3}, []byte{4, 5, 6}, "binary", false},
		{"multi_string match", []string{"a", "b"}, []string{"a", "b"}, "multi_string", true},
		{"multi_string mismatch", []string{"a", "b"}, []string{"a", "c"}, "multi_string", false},
		{"nil expect", "anything", nil, "string", true},
		// 新增测试用例，测试字符串与不同类型的比较
		{"string vs int match", "90", 90, "string", true},
		{"string vs int mismatch", "91", 90, "string", false},
		{"string vs float match", "90", 90.0, "string", true},
		{"string vs float with decimal", "90.5", 90.5, "string", true},
		{"string vs uint32 match", "90", uint32(90), "string", true},
		{"string vs uint64 match", "90", uint64(90), "string", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compareValues(tt.actual, tt.expect, tt.valueType); got != tt.want {
				t.Errorf("compareValues() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetRootKey(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    registry.Key
		wantErr bool
	}{
		{"HKCR", "HKCR", registry.CLASSES_ROOT, false},
		{"HKCU", "HKCU", registry.CURRENT_USER, false},
		{"HKLM", "HKLM", registry.LOCAL_MACHINE, false},
		{"HKU", "HKU", registry.USERS, false},
		{"HKCC", "HKCC", registry.CURRENT_CONFIG, false},
		{"unknown", "unknown", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getRootKey(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("getRootKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getRootKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetRegistryValue(t *testing.T) {
	// 使用临时注册表键进行测试
	key, cleanup := createTestKey(t)
	defer cleanup()

	tests := []struct {
		name      string
		valueName string
		valueType string
		value     interface{}
		wantErr   bool
	}{
		{"regular string", "testString", "string", "testValue", false},
		{"empty string", "testEmptyString", "string", "", false},
		{"long string", "testLongString", "string", strings.Repeat("a", 1024), false},
		{"expand string", "testExpandString", "expand_string", "%PATH%", false},
		{"dword", "testDword", "dword", uint32(42), false},
		{"dword max", "testDwordMax", "dword", uint32(0xFFFFFFFF), false},
		{"qword", "testQword", "qword", uint64(1<<63 - 1), false},
		{"binary", "testBinary", "binary", []byte{1, 2, 3}, false},
		{"empty binary", "testEmptyBinary", "binary", []byte{}, false},
		{"multi string", "testMultiString", "multi_string", []string{"first", "second", "third"}, false},
		{"empty multi string", "testEmptyMultiString", "multi_string", []string{}, false},
		{"invalid type", "testInvalid", "invalid", "value", true},
		{"nil value", "testNilValue", "string", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := setRegistryValue(key, tt.valueName, tt.valueType, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("setRegistryValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// 比较值，根据类型使用不同的比较方式
				switch tt.valueType {
				case "string":
					got, _, err := key.GetStringValue(tt.valueName)
					if err != nil {
						t.Errorf("GetStringValue() error = %v", err)
						return
					}
					if got != tt.value.(string) {
						t.Errorf("string value not set correctly, got %q, want %q", got, tt.value.(string))
					}
				case "dword":
					got, _, err := key.GetIntegerValue(tt.valueName)
					if err != nil || got != uint64(tt.value.(uint32)) {
						t.Errorf("dword value not set correctly, got %v, want %v", got, tt.value)
					}
				case "binary":
					got, _, err := key.GetBinaryValue(tt.valueName)
					if err != nil || !bytes.Equal(got, tt.value.([]byte)) {
						t.Errorf("binary value not set correctly, got %v, want %v", got, tt.value)
					}
				}

				// 验证类型
				_, valType, err := key.GetValue(tt.valueName, nil)
				if err != nil {
					t.Errorf("GetValue() error = %v", err)
					return
				}
				expectedType, _ := getRegistryValueType(tt.valueType)
				if valType != expectedType {
					t.Errorf("value type not set correctly, got %d, want %d", valType, expectedType)
				}
			}
		})
	}
}

func TestMonitorRegistry(t *testing.T) {
	// 设置日志级别为Debug，以便查看详细日志
	logrus.SetLevel(logrus.DebugLevel)

	// 创建测试键
	key, cleanup := createTestKey(t)
	defer cleanup()

	// 设置初始值
	keyPath := "SOFTWARE\\TestRegistryMonitor" // 使用与测试键一致的路径
	rootKey := "HKCU"                          // 使用与代码一致的格式
	initialValue := "initial"

	// 设置初始值
	logrus.Debugf("Setting initial registry value to: %s", initialValue)
	if err := key.SetStringValue("testValue", initialValue); err != nil {
		t.Fatalf("failed to set initial value: %v", err)
	}

	// 验证初始值设置成功
	var actualValue string
	var err error
	actualValue, _, err = key.GetStringValue("testValue")
	if err != nil {
		t.Fatalf("failed to read initial value: %v", err)
	}
	logrus.Debugf("Initial registry value read back: %s", actualValue)
	if actualValue != initialValue {
		t.Fatalf("initial value not set correctly, got %q want %q", actualValue, initialValue)
	}

	// 准备测试配置
	config := RegistryMonitor{
		Name:          "testMonitor",
		RootKey:       rootKey,
		Path:          keyPath,
		CheckInterval: 1,
		Values: []RegistryValueConfig{
			{
				Name:        "testValue",
				Type:        "string",
				ExpectValue: initialValue,
			},
		},
	}

	// 打印调试信息
	logrus.Debugf("Monitor configuration: %+v", config)
	logrus.Debugf("Initial value set to: %s", initialValue)

	// 设置上下文和等待组
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)

	// 启动监控
	go MonitorRegistry(config, ctx, &wg)

	// 等待监控启动
	time.Sleep(500 * time.Millisecond)

	// 验证初始值设置是否成功
	initialVal, _, err := key.GetStringValue("testValue")
	if err != nil {
		t.Fatalf("failed to get initial value: %v", err)
	}
	logrus.Debugf("Initial value verification: %s", initialVal)
	if initialVal != initialValue {
		t.Fatalf("initial value not set correctly, got %q want %q", initialVal, initialValue)
	}

	// 修改注册表值
	modifiedValue := "modified"
	logrus.Debugf("Modifying value to: %s", modifiedValue)
	if err := key.SetStringValue("testValue", modifiedValue); err != nil {
		t.Fatalf("failed to modify test value: %v", err)
	}

	// 验证值是否被成功修改
	modifiedVal, _, err := key.GetStringValue("testValue")
	if err != nil {
		t.Fatalf("failed to get modified value: %v", err)
	}
	logrus.Debugf("Modified value verification: %s", modifiedVal)
	if modifiedVal != modifiedValue {
		t.Fatalf("value not modified correctly, got %q want %q", modifiedVal, modifiedValue)
	}

	// 等待监控检测到变化并恢复值
	logrus.Debug("Waiting for monitor to detect and restore the value...")
	time.Sleep(2 * time.Second)

	// 检查中间状态
	midVal, _, err := key.GetStringValue("testValue")
	if err != nil {
		t.Fatalf("failed to get intermediate value: %v", err)
	}
	logrus.Debugf("Intermediate value check: %s", midVal)

	// 继续等待完全恢复
	time.Sleep(8 * time.Second)

	// 停止监控
	logrus.Debug("Stopping monitor...")
	cancel()
	wg.Wait()

	// 验证最终值
	finalVal, _, err := key.GetStringValue("testValue")
	if err != nil {
		t.Fatalf("failed to get final value: %v", err)
	}
	logrus.Debugf("Final value verification: %s", finalVal)

	if finalVal != initialValue {
		t.Errorf("value not restored to expected, got %q want %q", finalVal, initialValue)
		// 打印更多诊断信息
		logrus.WithFields(logrus.Fields{
			"initial_value":  initialValue,
			"modified_value": modifiedValue,
			"final_value":    finalVal,
			"config":         config,
		}).Error("Value restoration failed")
	}
}

// createTestKey 创建一个用于测试的临时注册表键
func createTestKey(t *testing.T) (registry.Key, func()) {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, "SOFTWARE\\TestRegistryMonitor", registry.ALL_ACCESS)
	if err != nil {
		t.Fatalf("failed to create test key: %v", err)
	}

	cleanup := func() {
		key.Close()
		registry.DeleteKey(registry.CURRENT_USER, "SOFTWARE\\TestRegistryMonitor")
	}

	return key, cleanup
}
