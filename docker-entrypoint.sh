#!/bin/sh
set -e

# Adapted from Switch Library Manager Web by the same author.
# Run the app as an unprivileged user (PUID/PGID, default 1000) instead of root.
# When the container is started with --user, it already runs unprivileged.
if [ "$(id -u)" = "0" ]; then
	# Give the data folder to the app. Files mounted read-only into it and other mounts are
	# left alone. The libraries are never changed here.
	find "$COMPLETIST_DATA_DIR" -xdev \( ! -user "$PUID" -o ! -group "$PGID" \) \
		-exec chown "$PUID:$PGID" {} + 2>/dev/null || true

	if ! su-exec "$PUID:$PGID" test -w "$COMPLETIST_DATA_DIR"; then
		echo "The data folder $COMPLETIST_DATA_DIR is not writable by user $PUID:$PGID." >&2
		echo "Set PUID/PGID to the owner of the folder on the host, or change its permissions." >&2
		exit 1
	fi

	if [ "$PUID" = "0" ]; then
		echo "Warning: PUID=0 runs the app as root. Set PUID/PGID to an unprivileged user (on Unraid 99/100)." >&2
	fi
	exec su-exec "$PUID:$PGID" env HOME="$COMPLETIST_DATA_DIR" "$@"
fi

exec "$@"
