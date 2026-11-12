#!/bin/sh
set -eu

config_path=${CONFIG_PATH:-/data/config.json}
config_dir=$(dirname "$config_path")

if ! mkdir -p "$config_dir"; then
    echo "cannot create configuration directory $config_dir; check the mounted volume" >&2
    exit 1
fi
if ! chown app:app "$config_dir"; then
    echo "cannot set ownership on $config_dir; check that the mounted volume permits ownership changes" >&2
    exit 1
fi
if [ -e "$config_path" ]; then
    if ! chown app:app "$config_path"; then
        echo "cannot set ownership on $config_path; check that the mounted volume permits ownership changes" >&2
        exit 1
    fi
fi

exec su-exec app "$@"
