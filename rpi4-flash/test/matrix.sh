#!/usr/bin/env bash
# Run e2e.sh over the configurations worth covering; prints one summary line
# per configuration. Usage: sudo test/matrix.sh WORKDIR [extra rpi4-flash flags]
set -o nounset -o pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
work=$1
shift
configs=(
  "aarch64 7.2.7 partition"
  "aarch64 latest file"
  "armv7 latest none"
)
mkdir -p "${work}"
status=0
for c in "${configs[@]}"; do
  read -r arch kernel swap <<<"${c}"
  out="${work}/${arch}-${kernel}-${swap}.out"
  if "${here}/e2e.sh" "${work}" "${arch}" "${kernel}" "${swap}" "$@" >"${out}" 2>&1; then
    echo "PASS  ${c}"
  else
    echo "FAIL  ${c}  (${out})"
    status=1
  fi
done
exit "${status}"
