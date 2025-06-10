# 测试注册表监控功能的PowerShell脚本

# 创建测试注册表项
$testKeyPath = "HKCU:\Software\ProcessMonitorTest"
$testValueName = "TestValue"
$testValueType = "String"
$testValueData = "InitialValue"

# 创建测试配置文件
$configContent = @"
registry_monitors:
  - name: "测试注册表监控"
    root_key: "HKCU"
    path: "Software\\ProcessMonitorTest"
    values:
      - name: "$testValueName"
        type: "$testValueType"
        expect_value: "$testValueData"
    check_interval: 5
    execute_on_change: true
    command: "powershell.exe"
    args:
      - "-ExecutionPolicy"
      - "Bypass"
      - "-Command"
      - "Add-Content -Path 'registry_change_log.txt' -Value ('Registry value changed: ' + `$env:CHANGED_VALUES + ', Match expected: ' + `$env:EXPECT_VALUE_MATCH)"
"@

# 创建测试配置文件
$configContent | Out-File -FilePath "test_registry_config.yaml" -Encoding utf8

Write-Host "创建测试注册表项: $testKeyPath"
if (!(Test-Path $testKeyPath)) {
    New-Item -Path $testKeyPath -Force | Out-Null
}

Write-Host "设置初始值: $testValueName = $testValueData"
New-ItemProperty -Path $testKeyPath -Name $testValueName -Value $testValueData -PropertyType $testValueType -Force | Out-Null

# 清除之前的日志
if (Test-Path "registry_change_log.txt") {
    Remove-Item "registry_change_log.txt" -Force
}

Write-Host "启动注册表监控器..."
Write-Host "请在另一个PowerShell窗口中运行: .\ProcessMonitor.exe -config test_registry_config.yaml"
Write-Host "按任意键继续修改注册表值进行测试..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")

# 修改注册表值
$newValue = "ChangedValue"
Write-Host "修改注册表值: $testValueName = $newValue"
Set-ItemProperty -Path $testKeyPath -Name $testValueName -Value $newValue

Write-Host "等待5秒钟让监控器检测变化..."
Start-Sleep -Seconds 5

# 检查日志文件
if (Test-Path "registry_change_log.txt") {
    Write-Host "检测到注册表变化，日志内容:"
    Get-Content "registry_change_log.txt"
} else {
    Write-Host "未检测到注册表变化或命令未执行"
}

Write-Host "`n测试完成后，按任意键清理测试环境..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")

# 清理测试环境
Write-Host "删除测试注册表项..."
if (Test-Path $testKeyPath) {
    Remove-Item -Path $testKeyPath -Recurse -Force
}

Write-Host "测试完成，环境已清理"