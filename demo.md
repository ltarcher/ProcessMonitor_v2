# 进程监控狗演示指南

## 演示环境准备

本项目包含以下文件：
- `processmonitor.exe` - 主监控程序
- `test_app.exe` - 测试应用程序（监听8080端口）
- `config.yaml` - 监控配置文件
- `monitor_watchdog.bat` - 看门狗脚本

## 演示步骤

### 1. 启动测试应用
```bash
# 启动测试应用（会监听8080端口）
.\test_app.exe
```

测试应用提供以下端点：
- `http://localhost:8080/` - 主页
- `http://localhost:8080/health` - 健康检查
- `http://localhost:8080/status` - 状态检查

### 2. 启动进程监控
```bash
# 在新的终端窗口中启动监控程序
.\processmonitor.exe -config config.yaml
```

监控程序会：
- 每10秒检查一次 `test_app.exe` 进程
- 检查8080端口是否可用
- 检查健康检查端点 `http://localhost:8080/health`

### 3. 测试自动重启功能

#### 方法1：手动终止进程
1. 在任务管理器中找到 `test_app.exe` 进程并结束它
2. 观察监控程序的日志输出
3. 监控程序会自动检测到进程停止并重新启动它

#### 方法2：使用命令行终止
```bash
# 查找并终止test_app进程
taskkill /f /im test_app.exe
```

### 4. 测试看门狗功能
```bash
# 启动看门狗脚本（会监控监控程序本身）
.\monitor_watchdog.bat
```

看门狗脚本会：
- 每30秒检查一次监控程序是否运行
- 如果监控程序停止，自动重启它

## 预期行为

### 正常运行时
```
time="2025-06-05T14:31:17+08:00" level=info msg="Starting Process Monitor v1.0"
time="2025-06-05T14:31:17+08:00" level=info msg="Monitoring 1 processes"
time="2025-06-05T14:31:17+08:00" level=info msg="Starting initial process: test_app.exe"
time="2025-06-05T14:31:27+08:00" level=debug msg="Process test_app.exe is healthy"
```

### 检测到异常时
```
time="2025-06-05T14:31:37+08:00" level=warning msg="Process test_app.exe is not running"
time="2025-06-05T14:31:37+08:00" level=warning msg="Process test_app.exe failed health checks. Restarting..."
time="2025-06-05T14:31:37+08:00" level=info msg="Killing existing process: test_app.exe"
time="2025-06-05T14:31:42+08:00" level=info msg="Starting process: test_app.exe"
```

## 配置说明

当前配置监控 `test_app.exe`：
- **检查间隔**: 10秒
- **重启延迟**: 5秒
- **端口监控**: 8080
- **健康检查**: `http://localhost:8080/health`

## 故障排除

### 常见问题

1. **端口被占用**
   ```bash
   # 检查8080端口使用情况
   netstat -ano | findstr :8080
   ```

2. **权限问题**
   - 确保以管理员权限运行监控程序
   - 检查防火墙设置

3. **网络连接问题**
   ```bash
   # 测试健康检查端点
   curl http://localhost:8080/health
   ```

## 自定义配置

修改 `config.yaml` 来监控你自己的应用：

```yaml
processes:
  - name: "your-app.exe"
    args: ["-config", "app.conf"]
    ports: [8080, 8081]
    health_checks: 
      - "http://localhost:8080/health"
      - "https://localhost:8081/status"
    check_interval: 30
    restart_delay: 10
```

## 生产环境部署

1. **编译发布版本**
   ```bash
   go build -ldflags "-s -w" -o processmonitor.exe main.go
   ```

2. **创建Windows服务**
   - 使用 `sc create` 命令或第三方工具如 NSSM
   - 确保服务以适当权限运行

3. **配置日志轮转**
   - 考虑使用日志管理工具
   - 定期清理旧日志文件

4. **监控告警**
   - 集成监控系统（如Prometheus）
   - 配置邮件或短信告警