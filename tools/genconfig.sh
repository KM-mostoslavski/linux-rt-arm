#!/bin/bash
# Regenerate ./config from ALARM's linux-aarch64 config + config.rt-fragment.
# Dev tool only (runs on an x86_64 host with an aarch64 cross toolchain, or
# natively on aarch64). The PKGBUILD consumes the resulting ./config.
#
# usage: tools/genconfig.sh <kernel-src-dir> <alarm-config>
set -euo pipefail
src=$(realpath "$1"); base=$(realpath "$2")
here=$(cd "$(dirname "$0")/.." && pwd)
frag="$here/config.rt-fragment"

if [[ $(uname -m) != aarch64 ]]; then
  export ARCH=arm64 CROSS_COMPILE=${CROSS_COMPILE:-aarch64-linux-gnu-}
fi

cp "$base" "$src/.config"
# Apply the fragment with scripts/config so choices are toggled explicitly.
while IFS= read -r line; do
  case "$line" in
    CONFIG_*=\"*\") k=${line%%=*}; v=${line#*=}; v=${v#\"}; v=${v%\"}
                    "$src/scripts/config" --file "$src/.config" --set-str "${k#CONFIG_}" "$v" ;;
    CONFIG_*=y)     k=${line%%=*}; "$src/scripts/config" --file "$src/.config" -e "${k#CONFIG_}" ;;
    CONFIG_*=m)     k=${line%%=*}; "$src/scripts/config" --file "$src/.config" -m "${k#CONFIG_}" ;;
    "# CONFIG_"*" is not set") k=${line#\# CONFIG_}; k=${k% is not set}
                    "$src/scripts/config" --file "$src/.config" -d "$k" ;;
  esac
done < "$frag"
make -C "$src" -s olddefconfig

# Verify every fragment line survived olddefconfig.
rc=0
while IFS= read -r line; do
  [[ $line == CONFIG_* || $line == "# CONFIG_"*" is not set" ]] || continue
  if [[ $line == "# CONFIG_"* ]]; then
    k=${line#\# }; k=${k%% *}
    grep -q "^$k=" "$src/.config" && { echo "FRAGMENT LOST: $line" >&2; rc=1; }
  else
    grep -qxF "$line" "$src/.config" || { echo "FRAGMENT LOST: $line (got: $(grep "^${line%%=*}=" "$src/.config"))" >&2; rc=1; }
  fi
done < "$frag"
cp "$src/.config" "$here/config"
echo "wrote $here/config"
exit $rc
