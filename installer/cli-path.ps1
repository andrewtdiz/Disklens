param([Parameter(Mandatory)][string]$Directory, [switch]$Remove)
$ErrorActionPreference = 'Stop'
$Directory = [IO.Path]::GetFullPath($Directory).TrimEnd('\')
$environment = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment')
try {
    $path = $environment.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    $entries = @($path -split ';' | Where-Object { $_ })
    $matches = @($entries | Where-Object { [Environment]::ExpandEnvironmentVariables($_).Replace('/', '\').TrimEnd('\') -ieq $Directory })
    if ($Remove) {
        if ($matches.Count -eq 0) { return }
        $entries = @($entries | Where-Object { [Environment]::ExpandEnvironmentVariables($_).Replace('/', '\').TrimEnd('\') -ine $Directory })
    } else {
        if ($matches.Count -gt 0) { return }
        $entries += $Directory
    }
    $environment.SetValue('Path', ($entries -join ';'), [Microsoft.Win32.RegistryValueKind]::ExpandString)
} finally { $environment.Dispose() }
