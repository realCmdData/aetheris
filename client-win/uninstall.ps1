# Removes Aetheris from this PC. Settings > Apps > Installed apps runs this (see Uninstall.cs).
# The app does its part first: asks, quits, signs this PC out, turns the folder back into a normal folder and
# deletes its data. This script then removes what the app can't remove from inside its own package: the package
# and the program files. It runs outside the package on purpose: removing a package ends the package's processes.
param([switch]$Quiet)
# Only the installed copy: a copy in a build folder would still uninstall the real profile, then keep its files.
if ($PSScriptRoot -ne (Join-Path $env:LOCALAPPDATA 'Programs\Aetheris')) { Write-Error 'Run the uninstaller of the installed copy.'; exit 2 }
$words = if ($Quiet) { 'uninstall', '--quiet' } else { 'uninstall' }
$app = Start-Process (Join-Path $PSScriptRoot 'Aetheris.exe') -ArgumentList $words -Wait -PassThru
if ($app.ExitCode -ne 0) { exit $app.ExitCode } # cancelled, or it failed: keep everything

Get-AppxPackage -Name 'Aetheris.Drive' | Remove-AppxPackage
Remove-Item -LiteralPath $PSScriptRoot -Recurse -Force -ErrorAction SilentlyContinue
$data = Join-Path $env:LOCALAPPDATA 'Aetheris'
Remove-Item -LiteralPath (Join-Path $data 'nas') -Recurse -Force -ErrorAction SilentlyContinue # the app tried; now nothing holds it
# The shared WebView2 cache goes with the last profile.
if (-not (Get-ChildItem -LiteralPath $data -Directory -ErrorAction SilentlyContinue | Where-Object Name -ne 'webview')) {
    Remove-Item -LiteralPath $data -Recurse -Force -ErrorAction SilentlyContinue
}
