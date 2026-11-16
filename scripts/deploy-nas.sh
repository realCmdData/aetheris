#!/usr/bin/env bash
# Builds the server for the NAS and installs it with the previous binary kept for rollback, restarts it,
# and checks it answers on the LAN. If the new build doesn't come up, the old one is put back.
# Run from Git Bash in the repo:  scripts/deploy-nas.sh
# The server: AETHERIS_NAS_SSH=user@lan-address and AETHERIS_NAS_HOST=<its tailnet name>, from the environment or
# from .dev/nas.env (git-ignored, two lines of NAME=value). The server's web address is not set here: it lives in its
# state folder (aetheris setup -origins).
set -euo pipefail
cd "$(dirname "$0")/.."
command -v go >/dev/null || export PATH="$PATH:/c/Program Files/Go/bin"
if [ -z "${AETHERIS_NAS_SSH:-}" ] && [ -f .dev/nas.env ]; then . ./.dev/nas.env; fi
NAS=${AETHERIS_NAS_SSH:?set AETHERIS_NAS_SSH=user@lan-address, or put it in .dev/nas.env}
KEY=(-i "$HOME/.ssh/aetheris_nas" -o IdentitiesOnly=yes -o BatchMode=yes)
HOST=${AETHERIS_NAS_HOST:?set AETHERIS_NAS_HOST=aetheris.<tailnet>.ts.net, or put it in .dev/nas.env}

if [ -n "${AETHERIS_BINARY:-}" ]; then # install a given file instead of building (e.g. to test the rollback)
  version="custom ($AETHERIS_BINARY)"
  cp "$AETHERIS_BINARY" .dev/aetheris-linux
else
  version=$(git describe --always --dirty)
  echo "building $version"
  rm -f .dev/aetheris-linux # go build won't overwrite a non-binary left there by an AETHERIS_BINARY run
  (cd server && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$version" -o ../.dev/aetheris-linux ./cmd/aetheris)
fi

ssh "${KEY[@]}" $NAS 'mkdir -p ~/aetheris'
scp -q "${KEY[@]}" .dev/aetheris-linux $NAS:aetheris/aetheris.new
scp -q "${KEY[@]}" scripts/nas/start.sh $NAS:aetheris/start.sh

swap() { # $1 = new binary name to put in place; restarts the server
  ssh "${KEY[@]}" $NAS sh -s <<EOF
cd ~/aetheris
chmod +x $1 start.sh
mv -f $1 aetheris
pid=\$(pidof aetheris) && kill \$pid && for i in 1 2 3 4 5 6 7 8 9 10; do pidof aetheris >/dev/null || break; sleep 1; done
sh ./start.sh
EOF
}

healthy() { # the LAN listener answers (the tailnet node may take a few more seconds)
  for _ in $(seq 1 30); do
    curl -sf -m 3 --resolve $HOST:8443:${NAS#*@} https://$HOST:8443/api/v1/auth/status >/dev/null && return 0
    sleep 2
  done
  return 1
}

ssh "${KEY[@]}" $NAS 'cd ~/aetheris && [ -f aetheris ] && cp -p aetheris aetheris.prev || true'
swap aetheris.new
if healthy; then
  echo "deployed $version"
  ssh "${KEY[@]}" $NAS 'grep -o "\"msg\":\"serving\",\"version\":\"[^\"]*\"" ~/aetheris/aetheris.log | tail -1'
else
  echo "new build did not answer: rolling back" >&2
  ssh "${KEY[@]}" $NAS 'cd ~/aetheris && tail -5 aetheris.log; cp -p aetheris.prev aetheris.rollback' >&2
  swap aetheris.rollback
  healthy && echo "rolled back to the previous build" >&2
  exit 1
fi
