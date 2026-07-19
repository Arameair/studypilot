#requires -Version 7.0

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$FfmpegPath,
    [Parameter(Mandatory = $true)][string]$AudioDevice,
    [Parameter(Mandatory = $true)][switch]$AuthorizeRecording,
    [string]$PythonPath = "",
    [string]$WorkerPath = "",
    [string]$ModelPath = "",
    [string]$ModelID = "faster-whisper/base.en"
)

$ErrorActionPreference = "Stop"
if (-not $AuthorizeRecording) { throw "Real recording requires the explicit -AuthorizeRecording switch." }
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$FfmpegPath = (Resolve-Path -LiteralPath $FfmpegPath).Path
if (-not [string]::Equals([IO.Path]::GetFileName($FfmpegPath), "ffmpeg.exe", [StringComparison]::OrdinalIgnoreCase)) {
    throw "FfmpegPath must resolve to ffmpeg.exe."
}
if ([string]::IsNullOrWhiteSpace($AudioDevice) -or $AudioDevice -match "[`0`r`n]" -or $AudioDevice -match "^(audio|video)=") {
    throw "AudioDevice is invalid."
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

$EvidenceRoot = Join-Path $env:TEMP ("spc-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
$WorkspaceRoot = Join-Path $EvidenceRoot "Capture Workspace"
$Executable = Join-Path $RepoRoot "bin\studypilot.exe"
$Server = $null
$RecorderPIDs = New-Object System.Collections.Generic.List[int]
$BaselineFfmpegPIDs = @((Get-Process -Name ffmpeg -ErrorAction SilentlyContinue).Id)
$Succeeded = $false
$Stage = "preparing"
$Revision = 0
$SessionBase = ""
$BaseUrl = ""

Add-Type -TypeDefinition @"
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Text;

public static class StudyPilotWindowsProcessGroup
{
    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    private struct STARTUPINFO
    {
        public int cb;
        public string lpReserved;
        public string lpDesktop;
        public string lpTitle;
        public int dwX;
        public int dwY;
        public int dwXSize;
        public int dwYSize;
        public int dwXCountChars;
        public int dwYCountChars;
        public int dwFillAttribute;
        public int dwFlags;
        public short wShowWindow;
        public short cbReserved2;
        public IntPtr lpReserved2;
        public IntPtr hStdInput;
        public IntPtr hStdOutput;
        public IntPtr hStdError;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct PROCESS_INFORMATION
    {
        public IntPtr hProcess;
        public IntPtr hThread;
        public int dwProcessId;
        public int dwThreadId;
    }

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern bool CreateProcessW(
        string applicationName,
        StringBuilder commandLine,
        IntPtr processAttributes,
        IntPtr threadAttributes,
        bool inheritHandles,
        uint creationFlags,
        IntPtr environment,
        string currentDirectory,
        ref STARTUPINFO startupInfo,
        out PROCESS_INFORMATION processInformation);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool GenerateConsoleCtrlEvent(uint ctrlEvent, uint processGroupId);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool CloseHandle(IntPtr handle);

    public static int Start(string applicationName, string commandLine, string currentDirectory)
    {
        STARTUPINFO startupInfo = new STARTUPINFO();
        startupInfo.cb = Marshal.SizeOf(startupInfo);
        PROCESS_INFORMATION processInformation;
        const uint CREATE_NEW_PROCESS_GROUP = 0x00000200;
        if (!CreateProcessW(applicationName, new StringBuilder(commandLine), IntPtr.Zero, IntPtr.Zero,
            false, CREATE_NEW_PROCESS_GROUP, IntPtr.Zero, currentDirectory, ref startupInfo,
            out processInformation))
        {
            throw new Win32Exception(Marshal.GetLastWin32Error(), "CreateProcessW failed.");
        }
        CloseHandle(processInformation.hThread);
        CloseHandle(processInformation.hProcess);
        return processInformation.dwProcessId;
    }

    public static void SendBreak(int processGroupId)
    {
        const uint CTRL_BREAK_EVENT = 1;
        if (!GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, (uint)processGroupId))
        {
            throw new Win32Exception(Marshal.GetLastWin32Error(), "GenerateConsoleCtrlEvent failed.");
        }
    }
}
"@

function Invoke-Native {
    param([Parameter(Mandatory = $true)][string]$ExecutablePath, [string[]]$Arguments = @())
    & $ExecutablePath @Arguments | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "$ExecutablePath failed with exit code $LASTEXITCODE." }
}
function Quote-WindowsArgument([string]$Value) {
    if ($Value -notmatch '[\s"]') { return $Value }
    return '"' + (($Value -replace '(\\*)"', '$1$1\"') -replace '(\\+)$', '$1$1') + '"'
}
function Get-FreePort {
    $Listener = New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback, 0)
    $Listener.Start()
    try { return ([Net.IPEndPoint]$Listener.LocalEndpoint).Port } finally { $Listener.Stop() }
}
function Start-CaptureServer {
    $Arguments = (@("gui", "--root", $WorkspaceRoot, "--address", "127.0.0.1:$Port") | ForEach-Object { Quote-WindowsArgument $_ }) -join " "
    $Overrides = @{
        STUDYPILOT_CAPTURE_BACKEND = "local"
        STUDYPILOT_CAPTURE_EXECUTABLE = $FfmpegPath
        STUDYPILOT_CAPTURE_DRIVER = "dshow"
        STUDYPILOT_CAPTURE_DEVICE = $AudioDevice
        STUDYPILOT_TRANSCRIPTION_BACKEND = $null
        STUDYPILOT_TRANSCRIPTION_MODEL_ID = $null
        STUDYPILOT_PYTHON = $null
        STUDYPILOT_TRANSCRIPTION_WORKER = $null
        STUDYPILOT_TRANSCRIPTION_MODEL = $null
    }
    if ($TranscriptionValues.Count -eq 3) {
        $Overrides.STUDYPILOT_TRANSCRIPTION_BACKEND = "local"
        $Overrides.STUDYPILOT_TRANSCRIPTION_MODEL_ID = $ModelID
        $Overrides.STUDYPILOT_PYTHON = $PythonPath
        $Overrides.STUDYPILOT_TRANSCRIPTION_WORKER = $WorkerPath
        $Overrides.STUDYPILOT_TRANSCRIPTION_MODEL = $ModelPath
    }
    $Previous = @{}
    foreach ($Name in $Overrides.Keys) {
        $Previous[$Name] = [Environment]::GetEnvironmentVariable($Name, "Process")
        [Environment]::SetEnvironmentVariable($Name, $Overrides[$Name], "Process")
    }
    try {
        $CommandLine = (Quote-WindowsArgument $Executable) + " " + $Arguments
        $PIDValue = [StudyPilotWindowsProcessGroup]::Start($Executable, $CommandLine, $RepoRoot)
        $script:Server = [Diagnostics.Process]::GetProcessById($PIDValue)
    }
    finally {
        foreach ($Name in $Previous.Keys) {
            [Environment]::SetEnvironmentVariable($Name, $Previous[$Name], "Process")
        }
    }
    for ($Attempt = 0; $Attempt -lt 100; $Attempt++) {
        if ($Server.HasExited) { throw "StudyPilot stopped before health became ready." }
        try {
            Invoke-RestMethod -Uri "$BaseUrl/api/v1/health" -Method Get -TimeoutSec 1 | Out-Null
            return
        }
        catch { Start-Sleep -Milliseconds 50 }
    }
    throw "StudyPilot health endpoint did not become ready."
}
function Stop-CaptureServer {
    if ($script:Server -and -not $script:Server.HasExited) {
        [StudyPilotWindowsProcessGroup]::SendBreak($script:Server.Id)
        if (-not $script:Server.WaitForExit(5000)) {
            $script:Server.Kill()
            throw "StudyPilot did not shut down cleanly within five seconds."
        }
    }
    $script:Server = $null
}
function Assert-PortReleased {
    for ($Attempt = 0; $Attempt -lt 50; $Attempt++) {
        $Client = [Net.Sockets.TcpClient]::new()
        try {
            $Connection = $Client.ConnectAsync("127.0.0.1", $Port)
            if (-not $Connection.Wait(100)) { return }
            if ($Connection.IsFaulted) { return }
        }
        catch { return }
        finally { $Client.Dispose() }
        Start-Sleep -Milliseconds 50
    }
    throw "StudyPilot loopback port remained bound after shutdown."
}
function Get-RecorderChildren {
    if (-not $Server) { return @() }
    return @(Get-Process -Name ffmpeg -ErrorAction SilentlyContinue | Where-Object {
        $BaselineFfmpegPIDs -notcontains $_.Id -and $_.StartTime -ge $Server.StartTime
    })
}
function Remember-RecorderChildren {
    foreach ($Child in (Get-RecorderChildren)) {
        if (-not $RecorderPIDs.Contains([int]$Child.Id)) { $RecorderPIDs.Add([int]$Child.Id) }
    }
}
function Assert-ActiveCaptureEvidence {
    $OwnershipFiles = @(Get-ChildItem -LiteralPath $WorkspaceRoot -Recurse -File -Filter ".studypilot-capture.lock")
    $PartialFiles = @(Get-ChildItem -LiteralPath $WorkspaceRoot -Recurse -File -Filter "*.partial")
    if ($OwnershipFiles.Count -ne 1 -or $PartialFiles.Count -ne 1) {
        throw "Active capture did not have exactly one ownership record and one partial WAV."
    }
    $Ownership = Get-Content -LiteralPath $OwnershipFiles[0].FullName -Raw | ConvertFrom-Json
    if ($Ownership.schema_version -ne 1 -or [string]::IsNullOrWhiteSpace($Ownership.capture_id) -or
        [string]::IsNullOrWhiteSpace($Ownership.segment_id) -or $Ownership.number -le 0 -or
        $Ownership.process_id -le 0 -or [string]::IsNullOrWhiteSpace($Ownership.host) -or
        [string]::IsNullOrWhiteSpace($Ownership.started_at)) {
        throw "Active capture ownership evidence is invalid."
    }
}
function Invoke-Api {
    param([string]$Method, [string]$Path, [object]$Body = $null)
    $Parameters = @{ Uri = "$BaseUrl$Path"; Method = $Method; TimeoutSec = 15 }
    if ($null -ne $Body) {
        $Parameters.ContentType = "application/json; charset=utf-8"
        $Parameters.Body = $Body | ConvertTo-Json -Depth 10 -Compress
    }
    $Result = Invoke-RestMethod @Parameters
    $Encoded = $Result | ConvertTo-Json -Depth 20 -Compress
    if ($Encoded.Contains($WorkspaceRoot) -or $Encoded.Contains($FfmpegPath) -or $Encoded.Contains($AudioDevice)) {
        throw "A GUI API response exposed private capture configuration."
    }
    return $Result
}
function Assert-WavFormat([string]$Path) {
    $Stream = [IO.File]::OpenRead($Path)
    $Reader = New-Object IO.BinaryReader($Stream)
    try {
        if ([Text.Encoding]::ASCII.GetString($Reader.ReadBytes(4)) -ne "RIFF") { throw "WAV RIFF header is missing." }
        $Reader.ReadUInt32() | Out-Null
        if ([Text.Encoding]::ASCII.GetString($Reader.ReadBytes(4)) -ne "WAVE") { throw "WAV format header is missing." }
        $FormatFound = $false
        $DataBytes = 0
        while ($Stream.Position + 8 -le $Stream.Length) {
            $Chunk = [Text.Encoding]::ASCII.GetString($Reader.ReadBytes(4))
            $Length = $Reader.ReadUInt32()
            if ($Chunk -eq "fmt ") {
                $AudioFormat = $Reader.ReadUInt16()
                $Channels = $Reader.ReadUInt16()
                $SampleRate = $Reader.ReadUInt32()
                $Reader.ReadUInt32() | Out-Null
                $Reader.ReadUInt16() | Out-Null
                $Bits = $Reader.ReadUInt16()
                if ($Length -gt 16) { $Reader.ReadBytes([int]$Length - 16) | Out-Null }
                if ($AudioFormat -ne 1 -or $Channels -ne 1 -or $SampleRate -ne 16000 -or $Bits -ne 16) {
                    throw "WAV is not mono 16 kHz 16-bit PCM."
                }
                $FormatFound = $true
            }
            elseif ($Chunk -eq "data") {
                $DataBytes = $Length
                $Reader.ReadBytes([int]$Length) | Out-Null
            }
            else {
                $Reader.ReadBytes([int]$Length) | Out-Null
            }
            if (($Length % 2) -eq 1 -and $Stream.Position -lt $Stream.Length) { $Reader.ReadByte() | Out-Null }
        }
        if (-not $FormatFound -or $DataBytes -eq 0) { throw "WAV contains no valid audio data." }
    }
    finally { $Reader.Dispose(); $Stream.Dispose() }
}

