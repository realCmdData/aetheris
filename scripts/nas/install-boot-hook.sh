#!/bin/sh
# One-time, run AS ADMIN on the NAS (you type the admin password):
#   cmd /c "ssh admin@<nas-lan-address> sh -s < scripts\nas\install-boot-hook.sh"
#
# Installs a root-owned watchdog that QTS's persistent root crontab runs every minute. It starts the
# Aetheris server as the aetheris user whenever it isn't running: after boot (the data volume mounts
# late, so it simply waits) and after a crash. Root never executes Aetheris code: start-stop-daemon
# switches user before exec. Also closes the world-writable root crontab found in P6.
set -e
[ "$(id -u)" = 0 ] || { echo "run this as admin"; exit 1; }

cat > /etc/config/aetheris-watchdog.sh <<'WATCHDOG'
#!/bin/sh
# Aetheris watchdog (root crontab, every minute). Installed by scripts/nas/install-boot-hook.sh.
APP=/share/homes/aetheris/aetheris
# QTS rewrites this file as rw-rw-rw- (seen after a reboot), which lets any local account run commands as root.
chmod 644 /etc/config/crontab
/bin/pidof aetheris >/dev/null && exit 0
[ -x "$APP/aetheris" ] && [ -d /share/Aetheris/ ] || exit 0
# start-stop-daemon is only a BusyBox applet on QTS: there is no command of that name on cron's PATH.
exec /bin/busybox start-stop-daemon -S -b -m -p /var/run/aetheris.pid -c aetheris -x /bin/sh -- -c \
  "cd $APP && sh ./start.sh"
WATCHDOG
chown 0:0 /etc/config/aetheris-watchdog.sh
chmod 755 /etc/config/aetheris-watchdog.sh

LINE='* * * * * /etc/config/aetheris-watchdog.sh'
grep -qF "$LINE" /etc/config/crontab || echo "$LINE" >> /etc/config/crontab
chmod 644 /etc/config/crontab # was rw-rw-rw-: any local account could have run commands as root (the watchdog keeps it so)
crontab /etc/config/crontab
/etc/init.d/crond.sh restart >/dev/null 2>&1
echo "Aetheris watchdog installed:"
ls -l /etc/config/aetheris-watchdog.sh /etc/config/crontab
grep aetheris /etc/config/crontab
