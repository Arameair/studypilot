#requires -Version 7.0

[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$TemporaryRoot = Join-Path $env:TEMP ("studypilot-windows-verify-" + [guid]::NewGuid().ToString("N"))
if ([string]::IsNullOrWhiteSpace($env:GOCACHE)) {
    $env:GOCACHE = Join-Path $env:TEMP "studypilot-go-build-cache"
}
$env:GOPROXY = "off"
$env:PYTHONPYCACHEPREFIX = Join-Path $TemporaryRoot "python-cache"
$Succeeded = $false

function Invoke-Native {
    param([Parameter(Mandatory = $true)][string]$Executable, [string[]]$Arguments = @())
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Executable failed with exit code $LASTEXITCODE."
    }
}

try {
    New-Item -ItemType Directory -Path $TemporaryRoot -Force | Out-Null
    New-Item -ItemType Directory -Path $env:GOCACHE -Force | Out-Null
    Set-Location $RepoRoot

    $Go = (Get-Command go -ErrorAction Stop).Source
    $Gofmt = (Get-Command gofmt -ErrorAction Stop).Source
    $GoDirectories = @(
        ".\cmd\studypilot",
        ".\internal\application",
        ".\internal\capture",
        ".\internal\filesystem",
        ".\internal\httpapi",
        ".\internal\migration",
        ".\internal\platformfs",
        ".\internal\runtime",
        ".\internal\session",
        ".\internal\studyartifact",
        ".\internal\transcription",
        ".\internal\workspace"
    )
    $Unformatted = & $Gofmt "-l" @GoDirectories
    if ($LASTEXITCODE -ne 0) {
        throw "gofmt inspection failed."
    }
    if ($Unformatted) {
        throw "Go formatting check failed: $($Unformatted -join ', ')"
    }

    $Packages = @(& $Go "list" "./...")
    if ($LASTEXITCODE -ne 0 -or $Packages.Count -eq 0) {
        throw "Go package listing failed before tests."
    }
    foreach ($Package in $Packages) {
        Invoke-Native $Go @("test", $Package)
    }
    Invoke-Native $Go @("vet", "./...")
    Invoke-Native $Go @("list", "./...")
    New-Item -ItemType Directory -Path (Join-Path $RepoRoot "bin") -Force | Out-Null
    Invoke-Native $Go @("build", "-o", (Join-Path $RepoRoot "bin\studypilot.exe"), "./cmd/studypilot")

    $PythonCommand = Get-Command python.exe -ErrorAction SilentlyContinue
    if (-not $PythonCommand) {
        $PythonCommand = Get-Command python -ErrorAction SilentlyContinue
    }
    if ($PythonCommand) {
        Invoke-Native $PythonCommand.Source @("-m", "unittest", "discover", "-s", "tools\transcription-worker\tests", "-p", "test_*.py")
        Invoke-Native $PythonCommand.Source @("-m", "py_compile", "tools\transcription-worker\worker.py", "tools\transcription-worker\tests\test_worker.py")
    }
    else {
        Write-Warning "Python is unavailable; standard-library worker tests were skipped."
    }

    Get-ChildItem (Join-Path $RepoRoot "scripts\*.ps1") | ForEach-Object {
        $Tokens = $null
        $Errors = $null
        [System.Management.Automation.Language.Parser]::ParseFile(
            $_.FullName,
            [ref]$Tokens,
            [ref]$Errors
        ) | Out-Null
        if ($Errors.Count -gt 0) {
            $Errors | Format-List | Out-String | Write-Error
            throw "PowerShell syntax validation failed for $($_.Name)."
        }
    }

    & (Join-Path $RepoRoot "scripts\validate-gui-workflow-windows.ps1")
    if ($LASTEXITCODE -ne 0) {
        throw "Synthetic Windows GUI workflow validation failed."
    }
    Invoke-Native "git" @("diff", "--check")
    $Succeeded = $true
    Write-Host "PASS - native Windows deterministic verification completed."
}
finally {
    Set-Location $RepoRoot
    if ($Succeeded -and (Test-Path -LiteralPath $TemporaryRoot)) {
        Remove-Item -LiteralPath $TemporaryRoot -Recurse -Force
    }
    elseif (-not $Succeeded) {
        Write-Warning "Windows verification evidence retained at: $TemporaryRoot"
    }
}
