# 注册表监控功能

ProcessMonitor 现在支持监控 Windows 注册表键值的变化，并在检测到变化时执行指定的命令。

## 功能特点

- 监控指定注册表键下的多个值
- 支持所有常见的注册表值类型（字符串、DWORD、QWORD、二进制等）
- 可以设置期望值，并检查实际值是否符合期望
- 可以在值变化时执行自定义命令
- 通过环境变量向命令传递变化的值和匹配状态

## 配置说明

在 `config.yaml` 文件中，使用 `registry_monitors` 部分配置注册表监控：

```yaml
registry_monitors:
  - name: "监控名称"
    root_key: "HKLM"                        # 根键 (HKLM, HKCU, HKCR, HKU, HKCC)
    path: "SOFTWARE\\Path\\To\\Key"         # 注册表键路径
    values:                                 # 要监控的值列表
      - name: "ValueName1"                  # 值名称
        type: "string"                      # 值类型
        expect_value: "ExpectedValue"       # 期望值（可选）
      - name: "ValueName2"
        type: "dword"
        expect_value: 1
    check_interval: 10                      # 检查间隔（秒）
    execute_on_change: true                 # 值变化时是否执行命令
    command: "program.exe"                  # 要执行的命令
    args: ["arg1", "arg2"]                  # 命令参数
    work_dir: "path/to/dir"                 # 工作目录（可选）
```

### 值类型

支持的值类型包括：
- `string`: 字符串
- `expand_string`: 可扩展字符串（包含环境变量引用）
- `binary`: 二进制数据
- `dword`: 32位整数
- `qword`: 64位整数
- `multi_string`: 多字符串

### 环境变量

当配置了命令执行时，以下环境变量会传递给命令：
- `CHANGED_VALUES`: 发生变化的值名称列表（逗号分隔）
- `EXPECT_VALUE_MATCH`: 是否所有变化的值都匹配期望值（true/false）

## 使用场景

1. **监控系统设置**：监控系统代理、防火墙、安全策略等关键设置的变化
2. **检测配置文件变化**：监控应用程序配置在注册表中的存储
3. **监控自启动项**：检测系统启动项的添加或修改
4. **监控环境变量**：检测系统环境变量的变化
5. **监控Windows自动登录配置**：检测自动登录设置的变化

## 测试注册表监控功能

项目包含一个 PowerShell 测试脚本 `test_registry_monitor.ps1`，可用于测试注册表监控功能：

1. 运行测试脚本：
   ```
   .\test_registry_monitor.ps1
   ```

2. 脚本会创建测试注册表项和配置文件，并提示你在另一个窗口中启动 ProcessMonitor

3. 按任意键后，脚本会修改注册表值，然后检查是否检测到变化

4. 测试完成后，脚本会清理测试环境

## 示例配置

### 监控系统代理设置

```yaml
registry_monitors:
  - name: "系统代理监控"
    root_key: "HKCU"
    path: "Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings"
    values:
      - name: "ProxyEnable"
        type: "dword"
        expect_value: 1
      - name: "ProxyServer"
        type: "string"
        expect_value: "127.0.0.1:8080"
    check_interval: 10
    execute_on_change: true
    command: "powershell.exe"
    args:
      - "-ExecutionPolicy"
      - "Bypass"
      - "-File"
      - "update_proxy.ps1"
```

### 监控Windows自动登录配置

```yaml
registry_monitors:
  - name: "Windows自动登录监控"
    root_key: "HKLM"
    path: "SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\\Winlogon"
    values:
      - name: "AutoAdminLogon"
        type: "string"
        expect_value: "1"
      - name: "DefaultUserName"
        type: "string"
      - name: "DefaultPassword"
        type: "string"
      - name: "DefaultDomainName"
        type: "string"
    check_interval: 60
    execute_on_change: true
    command: "powershell.exe"
    args:
      - "-ExecutionPolicy"
      - "Bypass"
      - "-Command"
      - "Send-MailMessage -To 'admin@example.com' -From 'system@example.com' -Subject 'Windows自动登录配置已更改' -Body ('检测到自动登录配置变更，变更的值: ' + $env:CHANGED_VALUES) -SmtpServer 'smtp.example.com'"
```

更多示例请参考 `config_example.yaml` 文件。