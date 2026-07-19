#requires -Version 7.0

[CmdletBinding()]
param([string]$FfmpegPath = "")

$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($FfmpegPath)) {
    $Command = Get-Command ffmpeg.exe -ErrorAction SilentlyContinue
    if (-not $Command) {
        $Command = Get-Command ffmpeg -ErrorAction SilentlyContinue
    }
    if (-not $Command) {
        throw "ffmpeg.exe was not found. Supply -FfmpegPath with an exact trusted executable."
    }
    $FfmpegPath = $Command.Source
}
$FfmpegPath = (Resolve-Path -LiteralPath $FfmpegPath).Path
if (-not (Test-Path -LiteralPath $FfmpegPath -PathType Leaf) -or
    -not [string]::Equals([IO.Path]::GetFileName($FfmpegPath), "ffmpeg.exe", [StringComparison]::OrdinalIgnoreCase)) {
    throw "FfmpegPath must resolve to ffmpeg.exe."
}

$Arguments = @("-hide_banner", "-list_devices", "true", "-f", "dshow", "-i", "dummy")
$PreviousPreference = $ErrorActionPreference
try {
    # PowerShell 5.1 promotes native stderr to ErrorRecord objects when Stop is
    # active. Enumeration intentionally writes to stderr and exits nonzero.
    $ErrorActionPreference = "Continue"
    $RawLines = @(& $FfmpegPath @Arguments 2>&1 | ForEach-Object { [string]$_ })
}
finally {
    $ErrorActionPreference = $PreviousPreference
}
# FFmpeg normally exits nonzero after enumeration because dummy is not a real input.
$BoundedLines = @($RawLines | Select-Object -Last 400)
$RawText = ($BoundedLines -join [Environment]::NewLine)
if ($RawText.Length -gt 65536) {
    $RawText = $RawText.Substring($RawText.Length - 65536)
}

$Devices = New-Object System.Collections.Generic.List[object]
$Current = $null
foreach ($Line in ($RawText -split "\r?\n")) {
    if ($Line -match '"([^"]+)"\s+\(audio\)') {
        $Current = [pscustomobject]@{ Name = $Matches[1]; AlternativeName = "" }
        $Devices.Add($Current)
        continue
    }
    if ($Current -and $Line -match 'Alternative name\s+"([^"]+)"') {
        $Current.AlternativeName = $Matches[1]
        $Current = $null
    }
}

if ($Devices.Count -eq 0) {
    Write-Warning "No audio names were parsed. FFmpeg device-list formatting is version-sensitive."
    Write-Host "Bounded raw DirectShow device-list output:"
    Write-Host $RawText
    exit 1
}
Write-Host "DirectShow audio devices (no device was selected and no recording occurred):"
for ($Index = 0; $Index -lt $Devices.Count; $Index++) {
    Write-Host ("[{0}] {1}" -f ($Index + 1), $Devices[$Index].Name)
    if ($Devices[$Index].AlternativeName) {
        Write-Host ("    Alternative DirectShow name: {0}" -f $Devices[$Index].AlternativeName)
    }
}
Write-Host "Copy the exact friendly or alternative name when configuring StudyPilot."
