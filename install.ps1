# Installs line-cli for the current Windows user, no administrator rights needed:
#
#   irm https://github.com/WellWells/line-cli/releases/latest/download/install.ps1 | iex
#
# The command is line: cmd\line.exe, the only folder put on PATH. It runs
# bin\linecli.exe, the program, which stays off PATH (so 'linec' + Tab offers
# nothing) and at the same path on every update (so a firewall rule for it
# keeps working).
#
# LINECLI_VERSION picks a release (default: the latest), LINECLI_INSTALL_DIR the
# folder that gets bin and cmd (default: %LOCALAPPDATA%\linecli), and
# LINECLI_DOWNLOAD_URL another place to get the release files from: a URL, or a
# folder such as the dist folder that "go run ./tools/dist" writes.
# LINECLI_SKILL_AGENTS picks the AI agents that get the line-cli skill without
# asking: agent ids separated by commas (claude-code,antigravity,...), or none.
# Keep this file ASCII: irm may not decode it as UTF-8.
& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    if ([Environment]::OSVersion.Platform -ne 'Win32NT') { throw 'line-cli runs on Windows only.' }
    if (-not [Environment]::Is64BitOperatingSystem) { throw 'line-cli needs 64-bit Windows.' }
    # Old .NET defaults lack TLS 1.2. SystemDefault (0) already has it and also
    # TLS 1.3; adding Tls12 to it would leave only TLS 1.2 for the whole session.
    if ([Net.ServicePointManager]::SecurityProtocol -ne 0) { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12 }

    $repo = 'WellWells/line-cli'
    $asset = 'line-cli_windows_amd64.zip'
    $root = if ($env:LINECLI_INSTALL_DIR) { $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($env:LINECLI_INSTALL_DIR.Trim()) } else { Join-Path $env:LOCALAPPDATA 'linecli' }
    if ($root.Length -gt 3) { $root = $root.TrimEnd('\', '/') }
    $bin = Join-Path $root 'bin'
    $cmdDir = Join-Path $root 'cmd'
    $exe = Join-Path $bin 'linecli.exe'
    $launcher = Join-Path $cmdDir 'line.exe'
    $version = if ($env:LINECLI_VERSION) { $env:LINECLI_VERSION.Trim() } else { 'latest' }
    if ($version -ne 'latest' -and $version -notlike 'v*') { $version = "v$version" }
    $src = if ($env:LINECLI_DOWNLOAD_URL) { $env:LINECLI_DOWNLOAD_URL.Trim().TrimEnd('/', '\') } elseif ($version -eq 'latest') { "https://github.com/$repo/releases/latest/download" } else { "https://github.com/$repo/releases/download/$version" }
    $web = $src -match '^https?://'
    if (-not $web) { $src = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($src) }

    function Get-ReleaseFile($name, $to) {
        if ($web) { Invoke-WebRequest "$src/$name" -OutFile $to -UseBasicParsing }
        else { Copy-Item -LiteralPath (Join-Path $src $name) -Destination $to }
    }

    # Install-Program copies a program into place. A running program cannot be
    # overwritten, but it can be renamed: the renamed copy is deleted here, or
    # by the next install if it is still running.
    function Install-Program($from, $to) {
        $folder = Split-Path -Parent $to
        $leaf = Split-Path -Leaf $to
        New-Item -ItemType Directory -Force -Path $folder | Out-Null
        Get-ChildItem -LiteralPath $folder -Filter "$leaf.*.old" -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue
        $old = $null
        if (Test-Path -LiteralPath $to) {
            $old = "$to.$([Guid]::NewGuid().ToString('N')).old"
            Move-Item -LiteralPath $to -Destination $old
        }
        try {
            Copy-Item -LiteralPath $from -Destination $to
        } catch {
            if ($old) { Move-Item -LiteralPath $old -Destination $to -Force }
            throw
        }
        if ($old) { Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue }
    }

    $first = -not (Test-Path -LiteralPath $exe)
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ('linecli-install-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        if ($web) { Write-Host "Downloading $asset ($version)" } else { Write-Host "Installing $asset from $src" }
        $zip = Join-Path $tmp $asset
        $sums = Join-Path $tmp 'checksums.txt'
        Get-ReleaseFile $asset $zip
        Get-ReleaseFile 'checksums.txt' $sums
        $line = Get-Content -LiteralPath $sums | Where-Object { $_ -match "^[0-9a-f]{64}  $([regex]::Escape($asset))$" } | Select-Object -First 1
        $expected = if ($line) { $line.Substring(0, 64) } else { '' }
        # .NET rather than Get-FileHash and Expand-Archive, which are missing when
        # Windows PowerShell inherits PowerShell 7's PSModulePath.
        $sha = [Security.Cryptography.SHA256]::Create()
        try { $actual = [BitConverter]::ToString($sha.ComputeHash([IO.File]::ReadAllBytes($zip))).Replace('-', '').ToLowerInvariant() } finally { $sha.Dispose() }
        if ($expected -ne $actual) { throw "Checksum mismatch for $asset; nothing was installed." }

        Add-Type -AssemblyName System.IO.Compression.FileSystem
        $files = Join-Path $tmp 'files'
        [IO.Compression.ZipFile]::ExtractToDirectory($zip, $files)
        $newExe = Join-Path $files 'bin\linecli.exe'
        $newLauncher = Join-Path $files 'cmd\line.exe'
        foreach ($f in $newExe, $newLauncher) {
            if (-not (Test-Path -LiteralPath $f)) { throw "$asset has no $($f.Substring($files.Length + 1)); nothing was installed." }
        }
        Install-Program $newExe $exe
        Install-Program $newLauncher $launcher
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }

    $installed = (& $launcher version) -replace '^line version '
    if ($LASTEXITCODE -ne 0 -or -not $installed) { throw "$launcher was installed but did not run." }

    # Put cmd on the user PATH and take bin off it (earlier installs put bin
    # there), editing the registry value directly to keep its type (usually
    # REG_EXPAND_SZ) and its %VARIABLE% entries, as
    # [Environment]::SetEnvironmentVariable would not.
    $hasCmd = $false
    $removed = $false
    $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment')
    try {
        $hasPath = $key.GetValueNames() -contains 'Path'
        $userPath = [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        $kept = @()
        foreach ($entry in @($userPath -split ';' | Where-Object { $_ })) {
            $full = [Environment]::ExpandEnvironmentVariables($entry).TrimEnd('\')
            if ($full -eq $bin) { $removed = $true; continue }
            if ($full -eq $cmdDir) { $hasCmd = $true }
            $kept += $entry
        }
        $added = -not $hasCmd
        if ($added) { $kept += $cmdDir }
        if ($added -or $removed) {
            $kind = if ($hasPath) { $key.GetValueKind('Path') } else { [Microsoft.Win32.RegistryValueKind]::ExpandString }
            $key.SetValue('Path', ($kept -join ';'), $kind)
        }
    } finally {
        $key.Close()
    }
    if ($added -or $removed) {
        # Tell Explorer the environment changed, so new terminals get the new PATH.
        try {
            if (-not ('LineCliInstall.Native' -as [type])) {
                Add-Type -Namespace LineCliInstall -Name Native -MemberDefinition '[DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint msg, UIntPtr wParam, string lParam, uint flags, uint timeout, out UIntPtr result);'
            }
            $result = [UIntPtr]::Zero
            [void][LineCliInstall.Native]::SendMessageTimeout([IntPtr]0xffff, 0x1a, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
        } catch { }
    }
    $sessionPath = @($env:Path -split ';' | Where-Object { $_ -and $_.TrimEnd('\') -ne $bin })
    if (($sessionPath | ForEach-Object { $_.TrimEnd('\') }) -notcontains $cmdDir) { $sessionPath += $cmdDir }
    $env:Path = $sessionPath -join ';'

    Write-Host "Installed line-cli $installed. The command is line ($launcher, which runs $exe)."

    # The line-cli skill teaches AI agents to use line. 'skill install --update'
    # asks which agents get it only the first time (a checklist, when a person is
    # at the console), saves the answer and keeps to it on later updates; every
    # run rewrites the skill where it is, so it always matches the program.
    $skillArgs = @('skill', 'install', '--compact')
    if ($env:LINECLI_SKILL_AGENTS) {
        $skillArgs += '--agent', $env:LINECLI_SKILL_AGENTS.Trim()
    } else {
        $skillArgs += '--update'
        # Nobody to answer the checklist: input from a pipe, or PowerShell
        # started with -NonInteractive (as AI agents and CI run it).
        $nonInteractive = @([Environment]::GetCommandLineArgs() | Where-Object { $_ -match '^[-/]noni' }).Count -gt 0
        if ($nonInteractive -or [Console]::IsInputRedirected) { $skillArgs += '--yes' }
    }
    $out = & $launcher @skillArgs
    if ($LASTEXITCODE -ne 0) {
        Write-Warning "Could not install the line-cli skill for AI agents: $out"
    } else {
        $rows = @($out | ConvertFrom-Json | ForEach-Object { $_ })
        $on = @($rows | Where-Object { 'installed', 'updated', 'unchanged' -contains $_.status })
        if ($on.Count) {
            Write-Host 'Installed the line-cli skill, which teaches AI agents to use line, for:'
            foreach ($r in $on) { Write-Host "  $($r.name)" }
        } else {
            Write-Host 'The line-cli skill is not installed for any AI agent.'
        }
        # One place failing does not stop the others; name what was not done.
        foreach ($r in @($rows | Where-Object { $_.status -eq 'failed' })) {
            Write-Warning "Could not update the line-cli skill for $($r.name): $($r.note)"
        }
        foreach ($r in @($rows | Where-Object { $_.status -eq 'conflict' })) {
            Write-Warning "Left $($r.path) alone: it is another skill named line-cli, so $($r.name) does not get this one."
        }
        Write-Host 'To choose the agents again: line skill install'
    }
    if ($added) { Write-Host "Added $cmdDir to your user PATH. Terminals opened from now on will find line." }
    if ($removed) { Write-Host "Took $bin off your user PATH: the command is now line, not linecli." }
    $found = Get-Command line.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($found -and $found.Path -ne $launcher) { Write-Warning "$($found.Path) comes before $cmdDir on PATH, so 'line' runs that program instead. Remove it, or move $cmdDir earlier in PATH." }
    foreach ($other in @(Get-Command linecli.exe -CommandType Application -All -ErrorAction SilentlyContinue)) {
        Write-Warning "$($other.Path) is on PATH, so 'linec' + Tab still offers linecli. Remove it, or take its folder off PATH; the command is line."
    }
    if ($first) { Write-Host "send and download connect to LINE's servers. If your firewall blocks programs until you allow them, allow $exe once (line.exe never connects). Updates install to the same path, so the rule keeps working." }
}
