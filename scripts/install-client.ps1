# Builds the Windows client as a release does (one self-contained exe, client-win/Setup.cs) and runs it: it quits the
# running copy, installs itself for this user at %LOCALAPPDATA%\Programs\Aetheris and starts with its window open.
# A local build is version 0.0.0, so it never updates itself; releases come from .github/workflows/release.yml.
$ErrorActionPreference = 'Stop'
$out = Join-Path $PSScriptRoot '..\client-win\bin\setup'

# The app window's interface (Svelte, in web/) is built into client-win/ui and carried inside the exe.
pnpm.cmd --dir "$PSScriptRoot\..\web" build:desktop
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

dotnet publish "$PSScriptRoot\..\client-win" -c Release -o $out --nologo -v q
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$setup = Start-Process (Join-Path $out 'Aetheris.exe') -Wait -PassThru
if ($setup.ExitCode -ne 0) { exit $setup.ExitCode }
"Installed and started $env:LOCALAPPDATA\Programs\Aetheris\Aetheris.exe"
