#!/bin/sh
set -e

# Fix ownership of the data directory so the non-root klipbord user can
# write to it. This is needed for bind mounts where the host directory
# may be owned by root (named volumes get correct ownership automatically).
if [ -d /data ]; then
    chown -R 10001:10001 /data
fi

# Drop privileges and run the configured command as the klipbord user.
exec su-exec 10001:10001 "$@"
