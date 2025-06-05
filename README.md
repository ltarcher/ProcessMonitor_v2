# 进程监控狗 (Process Monitor)

一个用Go语言实现的跨平台进程监控工具，支持Windows和Linux系统。该工具可以监控指定进程的运行状态，并在进程异常时自动重启。

## 功能特性

- **多种监控方式**：支持进程名监控、端口监控、HTTP/HTTPS健康检查
- **自动重启**：检测到进程异常时自动重启目标进程
- **跨平台支持**：同时支持Windows和Linux系统
- **配置灵活**：通过YAML配置文件管理监控规则
- **自我保护**：提供看门狗脚本确保监控进程本身的可靠性
- **详细日志**：完整的运行日志记录

## 安装和使用

### 1. 编译项目

```bash
# 下载依赖
go mod download

# 编译
go build -o processmonitor main.go

# 或者直接运行
go run main.go -config config.yaml
```

### 2. 配置文件

创建 `config.yaml` 配置文件：

```yaml
processes:
  - name: "your-app.exe"                    # 进程名或可执行文件路径
    args: ["-port", "8080"]                 # 启动参数
    ports: [8080, 8081]                     # 监控的端口列表
    health_checks:                          # HTTP健康检查URL列表
      - "http://localhost:8080/health"
      - "https://localhost:8081/status"
    check_interval: 30                      # 检查间隔（秒）
    restart_delay: 5                        # 重启延迟（秒）
  
  - name: "another-service"
    args: []
    ports: [3000]
    health_checks: ["http://localhost:3000/ping"]
    check_interval: 60
    restart_delay: 10
```

### 3. 运行监控

```bash
# 基本运行
./processmonitor -config config.yaml

# 创建看门狗脚本（用于监控监控进程本身）
./processmonitor -create-watchdog
```

### 4. Windows服务部署

将监控狗注册为Windows系统服务，实现开机自启动：

```bash
# 安装为Windows服务（需要管理员权限）
install_service.bat

# 管理服务
service_manager.bat

# 卸载服务
uninstall_service.bat
```

### 5. 看门狗脚本

生成的看门狗脚本会监控监控进程本身：

- **Windows**: `monitor_watchdog.bat`
- **Linux**: `monitor_watchdog.sh`

运行看门狗脚本以确保监控进程始终运行：

```bash
# Windows
monitor_watchdog.bat

# Linux
chmod +x monitor_watchdog.sh
./monitor_watchdog.sh
```

## 监控机制

### 1. 进程监控
- 检查指定名称的进程是否在运行
- 支持完整路径和进程名匹配

### 2. 端口监控
- 检查指定端口是否被占用
- 支持TCP端口检测

### 3. 健康检查
- 发送HTTP/HTTPS请求到指定URL
- 检查响应状态码是否为200

### 4. 自动重启流程
1. 检测到异常时，先终止现有进程
2. 等待指定的重启延迟时间
3. 使用配置的参数重新启动进程
4. 记录详细的操作日志

## 配置参数说明

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 进程名或可执行文件路径 |
| `args` | []string | 否 | 进程启动参数 |
| `ports` | []int | 否 | 需要监控的端口列表 |
| `health_checks` | []string | 否 | HTTP健康检查URL列表 |
| `check_interval` | int | 否 | 检查间隔秒数（默认30秒） |
| `restart_delay` | int | 否 | 重启前等待秒数（默认5秒） |
| `kill_on_exit` | bool | 否 | 监控狗退出时是否杀死被监控进程（默认false） |

## 日志功能

### 日志级别
程序使用结构化日志，包含以下级别：
- **Info**: 正常运行信息
- **Warn**: 检测到异常情况
- **Error**: 错误信息
- **Debug**: 详细调试信息

### 日志轮转和备份
- **文件大小限制**: 100MB自动轮转
- **备份命名**: `processmonitor.log.2025-06-05_16-12-35`
- **定期清理**: 每月自动删除超过1个月的日志文件
- **双重输出**: 同时输出到控制台和文件
- **自动管理**: 无需手动维护日志文件

### 日志文件管理
```bash
# 查看当前日志
type processmonitor.log

# 查看所有日志文件
dir processmonitor.log*

# 日志会自动轮转，无需手动管理
```

## 系统要求

- Go 1.21.0 或更高版本
- Windows 10+ 或 Linux (任何现代发行版)
- 网络访问权限（用于HTTP健康检查）

## 依赖包

- `github.com/shirou/gopsutil/v3` - 系统进程信息获取
- `github.com/sirupsen/logrus` - 结构化日志
- `gopkg.in/yaml.v2` - YAML配置解析

## 使用示例

### 监控Web服务

```yaml
processes:
  - name: "nginx.exe"
    ports: [80, 443]
    health_checks: ["http://localhost/health"]
    check_interval: 30
    restart_delay: 5
    kill_on_exit: true    # 监控狗退出时杀死nginx
```

### 监控数据库服务

```yaml
processes:
  - name: "mysqld"
    ports: [3306]
    check_interval: 60
    restart_delay: 10
    kill_on_exit: false   # 监控狗退出时保留数据库进程
```

### 监控自定义应用

```yaml
processes:
  - name: "./myapp"
    args: ["-config", "app.conf", "-port", "8080"]
    ports: [8080]
    health_checks: ["http://localhost:8080/api/health"]
    check_interval: 15
    restart_delay: 3
    kill_on_exit: false   # 监控狗退出时保留应用进程
```

## Windows服务部署

### 服务安装
```bash
# 以管理员身份运行
install_service.bat
```

### 服务管理
```bash
# 启动/停止/重启服务
service_manager.bat

# 或使用命令行
sc start ProcessMonitor
sc stop ProcessMonitor
sc query ProcessMonitor
```

### 服务特性
- **自动启动**：开机自动启动
- **故障恢复**：服务失败时自动重启
- **后台运行**：无需用户登录即可运行
- **系统集成**：完全集成到Windows服务管理

详细部署指南请参考：[`deploy_guide.md`](deploy_guide.md:1)

## 注意事项

1. 确保监控进程有足够权限启动和终止目标进程
2. 健康检查URL应该返回HTTP 200状态码
3. 合理设置检查间隔，避免过于频繁的检查
4. **生产环境推荐**：使用Windows服务部署，确保高可用性
5. **开发测试环境**：可使用看门狗脚本进行简单部署
6. **重要**：`kill_on_exit` 参数控制监控狗退出时的行为：
   - `true`：监控狗退出时会杀死被监控的进程
   - `false`：监控狗退出时保留被监控的进程继续运行
   - 对于数据库等重要服务，建议设置为 `false`

## 故障排除

### 常见问题

1. **进程无法启动**
   - 检查进程路径是否正确
   - 确认进程有执行权限
   - 查看日志中的错误信息

2. **端口检查失败**
   - 确认端口号配置正确
   - 检查防火墙设置
   - 验证进程是否真的监听了指定端口

3. **健康检查失败**
   - 确认URL可访问
   - 检查网络连接
   - 验证服务是否正确响应

## 许可证

MIT License