try {
    New-Item -ItemType Directory -Path $EvidenceRoot -Force | Out-Null
    Set-Location $RepoRoot
    $env:GOCACHE = Join-Path $EvidenceRoot "go-build-cache"
    $env:GOPROXY = "off"

    $Consent = Read-Host "Type RECORD to validate the selected DirectShow device with purpose-created speech"
    if ($Consent -cne "RECORD") { throw "Recording was not approved at the script prompt." }
    $ProbeWav = Join-Path $EvidenceRoot "direct-device-probe.wav"
    Write-Host "Speak a short purpose-created validation phrase now."
    $Stage = "validating selected DirectShow source directly"
    $ProbeArguments = @("-hide_banner", "-loglevel", "error", "-f", "dshow", "-i", "audio=$AudioDevice", "-t", "1", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-map_metadata", "-1", "-fflags", "+bitexact", "-flags:a", "+bitexact", "-f", "wav", "-y", $ProbeWav)
    Invoke-Native $FfmpegPath $ProbeArguments
    Assert-WavFormat $ProbeWav
    Write-Host "PASS - direct FFmpeg DirectShow prerequisite and canonical WAV validation."

    $Go = (Get-Command go -ErrorAction Stop).Source
    New-Item -ItemType Directory -Path (Split-Path $Executable) -Force | Out-Null
    Invoke-Native $Go @("build", "-o", $Executable, "./cmd/studypilot")
    Invoke-Native $Executable @("init", "--root", $WorkspaceRoot)
    Invoke-Native $Executable @("course", "create", "--root", $WorkspaceRoot, "--name", "Win Capture")
    Invoke-Native $Executable @("module", "create", "--root", $WorkspaceRoot, "--course", "Win Capture", "--number", "1", "--name", "DirectShow")

    $Port = Get-FreePort
    $BaseUrl = "http://127.0.0.1:$Port"
    $Stage = "starting StudyPilot"
    Start-CaptureServer
    $Courses = Invoke-Api Get "/api/v1/courses"
    $CourseID = $Courses.courses[0].id
    $Modules = Invoke-Api Get "/api/v1/courses/$CourseID/modules"
    $ModuleID = $Modules.modules[0].id
    $Created = Invoke-Api Post "/api/v1/courses/$CourseID/modules/$ModuleID/sessions" @{ title = "Capture test" }
    $SessionID = $Created.id
    $Revision = [uint64]$Created.revision
    $SessionBase = "/api/v1/sessions/$CourseID/$ModuleID/$SessionID"
    $Started = Invoke-Api Post "$SessionBase/start" @{ expected_revision = $Revision }
    $Revision = [uint64]$Started.revision
    $Workspace = Invoke-Api Get $SessionBase
    if (-not $Workspace.capture_execution.available -or $Workspace.capture_execution.driver -ne "dshow") {
        throw "DirectShow capture capability is unavailable."
    }

    $Consent = Read-Host "Type FIRST to record the first short purpose-created phrase"
    if ($Consent -cne "FIRST") { throw "First StudyPilot recording was not approved." }
    Write-Host "Speak the first validation phrase."
    $Stage = "recording segment one"
    $Recording = Invoke-Api Post "$SessionBase/capture/start" @{ expected_revision = $Revision }
    $Revision = [uint64]$Recording.revision
    Start-Sleep -Seconds 2
    Remember-RecorderChildren
    Assert-ActiveCaptureEvidence
    $Paused = Invoke-Api Post "$SessionBase/capture/pause" @{ expected_revision = $Revision }
    $Revision = [uint64]$Paused.revision

    $WavFiles = @(Get-ChildItem -LiteralPath $WorkspaceRoot -Recurse -File -Filter "*-audio.wav" | Sort-Object FullName)
    if ($WavFiles.Count -ne 1) { throw "Pause did not finalize exactly one WAV." }
    Assert-WavFormat $WavFiles[0].FullName
    if (Get-ChildItem -LiteralPath $WorkspaceRoot -Recurse -File | Where-Object { $_.Name -like "*.partial" -or $_.Name -eq ".studypilot-capture.lock" }) {
        throw "Pause did not clean the first partial WAV and ownership record."
    }
    $FirstHash = (Get-FileHash -LiteralPath $WavFiles[0].FullName -Algorithm SHA256).Hash
    Write-Host "PASS - StudyPilot start, pause, first WAV, ownership, and partial cleanup."

    $Consent = Read-Host "Type SECOND to record the second short purpose-created phrase"
    if ($Consent -cne "SECOND") { throw "Second StudyPilot recording was not approved." }
    Write-Host "Speak the second validation phrase."
    $Stage = "recording segment two"
    $Resumed = Invoke-Api Post "$SessionBase/capture/resume" @{ expected_revision = $Revision }
    $Revision = [uint64]$Resumed.revision
    Start-Sleep -Seconds 2
    Remember-RecorderChildren
    Assert-ActiveCaptureEvidence
    Invoke-Api Post "$SessionBase/capture/stop" @{ expected_revision = $Revision } | Out-Null

    $Stage = "validating finalized capture evidence"
    $Workspace = Invoke-Api Get $SessionBase
    $Revision = [uint64]$Workspace.session.revision
    $WavFiles = @(Get-ChildItem -LiteralPath $WorkspaceRoot -Recurse -File -Filter "*-audio.wav" | Sort-Object FullName)
    $Manifests = @(Get-ChildItem -LiteralPath $WorkspaceRoot -Recurse -File -Filter "*-segment.json" | Sort-Object FullName)
    if ($WavFiles.Count -ne 2 -or $Manifests.Count -ne 2 -or $Workspace.session.segments.Count -ne 2) {
        throw "Expected two WAVs, manifests, and runtime segments."
    }
    foreach ($Wav in $WavFiles) { Assert-WavFormat $Wav.FullName }
    foreach ($Manifest in $Manifests) {
        $Value = Get-Content -LiteralPath $Manifest.FullName -Raw | ConvertFrom-Json
        if ($Value.schema_version -ne 1 -or $Value.status -ne "stopped" -or
            [IO.Path]::GetFileName($Value.audio_file) -ne $Value.audio_file -or
            $Value.sample_rate -ne 16000 -or $Value.channels -ne 1 -or
            $Value.bit_depth -ne 16 -or $Value.partial -or $Value.bytes_written -le 0) {
            throw "A segment manifest does not describe finalized canonical WAV audio."
        }
    }
    if ((Get-FileHash -LiteralPath $WavFiles[0].FullName -Algorithm SHA256).Hash -ne $FirstHash) {
        throw "Finalizing segment two changed segment one."
    }
    if (Get-ChildItem -LiteralPath $WorkspaceRoot -Recurse -File | Where-Object { $_.Name -like "*.partial" -or $_.Name -eq ".studypilot-capture.lock" }) {
        throw "Clean capture left partial or ownership evidence."
    }
    if ((Get-RecorderChildren).Count -ne 0) { throw "An owned FFmpeg process remained after stop." }
    Write-Host "PASS - resume, stop, second WAV, manifests, cleanup, and owned FFmpeg reaping."

    if ($TranscriptionValues.Count -eq 3) {
        $Stage = "validating optional local transcription"
        $BeforeHashes = @($WavFiles | ForEach-Object { (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash })
        foreach ($Segment in $Workspace.session.segments) {
            $Result = Invoke-Api Post "$SessionBase/transcription/execute" @{
                segment_id = $Segment.id
                backend = "faster-whisper"
                model = $ModelID
                language = "en"
                max_attempts = 3
                expected_revision = $Revision
            }
            if (-not $Result.completed) { throw "Local transcription did not complete." }
            $Revision = [uint64]$Result.runtime_revision
        }
        $AfterHashes = @($WavFiles | ForEach-Object { (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash })
        if (($BeforeHashes -join "|") -ne ($AfterHashes -join "|")) { throw "Transcription changed a source WAV." }
        $TranscriptText = @(Get-ChildItem -LiteralPath $WorkspaceRoot -Recurse -File -Filter "*-transcript.txt")
        if ($TranscriptText.Count -ne 2 -or @($TranscriptText | Where-Object { $_.Length -eq 0 }).Count -ne 0) {
            throw "Local transcription did not produce two nonempty transcript artifacts."
        }
    }

    $Stage = "restarting and inspecting persisted state"
    Stop-CaptureServer
    Assert-PortReleased
    Start-CaptureServer
    $Restarted = Invoke-Api Get $SessionBase
    if ($Restarted.session.segments.Count -ne 2 -or @($Restarted.capture.issues).Count -ne 0) {
        throw "Restart inspection did not preserve clean finalized capture state."
    }
    Stop-CaptureServer
    Assert-PortReleased
    foreach ($PIDValue in $RecorderPIDs) {
        if (Get-Process -Id $PIDValue -ErrorAction SilentlyContinue) { throw "An owned FFmpeg process remained after validation." }
    }
    Write-Host "PASS - restart inspection and loopback port release."
    $Succeeded = $true
    Write-Host "PASS - native Windows DirectShow capture finalized two canonical segments and survived restart."
}
finally {
    if ($Server -and -not $Server.HasExited) {
        if ($SessionBase -and $Revision -gt 0) {
            try { Invoke-Api Post "$SessionBase/capture/stop" @{ expected_revision = $Revision } | Out-Null } catch {}
        }
        try { $Server.Kill(); $Server.WaitForExit(5000) | Out-Null } catch {}
    }
    foreach ($PIDValue in $RecorderPIDs) {
        $Owned = Get-Process -Id $PIDValue -ErrorAction SilentlyContinue
        if ($Owned) { try { Stop-Process -Id $PIDValue -Force -ErrorAction Stop } catch {} }
    }
    Set-Location $RepoRoot
    if ($Succeeded) {
        Remove-Item -LiteralPath $EvidenceRoot -Recurse -Force
    }
    else {
        $Partial = @(Get-ChildItem -LiteralPath $WorkspaceRoot -Recurse -File -ErrorAction SilentlyContinue | Where-Object { $_.Name -like "*.partial" }).Count -gt 0
        $Ownership = @(Get-ChildItem -LiteralPath $WorkspaceRoot -Recurse -File -ErrorAction SilentlyContinue | Where-Object { $_.Name -eq ".studypilot-capture.lock" }).Count -gt 0
        $RecorderAlive = @($RecorderPIDs | Where-Object { Get-Process -Id $_ -ErrorAction SilentlyContinue }).Count -gt 0
        Write-Warning "Windows capture validation failed during '$Stage'. Evidence retained at: $EvidenceRoot. Partial=$Partial Ownership=$Ownership RecorderAlive=$RecorderAlive"
    }
}
