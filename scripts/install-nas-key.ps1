# One-time per PC: installs this PC's Aetheris SSH key on the NAS. You type the NAS password at the prompt.
# -Nas is the NAS's LAN address (default: the host of AETHERIS_NAS_SSH in .dev/nas.env).
# Default user comes from .env; pass -User admin if QTS only lets administrators use SSH.
param([string]$Nas, [string]$User)
$ErrorActionPreference = 'Stop'
$key = "$env:USERPROFILE\.ssh\aetheris_nas.pub"
if (-not $Nas) {
    $line = Get-Content "$PSScriptRoot\..\.dev\nas.env" -ErrorAction SilentlyContinue | Where-Object { $_ -like 'AETHERIS_NAS_SSH=*' }
    if (-not $line) { throw 'Pass -Nas <LAN address>, or put AETHERIS_NAS_SSH=user@address in .dev\nas.env' }
    $Nas = ($line -split '@', 2)[1].Trim()
}
if (-not $User) { $User = ((Get-Content "$PSScriptRoot\..\.env" | Where-Object { $_ -like 'AETHERIS_LOCAL_LOGIN_USERNAME=*' }) -split '=', 2)[1].Trim() }
$user = $User

# from= makes the key useless outside the home LAN (the NAS's /24)
$lan = ($Nas -split '\.')[0..2] -join '.'
$line = "from=`"$lan.*`" " + (Get-Content $key -Raw).Trim()
$line | ssh -o StrictHostKeyChecking=accept-new "$user@$Nas" 'umask 077; mkdir -p ~/.ssh && tr -d \\r >> ~/.ssh/authorized_keys && chmod 700 ~/.ssh && chmod 600 ~/.ssh/authorized_keys && chmod go-w ~ && echo key installed for $(whoami) in $HOME'
