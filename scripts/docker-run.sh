#!/bin/sh
set -eu

LEDGER_ENV_FILE=${LEDGER_ENV_FILE:-.env}
case "$LEDGER_ENV_FILE" in
    \~/*) LEDGER_ENV_FILE="$HOME/${LEDGER_ENV_FILE#\~/}" ;;
esac
if [ ! -f "$LEDGER_ENV_FILE" ]; then
    echo "App configuration file does not exist: $LEDGER_ENV_FILE" >&2
    exit 1
fi
config_dir=$(cd -- "$(dirname -- "$LEDGER_ENV_FILE")" && pwd)
LEDGER_ENV_FILE="$config_dir/$(basename -- "$LEDGER_ENV_FILE")"

# Read the token path as literal data, matching the app's KEY=value syntax.
# Never source .env: its contents must not execute shell commands.
if [ "${GOOGLE_TOKEN_FILE+x}" != x ]; then
    GOOGLE_TOKEN_FILE=$(awk '
        function trim(value) {
            sub(/^[[:space:]]+/, "", value)
            sub(/[[:space:]]+$/, "", value)
            return value
        }
        {
            entry = trim($0)
            separator = index(entry, "=")
            if (trim(substr(entry, 1, separator - 1)) != "GOOGLE_TOKEN_FILE") next
            value = trim(substr(entry, separator + 1))
            quote = substr(value, 1, 1)
            if (quote == "\"" || quote == sprintf("%c", 39)) {
                if (length(value) < 2 || substr(value, length(value), 1) != quote) exit 1
                value = substr(value, 2, length(value) - 2)
            }
            token = value
        }
        END { print token }
    ' "$LEDGER_ENV_FILE")
fi
if [ -z "$GOOGLE_TOKEN_FILE" ]; then
    echo "Set GOOGLE_TOKEN_FILE in the app configuration or environment." >&2
    exit 1
fi
case "$GOOGLE_TOKEN_FILE" in
    \~/*) GOOGLE_TOKEN_FILE="$HOME/${GOOGLE_TOKEN_FILE#\~/}" ;;
    /*) ;;
    *) GOOGLE_TOKEN_FILE="$config_dir/$GOOGLE_TOKEN_FILE" ;;
esac
if [ ! -f "$GOOGLE_TOKEN_FILE" ]; then
    echo "Google token file does not exist: $GOOGLE_TOKEN_FILE" >&2
    exit 1
fi
export GOOGLE_TOKEN_FILE LEDGER_ENV_FILE
LOCAL_UID=$(id -u)
LOCAL_GID=$(id -g)
export LOCAL_UID LOCAL_GID
exec docker compose --env-file /dev/null up --build
