#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$root"

version=${1:-$(git describe --tags --always --dirty)}
package_version=${version#v}
output="dist/simplefsmanager_${package_version}_amd64.deb"

echo "Building SimpleFSManager ${version}"
make build VERSION="$version"

echo "Creating ${output}"
packaging/debian/build.sh "$version" simplefsmanager "$output"

dpkg-deb --info "$output" | awk '/Package:|Version:|Architecture:/'
ls -lh "$output"
echo "Package created: $output"
