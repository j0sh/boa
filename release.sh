#!/bin/sh

set -e
REMOTE=${1:?Usage: ./release.sh <remote>}
read -r VERSION < VERSION

git tag -a "v$VERSION" -m "Release v$VERSION"
git push "$REMOTE" "v$VERSION"
