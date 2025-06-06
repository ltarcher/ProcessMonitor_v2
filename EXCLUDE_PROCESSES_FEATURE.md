# 进程排斥功能说明

## 功能概述

进程排斥功能允许您配置一个进程列表，当这些进程中的任何一个正在运行时，监控器将跳过启动或重启被监控的进程。这个功能对于避免在系统维护、部署或其他关键操作期间意外启动服务非常有用。

## 配置方式

在配置文件中为每个被监控的进程添加 `exclude_processes` 字段：

```yaml
processes:
  - name: "myapp.exe"
    args: []
    ports: [8080]
    health_checks: ["http://localhost:8080/health"]
    check_interval: 10
    restart_delay: 5
    kill_on_exit: false
    exclude_processes: ["backup.exe", "deploy.exe", "maintenance.exe"]
```

## 工作原理

1. **初始启动检查**: 当监控器启动时，会检查每个被监控进程的排斥列表
2. **重启检查**: 当检测到进程需要重启时，会再次检查排斥列表
3. **跳过操作**: 如果发现排斥进程正在运行，则跳过启动/重启操作
4. **日志记录**: 所有跳过操作都会在日志中记录原因

## 使用场景

### 1. 系统维护期间
```yaml
exclude_processes: ["backup.exe", "defrag.exe", "antivirus.exe"]
```
当系统备份、磁盘整理或杀毒软件运行时，避免启动可能影响性能的服务。

### 2. 部署和更新
```yaml
exclude_processes: ["deploy.exe", "update.exe", "migration.exe"]
```
在部署脚本、更新程序或数据库迁移运行时，防止服务重启。

### 3. 开发和调试
```yaml
exclude_processes: ["debugger.exe", "profiler.exe", "notepad.exe"]
```
当开发工具运行时，保持服务稳定以便调试。

### 4. 资源冲突避免
```yaml
exclude_processes: ["similar-service.exe", "test-version.exe"]
```
避免同类服务同时运行造成端口冲突。

## 日志输出示例

当排斥进程被检测到时，日志会显示类似信息：

```
2025/06/06 08:50:00 WARN Found exclude processes [backup.exe], skipping start of myapp.exe
2025/06/06 08:55:00 INFO Skipping restart of myapp.exe due to exclude processes
```

## 注意事项

1. **进程名匹配**: 系统会检查进程的可执行文件名和命令行参数
2. **大小写敏感**: 进程名匹配是大小写敏感的
3. **路径无关**: 只需要指定进程名，不需要完整路径
4. **实时检查**: 每次启动/重启前都会重新检查排斥进程状态
5. **空列表**: 如果 `exclude_processes` 为空或未配置，则不进行排斥检查

## 测试方法

1. 配置一个简单的排斥进程（如 `notepad.exe`）
2. 启动监控器
3. 打开记事本程序
4. 观察监控器是否跳过启动被监控的进程
5. 关闭记事本程序
6. 观察监控器是否恢复正常启动流程

## 配置示例

完整的配置示例请参考 `config_example.yaml` 文件中的示例6。