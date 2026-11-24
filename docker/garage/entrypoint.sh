#!/bin/sh
set -eu

ready=/var/lib/garage/meta/biletflow-ready

garage server &
server=$!
trap 'kill -TERM "$server"' TERM INT

until garage status >/dev/null 2>&1; do sleep 1; done

if [ ! -f "$ready" ]; then
  node=$(garage node id -q | cut -d@ -f1)
  garage layout assign -z dc1 -c 1G "$node"
  garage layout apply --version 1
  garage key import --yes -n biletflow "$S3_ACCESS_KEY_ID" "$S3_SECRET_ACCESS_KEY"
  garage bucket create "$S3_BUCKET"
  garage bucket allow --read --write "$S3_BUCKET" --key biletflow
  touch "$ready"
fi

wait "$server"
