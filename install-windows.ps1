param([string]$InstallDirectory = (Join-Path $env:LOCALAPPDATA 'Programs/Disklens'))
$ErrorActionPreference = 'Stop'
$goCommand = Get-Command go -ErrorAction SilentlyContinue
$goBinary = if ($goCommand) { $goCommand.Source } else { Join-Path $env:USERPROFILE 'go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.3.windows-amd64/bin/go.exe' }
if (-not (Test-Path -LiteralPath $goBinary)) { throw 'Install Go 1.27.3 or later.' }
$packageDirectory = Join-Path $PSScriptRoot 'build/install-windows'
$packageCLI = Join-Path $packageDirectory 'cli'
$null = New-Item -ItemType Directory -Force -Path $packageCLI
Push-Location $PSScriptRoot
try {
    & (Join-Path $PSScriptRoot 'build-resources.ps1')
    & $goBinary build -trimpath -tags mygo_noinspector -ldflags '-H=windowsgui' -o (Join-Path $packageDirectory 'Disklens.exe') ./app
    if ($LASTEXITCODE -ne 0) { throw 'Desktop build failed.' }
    & $goBinary build -trimpath -tags cli -o (Join-Path $packageCLI 'disklens.exe') ./app
    if ($LASTEXITCODE -ne 0) { throw 'CLI build failed.' }
} finally { Pop-Location }
$installRoot = [IO.Path]::GetFullPath($InstallDirectory)
$desktopBinary = Join-Path $installRoot 'Disklens.exe'
$cliDirectory = Join-Path $installRoot 'cli'
$cliBinary = Join-Path $cliDirectory 'disklens.exe'
$running = Get-Process -Name 'Disklens' -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $desktopBinary -or $_.Path -eq $cliBinary }
if ($running) { throw 'Close the installed Disklens application before updating it.' }
$null = New-Item -ItemType Directory -Force -Path $cliDirectory
Copy-Item -LiteralPath (Join-Path $packageDirectory 'Disklens.exe') -Destination $desktopBinary -Force
Copy-Item -LiteralPath (Join-Path $packageCLI 'disklens.exe') -Destination $cliBinary -Force
$shell = New-Object -ComObject WScript.Shell
foreach ($shortcutDirectory in @([Environment]::GetFolderPath('Programs'), [Environment]::GetFolderPath('DesktopDirectory'))) {
    $shortcut = $shell.CreateShortcut((Join-Path $shortcutDirectory 'Disklens.lnk'))
    $shortcut.TargetPath = $desktopBinary
    $shortcut.WorkingDirectory = $installRoot
    $shortcut.Description = 'Disk usage explorer'
    $shortcut.Save()
}
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$pathEntries = @($userPath -split ';' | Where-Object { $_ })
if ($pathEntries -notcontains $cliDirectory) {
    [Environment]::SetEnvironmentVariable('Path', (($pathEntries + $cliDirectory) -join ';'), 'User')
}
if (($env:PATH -split ';') -notcontains $cliDirectory) { $env:PATH += ';' + $cliDirectory }
& $cliBinary --help
if ($LASTEXITCODE -ne 0) { throw 'Installed CLI verification failed.' }
Write-Output "Installed desktop: $desktopBinary"
Write-Output "Installed CLI: $cliBinary"
Write-Output 'Start a new terminal to use disklens diff.'
