# Windows服务部署指南

## 概述

本指南介绍如何将进程监控狗部署为Windows系统服务，实现开机自启动和后台运行。

## 部署脚本

### 1. 服务安装脚本 - `install_service.bat`
- **功能**：将进程监控狗注册为Windows系统服务
- **权限**：需要管理员权限运行
- **配置**：自动配置服务启动类型为自动启动
- **恢复**：配置服务失败时自动重启

### 2. 服务卸载脚本 - `uninstall_service.bat`
- **功能**：从系统中移除进程监控狗服务
- **权限**：需要管理员权限运行
- **清理**：完全清理服务注册信息

### 3. 服务管理脚本 - `service_manager.bat`
- **功能**：提供服务管理的图形化菜单
- **操作**：启动、停止、重启、查看状态等
- **日志**：快速访问事件查看器

## 部署步骤

### 第一步：准备文件
确保以下文件在同一目录中：
```
processmonitor.exe          # 主程序
config.yaml                 # 配置文件
install_service.bat         # 安装脚本
uninstall_service.bat       # 卸载脚本
service_manager.bat         # 管理脚本
```

### 第二步：配置监控规则
编辑 `config.yaml` 文件，配置需要监控的进程：
```yaml
processes:
  - name: "your-app.exe"
    args: []
    ports: [8080]
    health_checks: ["http://localhost:8080/health"]
    check_interval: 30
    restart_delay: 5
    kill_on_exit: false
```

### 第三步：安装服务
1. 右键点击 `install_service.bat`
2. 选择"以管理员身份运行"
3. 等待安装完成

### 第四步：验证安装
使用以下方法验证服务安装成功：

#### 方法1：使用管理脚本
1. 右键点击 `service_manager.bat`
2. 选择"以管理员身份运行"
3. 选择选项1检查服务状态

#### 方法2：使用Windows服务管理器
1. 按 `Win + R`，输入 `services.msc`
2. 查找"Process Monitor Service"
3. 确认服务状态为"正在运行"

#### 方法3：使用命令行
```cmd
sc query ProcessMonitor
```

## 服务配置详情

### 服务信息
- **服务名称**：ProcessMonitor
- **显示名称**：Process Monitor Service
- **描述**：Monitors and restarts configured processes automatically
- **启动类型**：自动
- **恢复选项**：失败时自动重启

### 服务路径
```
"C:\path\to\processmonitor.exe" -config "C:\path\to\config.yaml"
```

### 恢复配置
- **第一次失败**：5秒后重启
- **第二次失败**：10秒后重启
- **后续失败**：30秒后重启
- **重置计数器**：24小时

## 服务管理

### 启动服务
```cmd
# 命令行方式
sc start ProcessMonitor

# 或使用管理脚本
service_manager.bat -> 选项2
```

### 停止服务
```cmd
# 命令行方式
sc stop ProcessMonitor

# 或使用管理脚本
service_manager.bat -> 选项3
```

### 重启服务
```cmd
# 使用管理脚本
service_manager.bat -> 选项4
```

### 查看服务状态
```cmd
# 命令行方式
sc query ProcessMonitor

# 或使用管理脚本
service_manager.bat -> 选项1
```

## 日志管理

### 服务日志位置
- **应用程序日志**：`processmonitor.log`（程序目录）
- **系统事件日志**：Windows事件查看器

### 查看系统事件日志
1. 运行 `service_manager.bat`
2. 选择选项6打开事件查看器
3. 查看以下位置：
   - Windows日志 > 应用程序
   - Windows日志 > 系统

### 常见日志事件
- **服务启动**：事件ID 7036
- **服务停止**：事件ID 7036
- **服务失败**：事件ID 7034
- **应用程序错误**：查看应用程序日志

## 故障排除

### 安装失败
**问题**：服务安装失败
**解决方案**：
1. 确保以管理员权限运行
2. 检查文件路径是否正确
3. 确保processmonitor.exe和config.yaml存在

### 服务无法启动
**问题**：服务安装成功但无法启动
**解决方案**：
1. 检查config.yaml语法是否正确
2. 确保被监控的程序路径正确
3. 查看Windows事件日志获取详细错误信息

### 权限问题
**问题**：服务运行时权限不足
**解决方案**：
1. 在服务属性中配置"登录"选项
2. 使用具有足够权限的账户运行服务
3. 确保服务账户对程序目录有读写权限

### 配置更新
**问题**：需要更新监控配置
**解决方案**：
1. 修改config.yaml文件
2. 重启服务使配置生效：
   ```cmd
   sc stop ProcessMonitor
   sc start ProcessMonitor
   ```

## 卸载服务

### 完全卸载步骤
1. 停止服务（如果正在运行）
2. 右键点击 `uninstall_service.bat`
3. 选择"以管理员身份运行"
4. 等待卸载完成

### 手动卸载（备用方法）
```cmd
# 停止服务
sc stop ProcessMonitor

# 删除服务
sc delete ProcessMonitor
```

## 最佳实践

### 1. 部署前测试
在注册为服务前，先手动运行程序确保配置正确：
```cmd
processmonitor.exe -config config.yaml
```

### 2. 备份配置
定期备份config.yaml和重要的日志文件。

### 3. 监控服务状态
定期检查服务运行状态，可以通过：
- Windows服务管理器
- 事件查看器
- 自定义监控脚本

### 4. 日志轮转
程序已内置日志轮转功能（100MB限制），无需额外配置。

### 5. 安全考虑
- 使用专用服务账户运行
- 限制配置文件的访问权限
- 定期更新程序版本

## 高级配置

### 自定义服务账户
如需使用特定账户运行服务：
1. 打开services.msc
2. 找到"Process Monitor Service"
3. 右键 > 属性 > 登录
4. 选择"此账户"并输入账户信息

### 依赖服务配置
如果监控的程序依赖其他服务，可以配置服务依赖：
```cmd
sc config ProcessMonitor depend= "Dependency1/Dependency2"
```

### 服务优先级
调整服务启动优先级：
```cmd
sc config ProcessMonitor start= delayed-auto
```

## 支持和维护

### 定期维护任务
- 检查日志文件大小和轮转情况
- 验证被监控进程的运行状态
- 更新配置文件（如需要）
- 检查Windows事件日志中的错误

### 升级程序
1. 停止服务
2. 替换processmonitor.exe
3. 启动服务
4. 验证运行状态

通过以上部署指南，您可以将进程监控狗成功部署为Windows系统服务，实现稳定可靠的进程监控功能。