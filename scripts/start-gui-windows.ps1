#requires -Version 7.0

[CmdletBinding()]
param(
    [string]$Root = "",
    [string]$FfmpegPath = "",
    [string]$AudioDevice = "",
    [string]$PythonPath = "",
    [string]$WorkerPath = "",
    [string]$ModelPath = "",
    [ValidateRange(1, 65535)][int]$Port = 8765,
    [switch]$OpenBrowser
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$Executable = Join-Path $RepoRoot "bin\studypilot.exe"
if (-not (Test-Path -LiteralPath $Executable -PathType Leaf)) {
    throw "bin\studypilot.exe is missing. Run scripts\verify-windows.ps1 or go build first."
}
if (($FfmpegPath -and -not $AudioDevice) -or ($AudioDevice -and -not $FfmpegPath)) {
    throw "FfmpegPath and AudioDevice must be supplied together; no device is selected automatically."
}
if ($FfmpegPath) {
    $FfmpegPath = (Resolve-Path -LiteralPath $FfmpegPath).Path
    if (-not [string]::Equals([IO.Path]::GetFileName($FfmpegPath), "ffmpeg.exe", [StringComparison]::OrdinalIgnoreCase)) {
        throw "FfmpegPath must resolve to ffmpeg.exe."
    }
}
$TranscriptionValues = @($PythonPath, $WorkerPath, $ModelPath) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
if ($TranscriptionValues.Count -ne 0 -and $TranscriptionValues.Count -ne 3) {
    throw "PythonPath, WorkerPath, and ModelPath must be supplied together."
}
if ($TranscriptionValues.Count -eq 3) {
    $PythonPath = (Resolve-Path -LiteralPath $PythonPath).Path
    $WorkerPath = (Resolve-Path -LiteralPath $WorkerPath).Path
    $ModelPath = (Resolve-Path -LiteralPath $ModelPath).Path
}

function Quote-WindowsArgument([string]$Value) {
    if ($Value -notmatch '[\s"]') { return $Value }
    return '"' + (($Value -replace '(\\*)"', '$1$1\"') -replace '(\\+)$', '$1$1') + '"'
}

$Address = "127.0.0.1:$Port"
$GuiArguments = @("gui")
if (-not [string]::IsNullOrWhiteSpace($Root)) {
    $ResolvedRoot = [IO.Path]::GetFullPath($Root)
    $GuiArguments += @("--root", $ResolvedRoot)
}
$GuiArguments += @("--address", $Address)
$ProcessInfo = New-Object System.Diagnostics.ProcessStartInfo
$ProcessInfo.FileName = $Executable
$ProcessInfo.Arguments = ($GuiArguments | ForEach-Object { Quote-WindowsArgument $_ }) -join " "
$ProcessInfo.UseShellExecute = $false
$ProcessInfo.CreateNoWindow = $false
if ($FfmpegPath) {
    $ProcessInfo.EnvironmentVariables["STUDYPILOT_CAPTURE_BACKEND"] = "local"
    $ProcessInfo.EnvironmentVariables["STUDYPILOT_CAPTURE_EXECUTABLE"] = $FfmpegPath
    $ProcessInfo.EnvironmentVariables["STUDYPILOT_CAPTURE_DRIVER"] = "dshow"
    $ProcessInfo.EnvironmentVariables["STUDYPILOT_CAPTURE_DEVICE"] = $AudioDevice
}
if ($TranscriptionValues.Count -eq 3) {
    $ProcessInfo.EnvironmentVariables["STUDYPILOT_TRANSCRIPTION_BACKEND"] = "local"
    $ProcessInfo.EnvironmentVariables["STUDYPILOT_TRANSCRIPTION_MODEL_ID"] = "faster-whisper/base.en"
    $ProcessInfo.EnvironmentVariables["STUDYPILOT_PYTHON"] = $PythonPath
    $ProcessInfo.EnvironmentVariables["STUDYPILOT_TRANSCRIPTION_WORKER"] = $WorkerPath
    $ProcessInfo.EnvironmentVariables["STUDYPILOT_TRANSCRIPTION_MODEL"] = $ModelPath
}
$Process = [System.Diagnostics.Process]::Start($ProcessInfo)
$Url = "http://$Address/"
Write-Host "StudyPilot started as PID $($Process.Id) at $Url"
if ($OpenBrowser) {
    Start-Process $Url
}
