#!/bin/sh
set -eu

if [ "$#" -ne 3 ]; then
    echo "usage: $0 VERSION BINARY OUTPUT" >&2
    exit 2
fi

version=${1#v}
binary=$2
output=$3

case "$version" in
    '' | *[!0-9A-Za-z.+:~\-]*)
        echo "invalid Debian package version: $version" >&2
        exit 2
        ;;
esac

if [ ! -x "$binary" ]; then
    echo "binary is not executable: $binary" >&2
    exit 2
fi

staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT HUP INT TERM

mkdir -p "$(dirname "$output")"
install -D -m 0755 "$binary" "$staging/usr/bin/simplefsmanager"
install -D -m 0644 deploy/simplefsmanager.service "$staging/lib/systemd/system/simplefsmanager.service"
install -D -m 0644 deploy/simplefsmanager.pam "$staging/etc/pam.d/simplefsmanager"
install -D -m 0755 packaging/debian/DEBIAN/postinst "$staging/DEBIAN/postinst"
install -D -m 0755 packaging/debian/DEBIAN/prerm "$staging/DEBIAN/prerm"
install -D -m 0755 packaging/debian/DEBIAN/postrm "$staging/DEBIAN/postrm"
sed "s/@VERSION@/$version/g" packaging/debian/DEBIAN/control > "$staging/DEBIAN/control"

dpkg-deb --root-owner-group --build "$staging" "$output"
