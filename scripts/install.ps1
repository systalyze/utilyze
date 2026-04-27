$ErrorActionPreference = 'Stop'

$BinName = 'utlz'
$AltBinName = 'utilyze'
$Version = if ($env:UTLZ_VERSION) { $env:UTLZ_VERSION } else { 'latest' }

if ($env:UTLZ_INSTALL_DIR) {
    $InstallDir = $env:UTLZ_INSTALL_DIR
} elseif ($env:LOCALAPPDATA) {
    $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\utilyze\bin'
} elseif ($env:USERPROFILE) {
    $InstallDir = Join-Path $env:USERPROFILE '.utilyze\bin'
} else {
    throw "LOCALAPPDATA and USERPROFILE are not set. Set UTLZ_INSTALL_DIR to the directory you want to install $BinName to."
}

$ProcessorArch = if ($env:PROCESSOR_ARCHITEW6432) {
    $env:PROCESSOR_ARCHITEW6432
} else {
    $env:PROCESSOR_ARCHITECTURE
}

switch ($ProcessorArch) {
    'AMD64' { $Arch = 'amd64' }
    'ARM64' { $Arch = 'arm64' }
    default { throw "Unsupported architecture: $ProcessorArch" }
}

function Normalize-PathEntry {
    param([string]$PathEntry)

    try {
        return [System.IO.Path]::GetFullPath($PathEntry.Trim('"')).TrimEnd('\', '/')
    } catch {
        return $null
    }
}

function Test-DirectoryOnPath {
    param([string]$Directory)

    $normalizedDirectory = Normalize-PathEntry $Directory
    if (-not $normalizedDirectory) {
        return $false
    }

    foreach ($entry in ($env:PATH -split ';')) {
        if ([string]::IsNullOrWhiteSpace($entry)) {
            continue
        }

        $normalizedEntry = Normalize-PathEntry $entry
        if ($normalizedEntry -and [string]::Equals($normalizedEntry, $normalizedDirectory, [System.StringComparison]::OrdinalIgnoreCase)) {
            return $true
        }
    }

    return $false
}

function Add-DirectoryToUserPath {
    param([string]$Directory)

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $entries = @()
    if ($userPath) {
        $entries = $userPath -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    }

    $normalizedDirectory = Normalize-PathEntry $Directory
    foreach ($entry in $entries) {
        $normalizedEntry = Normalize-PathEntry $entry
        if ($normalizedEntry -and [string]::Equals($normalizedEntry, $normalizedDirectory, [System.StringComparison]::OrdinalIgnoreCase)) {
            return
        }
    }

    $entries += $Directory
    [Environment]::SetEnvironmentVariable('Path', ($entries -join ';'), 'User')
}

$AssetName = "$BinName-windows-$Arch.exe"
if ($Version -eq 'latest') {
    $Url = "https://github.com/systalyze/utilyze/releases/latest/download/$AssetName"
} else {
    $Url = "https://github.com/systalyze/utilyze/releases/download/$Version/$AssetName"
}

$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) "utlz-install-$([System.Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $TempDir -Force | Out-Null

try {
    $DownloadedBinary = Join-Path $TempDir "$BinName.exe"
    $InstalledBinary = Join-Path $InstallDir "$BinName.exe"
    $InstalledAltBinary = Join-Path $InstallDir "$AltBinName.exe"

    Write-Host "Downloading $AssetName..."
    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    } catch {
        # best effort for older Windows PowerShell defaults.
    }
    Invoke-WebRequest -Uri $Url -OutFile $DownloadedBinary -UseBasicParsing

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Copy-Item -LiteralPath $DownloadedBinary -Destination $InstalledBinary -Force
    Copy-Item -LiteralPath $DownloadedBinary -Destination $InstalledAltBinary -Force

    $InstalledVersion = $Version
    try {
        $VersionOutput = & $InstalledBinary -version 2>$null
        if ($LASTEXITCODE -eq 0 -and $VersionOutput) {
            $InstalledVersion = ($VersionOutput | Select-Object -First 1)
        }
    } catch {
    }

    Write-Host "Installed $BinName and $AltBinName to $InstallDir ($InstalledVersion)"

    $InstallDirOnPath = Test-DirectoryOnPath $InstallDir
    $CanPrompt = [Environment]::UserInteractive -and -not [Console]::IsInputRedirected -and -not $env:UTLZ_INSTALL_WITHOUT_PATH
    if (-not $InstallDirOnPath -and $CanPrompt) {
        $Answer = Read-Host "Would you like to add $InstallDir to your user PATH? [y/N]"
        if ($Answer -match '^(y|yes)$') {
            Add-DirectoryToUserPath $InstallDir
            $env:PATH = "$env:PATH;$InstallDir"
            $InstallDirOnPath = $true
            Write-Host "Added $InstallDir to your user PATH. Restart open terminals to pick up the change."
        }
    }

    if ($InstallDirOnPath) {
        $RunHint = $BinName
    } else {
        Write-Warning "Add $InstallDir to PATH to run $BinName from any terminal."
        $RunHint = $InstalledBinary
    }

    Write-Host "Successfully installed. Start monitoring your GPUs by running '$RunHint'."
} finally {
    Remove-Item -LiteralPath $TempDir -Recurse -Force -ErrorAction SilentlyContinue
}
