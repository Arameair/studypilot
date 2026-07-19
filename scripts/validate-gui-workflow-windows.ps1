#requires -Version 7.0

[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$EvidenceRoot = Join-Path $env:TEMP ("spg-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
$WorkspaceRoot = Join-Path $EvidenceRoot "Workspace With Spaces"
$Executable = Join-Path $RepoRoot "bin\studypilot.exe"
$Server = $null
$Succeeded = $false
$Stage = "preparing"

function Invoke-Native {
    param([Parameter(Mandatory = $true)][string]$ExecutablePath, [string[]]$Arguments = @())
    & $ExecutablePath @Arguments | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "$ExecutablePath failed with exit code $LASTEXITCODE."
    }
}

function Quote-WindowsArgument([string]$Value) {
    if ($Value -notmatch '[\s"]') { return $Value }
    return '"' + (($Value -replace '(\\*)"', '$1$1\"') -replace '(\\+)$', '$1$1') + '"'
}

function Get-FreePort {
    $Listener = New-Object System.Net.Sockets.TcpListener([Net.IPAddress]::Loopback, 0)
    $Listener.Start()
    try { return ([Net.IPEndPoint]$Listener.LocalEndpoint).Port }
    finally { $Listener.Stop() }
}

function Start-StudyPilotServer {
    $Info = New-Object System.Diagnostics.ProcessStartInfo
    $Info.FileName = $Executable
    $Info.Arguments = (@("gui", "--root", $WorkspaceRoot, "--address", "127.0.0.1:$Port") | ForEach-Object { Quote-WindowsArgument $_ }) -join " "
    $Info.UseShellExecute = $false
    $Info.CreateNoWindow = $true
    $Info.EnvironmentVariables["STUDYPILOT_CAPTURE_BACKEND"] = "synthetic"
    $Info.EnvironmentVariables["STUDYPILOT_TRANSCRIPTION_BACKEND"] = "synthetic"
    $Info.EnvironmentVariables["STUDYPILOT_TRANSCRIPTION_MODEL_ID"] = "synthetic/deterministic"
    $script:Server = [System.Diagnostics.Process]::Start($Info)
    for ($Attempt = 0; $Attempt -lt 100; $Attempt++) {
        if ($Server.HasExited) { throw "StudyPilot stopped before the health endpoint was ready." }
        try {
            Invoke-RestMethod -Uri "$BaseUrl/api/v1/health" -Method Get -TimeoutSec 1 | Out-Null
            return
        }
        catch {
            Start-Sleep -Milliseconds 50
        }
    }
    throw "StudyPilot health endpoint did not become ready."
}

function Stop-StudyPilotServer {
    if ($script:Server -and -not $script:Server.HasExited) {
        $script:Server.Kill()
        if (-not $script:Server.WaitForExit(5000)) {
            throw "StudyPilot did not stop within five seconds."
        }
    }
    $script:Server = $null
}

function Invoke-Api {
    param(
        [Parameter(Mandatory = $true)][string]$Method,
        [Parameter(Mandatory = $true)][string]$Path,
        [object]$Body = $null
    )
    $Parameters = @{
        Uri = "$BaseUrl$Path"
        Method = $Method
        TimeoutSec = 10
    }
    if ($null -ne $Body) {
        $Parameters.ContentType = "application/json; charset=utf-8"
        $Parameters.Body = $Body | ConvertTo-Json -Depth 10 -Compress
    }
    $Result = Invoke-RestMethod @Parameters
    $Encoded = $Result | ConvertTo-Json -Depth 20 -Compress
    if ($Encoded.Contains($WorkspaceRoot) -or $Encoded -match '[A-Za-z]:\\\\Users\\\\') {
        throw "A GUI API response exposed an absolute private path."
    }
    return $Result
}

try {
    New-Item -ItemType Directory -Path $EvidenceRoot -Force | Out-Null
    Set-Location $RepoRoot
    $env:GOCACHE = Join-Path $EvidenceRoot "go-build-cache"
    $env:GOPROXY = "off"
    $Go = (Get-Command go -ErrorAction Stop).Source

    $Stage = "building native Windows executable"
    New-Item -ItemType Directory -Path (Split-Path $Executable) -Force | Out-Null
    Invoke-Native $Go @("build", "-o", $Executable, "./cmd/studypilot")
    $Stage = "initializing isolated workspace"
    Invoke-Native $Executable @("init", "--root", $WorkspaceRoot)
    Invoke-Native $Executable @("course", "create", "--root", $WorkspaceRoot, "--name", "Win Course")
    Invoke-Native $Executable @("module", "create", "--root", $WorkspaceRoot, "--course", "Win Course", "--number", "1", "--name", "Win Module")

    $Port = Get-FreePort
    $BaseUrl = "http://127.0.0.1:$Port"
    $Stage = "starting synthetic GUI server"
    Start-StudyPilotServer

    $Stage = "creating the synthetic session through the loopback API"
    $Courses = Invoke-Api Get "/api/v1/courses"
    $CourseID = $Courses.courses[0].id
    $Modules = Invoke-Api Get "/api/v1/courses/$CourseID/modules"
    $ModuleID = $Modules.modules[0].id
    $Created = Invoke-Api Post "/api/v1/courses/$CourseID/modules/$ModuleID/sessions" @{ title = "GUI session" }
    $SessionID = $Created.id
    $Revision = [uint64]$Created.revision
    $SessionBase = "/api/v1/sessions/$CourseID/$ModuleID/$SessionID"
    $ModuleBase = "/api/v1/courses/$CourseID/modules/$ModuleID"

    $Planned = Invoke-Api Get $SessionBase
    if (-not $Planned.controls.start_session -or $Planned.controls.start_capture) {
        throw "Planned-session controls are incorrect."
    }
    $Started = Invoke-Api Post "$SessionBase/start" @{ expected_revision = $Revision }
    $Revision = [uint64]$Started.revision
    $Active = Invoke-Api Get $SessionBase
    if ($Active.session.session_status -ne "active" -or -not $Active.controls.start_capture) {
        throw "Active session did not become recording-eligible."
    }

    $Recording = Invoke-Api Post "$SessionBase/capture/start" @{ expected_revision = $Revision }
    $Revision = [uint64]$Recording.revision
    $Paused = Invoke-Api Post "$SessionBase/capture/pause" @{ expected_revision = $Revision }
    $Revision = [uint64]$Paused.revision
    $AfterPause = Invoke-Api Get $SessionBase
    if ($AfterPause.session.capture_status -ne "paused" -or $AfterPause.session.segments.Count -ne 1) {
        throw "Synthetic pause did not finalize segment one."
    }

    $Refresh = Invoke-Api Post "$ModuleBase/artifacts/refresh" @{ expected_artifact_revision = 0 }
    $ArtifactRevision = [uint64]$Refresh.revision
    $NoteCreated = Invoke-Api Post "$SessionBase/notes/session" @{ title = "Session Notes"; expected_artifact_revision = $ArtifactRevision }
    $ArtifactRevision = [uint64]$NoteCreated.revision
    $UnicodeText = "caf$([char]0x00E9) $([char]0x2014) $([char]0x6771)$([char]0x4EAC) $([char]0xD83D)$([char]0xDE80)"
    $NoteText = "# Windows notes`n`nUnicode: $UnicodeText`n`nLiteral text: <script>alert('inert')</script>"
    $Saved = Invoke-Api Put "$SessionBase/notes/session" @{ content = $NoteText; expected_artifact_revision = $ArtifactRevision }
    $ArtifactRevision = [uint64]$Saved.revision
    $Loaded = Invoke-Api Get "$SessionBase/notes/session"
    if ($Loaded.content -cne $NoteText) {
        throw "Session note content did not round-trip exactly."
    }
    $UpdatedText = $NoteText + "`n`nSecond save after navigation-cancel coverage."
    $Saved = Invoke-Api Put "$SessionBase/notes/session" @{ content = $UpdatedText; expected_artifact_revision = $ArtifactRevision }
    if ($Saved.content -cne $UpdatedText) {
        throw "Updated session note content did not round-trip exactly."
    }

    $Resumed = Invoke-Api Post "$SessionBase/capture/resume" @{ expected_revision = $Revision }
    $Revision = [uint64]$Resumed.revision
    Invoke-Api Post "$SessionBase/capture/stop" @{ expected_revision = $Revision } | Out-Null
    $Stopped = Invoke-Api Get $SessionBase
    $Revision = [uint64]$Stopped.session.revision
    if ($Stopped.session.segments.Count -ne 2 -or $Stopped.session.capture_status -ne "stopped") {
        throw "Synthetic stop did not finalize two segments."
    }

    $WavFiles = @(Get-ChildItem -LiteralPath $WorkspaceRoot -Recurse -File -Filter "*-audio.wav" | Sort-Object FullName)
    if ($WavFiles.Count -ne 2) { throw "Expected exactly two finalized WAV files." }
    $BeforeHashes = @($WavFiles | ForEach-Object { (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash })
    foreach ($Segment in $Stopped.session.segments) {
        $Transcribed = Invoke-Api Post "$SessionBase/transcription/execute" @{
            segment_id = $Segment.id
            backend = "synthetic"
            model = "synthetic/deterministic"
            language = "en"
            max_attempts = 3
            expected_revision = $Revision
        }
        if (-not $Transcribed.completed) { throw "Synthetic transcription did not complete." }
        $Revision = [uint64]$Transcribed.runtime_revision
    }
    $AfterHashes = @($WavFiles | ForEach-Object { (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash })
    if (($BeforeHashes -join "|") -ne ($AfterHashes -join "|")) {
        throw "Transcription changed finalized source WAV content."
    }
    Invoke-Api Post "$SessionBase/complete" @{ expected_revision = $Revision } | Out-Null

    $Stage = "restarting and inspecting persistence"
    Stop-StudyPilotServer
    Start-StudyPilotServer
    $Restarted = Invoke-Api Get $SessionBase
    if ($Restarted.session.session_status -ne "completed" -or $Restarted.session.segments.Count -ne 2) {
        throw "Completed session or segments did not survive restart."
    }
    if (-not $Restarted.notes.session_exists) { throw "Session notes did not survive restart." }
    if (@($Restarted.capture.issues).Count -ne 0) { throw "Restart inspection reported false capture recovery." }
    if (@($Restarted.transcription.issues | Where-Object { $_.code -eq "runtime_job_missing_from_queue" }).Count -ne 0) {
        throw "Restart inspection reported a false transcription queue error."
    }
    $ReloadedNote = Invoke-Api Get "$SessionBase/notes/session"
    if ($ReloadedNote.content -cne $UpdatedText) { throw "Edited notes did not survive restart." }
    if (Get-ChildItem -LiteralPath $WorkspaceRoot -Recurse -File | Where-Object { $_.Name -like "*.partial" -or $_.Name -eq ".studypilot-capture.lock" }) {
        throw "Clean synthetic workflow left partial or capture ownership evidence."
    }

    Stop-StudyPilotServer
    $Released = $false
    try {
        $Client = New-Object Net.Sockets.TcpClient
        $Connect = $Client.BeginConnect("127.0.0.1", $Port, $null, $null)
        if (-not $Connect.AsyncWaitHandle.WaitOne(300)) { $Released = $true }
        elseif (-not $Client.Connected) { $Released = $true }
        $Client.Close()
    }
    catch { $Released = $true }
    if (-not $Released) { throw "Loopback port remained open after shutdown." }

    $Succeeded = $true
    Write-Host "PASS - native Windows synthetic GUI/API workflow persisted two segments, transcripts, and editable notes."
}
finally {
    if ($Server -and -not $Server.HasExited) {
        try { $Server.Kill(); $Server.WaitForExit(5000) | Out-Null } catch {}
    }
    Set-Location $RepoRoot
    if ($Succeeded) {
        Remove-Item -LiteralPath $EvidenceRoot -Recurse -Force
    }
    else {
        Write-Warning "Windows GUI workflow failed during '$Stage'. Evidence retained at: $EvidenceRoot"
    }
}
