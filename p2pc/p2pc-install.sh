#!/bin/bash

installServer() {
  if [ ! -d /home/p2pc ]; then
    useradd p2pc
    mkdir -p /home/p2pc
    chown -R p2pc:p2pc /home/p2pc
  fi
  cp -rf * /home/p2pc/srv/
  if [ ! -f /etc/systemd/system/p2pc.service ]; then
    cp -f p2pc.service /etc/systemd/system/
  fi
  systemctl enable p2pc.service
}

case "$1" in
-i)
  installServer
  ;;
*)
  echo "Usage: ./p2pc-install.sh -i"
  ;;
esac
