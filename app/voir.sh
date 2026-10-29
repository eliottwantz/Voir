#!/bin/sh
# voir [folder]... opens the Git repositories in Voir.
# Installed by Voir: Voir > Install `voir` CLI.
case $1 in
-h | --help)
    echo "Usage: voir [<path>...]"
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
    if [ -z "$url" ]; then url="voir://open?path=$p"; else url="$url&path=$p"; fi
done
for id in com.develiott.voir.dev com.develiott.voir; do
    open -b "$id" ${url:+"$url"} 2>/dev/null && exit 0
done
echo "voir: Voir is not installed" >&2
exit 1
