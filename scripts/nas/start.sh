#!/bin/sh
# Starts the Aetheris server in the background as whoever runs it (the aetheris user: via the root
# watchdog's start-stop-daemon, or scripts/deploy-nas.sh). Does nothing if it already runs.
cd "$(dirname "$0")" || exit 1
pidof aetheris >/dev/null && exit 0
[ -f aetheris.log ] && [ "$(wc -c < aetheris.log)" -gt 5242880 ] && mv -f aetheris.log aetheris.log.1 # keep ≤ 10 MB of logs
umask 002 # files Aetheris creates stay editable for SMB users of the share
setsid ./aetheris serve -data /share/Aetheris -state /share/homes/aetheris/.aetheris </dev/null >>aetheris.log 2>&1 &
