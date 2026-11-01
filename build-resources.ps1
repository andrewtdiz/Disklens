$ErrorActionPreference = 'Stop'
$compiler = (Get-Command windres -ErrorAction Stop).Source
Push-Location $PSScriptRoot
try {
    & $compiler -F pe-x86-64 -i assets/windows.rc -o icon_windows_amd64.syso
    if ($LASTEXITCODE -ne 0) { throw 'Windows icon resource compilation failed.' }
} finally { Pop-Location }
