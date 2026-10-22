#!/bin/sh
# myfork [folder]... opens the Git repositories in MyFork.
# Installed by MyFork: MyFork > Install `myfork` CLI.
case $1 in
-h | --help)
    echo "Usage: myfork [<path>...]"
    echo
    echo "Open the Git repositories at <path> (default: the ones of the last run) in"
    echo "tabs."
    exit 0
    ;;
esac
url=
for p in "$@"; do
    case $p in
    -psn_*) continue ;;
    esac
    if [ -d "$p" ]; then
        p=$(cd -- "$p" && pwd) || continue
    else
        case $p in
        /*) ;;
        *) p=$PWD/$p ;;
        esac
    fi
    p=$(printf %s "$p" | perl -pe 's/([^A-Za-z0-9._~\/-])/sprintf("%%%02X", ord($1))/ge')
    if [ -z "$url" ]; then url="myfork://open?path=$p"; else url="$url&path=$p"; fi
done
for id in com.develiott.myfork.dev com.develiott.myfork; do
    open -b "$id" ${url:+"$url"} 2>/dev/null && exit 0
done
echo "myfork: MyFork is not installed" >&2
exit 1
