#requires -Version 7.0

[CmdletBinding()]
param(
    [string]$PythonPath = "",
    [switch]$CreateEnvironment,
    [switch]$InstallDependencies,
    [switch]$DownloadBaseEnglishModel
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$EnvironmentRoot = Join-Path $RepoRoot ".venv-transcription"
$EnvironmentPython = Join-Path $EnvironmentRoot "Scripts\python.exe"
$Requirements = Join-Path $RepoRoot "tools\transcription-worker\requirements.txt"
$ModelRoot = Join-Path $RepoRoot ".local\transcription-models\base.en"

function Invoke-Python {
    param([Parameter(Mandatory = $true)][string]$Executable, [string[]]$Arguments)
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Python operation failed with exit code $LASTEXITCODE."
    }
}

Set-Location $RepoRoot
if ([string]::IsNullOrWhiteSpace($PythonPath)) {
    $PythonCommand = Get-Command python.exe -ErrorAction SilentlyContinue
    if (-not $PythonCommand) {
        $PythonCommand = Get-Command python -ErrorAction SilentlyContinue
    }
    if ($PythonCommand) {
        $PythonPath = $PythonCommand.Source
    }
}

Write-Host "StudyPilot local transcription audit"
Write-Host "Virtual environment: $(if (Test-Path -LiteralPath $EnvironmentPython -PathType Leaf) { 'present' } else { 'missing' })"
Write-Host "Pinned requirements: $(if (Test-Path -LiteralPath $Requirements -PathType Leaf) { 'present' } else { 'missing' })"
Write-Host "Base English model: $(if (Test-Path -LiteralPath $ModelRoot -PathType Container) { 'present' } else { 'missing' })"

if (-not ($CreateEnvironment -or $InstallDependencies -or $DownloadBaseEnglishModel)) {
    Write-Host "Audit only. No environment, dependency, or model changes were made."
    exit 0
}
if ([string]::IsNullOrWhiteSpace($PythonPath) -or -not (Test-Path -LiteralPath $PythonPath -PathType Leaf)) {
    throw "A native Windows Python executable is required."
}
Invoke-Python $PythonPath @("-c", "import sys; raise SystemExit(0 if (3, 10) <= sys.version_info[:2] <= (3, 13) else 2)")

if ($CreateEnvironment -and -not (Test-Path -LiteralPath $EnvironmentPython -PathType Leaf)) {
    Invoke-Python $PythonPath @("-m", "venv", $EnvironmentRoot)
}
if (($InstallDependencies -or $DownloadBaseEnglishModel) -and -not (Test-Path -LiteralPath $EnvironmentPython -PathType Leaf)) {
    throw "The repository-local environment is missing. Re-run with -CreateEnvironment first or include it now."
}
if ($InstallDependencies) {
    Invoke-Python $EnvironmentPython @("-m", "pip", "install", "--require-virtualenv", "--requirement", $Requirements)
}
if ($DownloadBaseEnglishModel) {
    $DownloadCode = @'
from huggingface_hub import snapshot_download
import os
target = os.environ["STUDYPILOT_APPROVED_MODEL_TARGET"]
os.makedirs(target, exist_ok=True)
snapshot_download(
    repo_id="Systran/faster-whisper-base.en",
    local_dir=target,
    local_dir_use_symlinks=False,
)
'@
    $PreviousTarget = $env:STUDYPILOT_APPROVED_MODEL_TARGET
    try {
        $env:STUDYPILOT_APPROVED_MODEL_TARGET = $ModelRoot
        Invoke-Python $EnvironmentPython @("-c", $DownloadCode)
    }
    finally {
        $env:STUDYPILOT_APPROVED_MODEL_TARGET = $PreviousTarget
    }
}
Write-Host "Requested local transcription setup operations completed."
