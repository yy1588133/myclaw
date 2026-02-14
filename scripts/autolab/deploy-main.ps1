#!/usr/bin/env pwsh
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Invoke-CheckedCommand {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,
        [Parameter()]
        [string[]]$Arguments = @()
    )

    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        $joinedArgs = ($Arguments -join " ")
        throw "Error: command failed ($FilePath $joinedArgs) with exit code $LASTEXITCODE"
    }
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$defaultRepoPath = (Resolve-Path (Join-Path $scriptDir "..\..")).Path

$prodRepoPath = if ($env:PROD_REPO_PATH -and $env:PROD_REPO_PATH.Trim() -ne "") {
    $env:PROD_REPO_PATH
} else {
    $defaultRepoPath
}

$serviceName = if ($env:SERVICE_NAME -and $env:SERVICE_NAME.Trim() -ne "") {
    $env:SERVICE_NAME
} else {
    "myclaw"
}

$binaryPath = if ($env:BINARY_PATH -and $env:BINARY_PATH.Trim() -ne "") {
    $env:BINARY_PATH
} else {
    Join-Path $prodRepoPath "bin/myclaw.exe"
}

$restartCmd = if ($env:DEPLOY_RESTART_CMD -and $env:DEPLOY_RESTART_CMD.Trim() -ne "") {
    $env:DEPLOY_RESTART_CMD
} else {
    ""
}

if (-not (Test-Path -LiteralPath (Join-Path $prodRepoPath ".git"))) {
    throw "Error: PROD_REPO_PATH is not a git repository: $prodRepoPath"
}

$git = Get-Command git -ErrorAction SilentlyContinue
if (-not $git) {
    throw "Error: git binary not found in PATH"
}
$gitBin = $git.Source

$go = Get-Command go -ErrorAction SilentlyContinue
if ($go) {
    $goBin = $go.Source
} elseif (Test-Path -LiteralPath "C:\Program Files\Go\bin\go.exe") {
    $goBin = "C:\Program Files\Go\bin\go.exe"
} else {
    throw "Error: go binary not found in PATH or C:\Program Files\Go\bin\go.exe"
}

Push-Location $prodRepoPath
try {
    Invoke-CheckedCommand -FilePath $gitBin -Arguments @("fetch", "origin")
    Invoke-CheckedCommand -FilePath $gitBin -Arguments @("pull", "--ff-only", "origin", "main")

    $binaryDir = Split-Path -Parent $binaryPath
    if ($binaryDir -and -not (Test-Path -LiteralPath $binaryDir)) {
        New-Item -ItemType Directory -Path $binaryDir -Force | Out-Null
    }

    Invoke-CheckedCommand -FilePath $goBin -Arguments @("build", "-o", $binaryPath, "./cmd/myclaw")

    $service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
    if ($service) {
        Restart-Service -Name $serviceName -ErrorAction Stop
        Start-Sleep -Seconds 2
        $service = Get-Service -Name $serviceName -ErrorAction Stop
        if ($service.Status -ne [System.ServiceProcess.ServiceControllerStatus]::Running) {
            throw "Error: service '$serviceName' is not running after restart (status=$($service.Status))"
        }
    } elseif ($restartCmd -ne "") {
        Invoke-CheckedCommand -FilePath "cmd.exe" -Arguments @("/c", $restartCmd)
    } else {
        throw "Error: service '$serviceName' not found and DEPLOY_RESTART_CMD is empty"
    }

    $sha = (& $gitBin rev-parse --short HEAD).Trim()
    if ($LASTEXITCODE -ne 0) {
        throw "Error: failed to resolve deployed git sha"
    }
    Write-Output "deploy=ok service=$serviceName sha=$sha"
}
finally {
    Pop-Location
}
