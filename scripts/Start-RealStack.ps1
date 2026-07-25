param(
    [string]$Password = '1234',
    [ValidateRange(1, 64)]
    [int]$MaxMemoryGB = 8,
    [ValidateRange(1, 32)]
    [int]$Players = 4
)

$ErrorActionPreference = 'Stop'
$projectDir = Split-Path -Parent $PSScriptRoot
$agent = Join-Path $projectDir 'native_agent.py'
$compose = Join-Path $projectDir 'compose.real.yml'
$runtimeDir = 'D:\PalworldServer\logs'
$headers = @{ 'X-Pal-Control-Token' = $Password }

$listener = Get-NetTCPConnection -LocalPort 8213 -State Listen -ErrorAction SilentlyContinue
if (-not $listener) {
    [IO.Directory]::CreateDirectory($runtimeDir) | Out-Null
    $env:PALWORLD_CONTROL_TOKEN = $Password
    $env:PALWORLD_ADMIN_PASSWORD = $Password
    $env:PALWORLD_MAX_MEMORY_GB = [string]$MaxMemoryGB
    $env:PALWORLD_MAX_PLAYERS = [string]$Players
    $python = (python -c 'import sys; print(sys.executable)').Trim()
    Start-Process `
        -FilePath $python `
        -ArgumentList $agent `
        -WorkingDirectory $projectDir `
        -WindowStyle Hidden `
        -RedirectStandardOutput (Join-Path $runtimeDir 'native-agent.log') `
        -RedirectStandardError (Join-Path $runtimeDir 'native-agent-error.log')

    $deadline = (Get-Date).AddSeconds(15)
    do {
        Start-Sleep -Milliseconds 500
        try {
            $status = Invoke-RestMethod `
                -Uri 'http://127.0.0.1:8213/status' `
                -Headers $headers `
                -TimeoutSec 2
        }
        catch {
            $status = $null
        }
    } until ($status -or (Get-Date) -ge $deadline)
    if (-not $status) {
        throw 'The native Palworld control agent did not start.'
    }
}
else {
    $status = Invoke-RestMethod `
        -Uri 'http://127.0.0.1:8213/status' `
        -Headers $headers `
        -TimeoutSec 5
}

if ($status.status -ne 'running') {
    Invoke-RestMethod `
        -Uri 'http://127.0.0.1:8213/start' `
        -Method Post `
        -Headers $headers `
        -TimeoutSec 30 | Out-Null
}

docker compose -f $compose up -d
if ($LASTEXITCODE -ne 0) {
    throw 'Docker Compose failed to start PAL CTRL.'
}

Write-Host 'Real Palworld and PAL CTRL are running.'
Write-Host 'Panel: http://127.0.0.1:8080'
Write-Host "Memory cap: $MaxMemoryGB GB | Players: $Players"
