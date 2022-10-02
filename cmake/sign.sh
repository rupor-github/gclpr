#!/bin/bash
set -e
# gopass show -o build/minisign
passage show build | minisign -S -s ${HOME}/.minisign/build.key -c "gclpr for ${1} release signature" -m gclpr_${1}.zip
if [[ ${1} == win* ]]; then
   sed -i -e "s/__CURRENT_HASH_${1}__/$(sha256sum -z gclpr_${1}.zip | awk '{ print $1; }')/g" gclpr.json
fi
