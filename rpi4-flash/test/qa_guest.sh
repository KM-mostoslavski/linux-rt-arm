#!/bin/sh
# In-guest checks, copied into the TEST image by e2e.sh and run by
# qemu_boot.py after logging in (typing long lines over QEMU's emulated
# UART loses input, so only "sh /root/rpi4-flash-qa.sh" is typed).
f=0
ck() {
  if sh -c "$2" >/dev/null 2>&1; then echo "QA PASS  $1"; else echo "QA FAIL  $1"; f=1; fi
}
echo "QA kernel: $(uname -rv)"
echo "QA /boot: $(findmnt -no SOURCE,FSTYPE /boot)"
echo "QA swap: $(swapon --noheadings --show=NAME,SIZE | tr '\n' ' ')"
echo "QA fstab: $(grep -v '^#' /etc/fstab | grep . | tr -s ' ' | tr '\n' ';')"
ck "/boot mounted (vfat, by UUID)" 'findmnt -no FSTYPE /boot | grep -qx vfat && grep -q "^UUID=.* /boot " /etc/fstab'
ck "/boot holds the initramfs" 'test -s /boot/initramfs-linux.img'
ck "root mounted rw" 'findmnt -no OPTIONS / | grep -q "^rw"'
ck "fstab uses no /dev/mmcblk" '! grep -q "^/dev/mmcblk" /etc/fstab'
ck "active swaps match fstab" 'test "$(grep -c "[[:space:]]swap[[:space:]]" /etc/fstab)" = "$(swapon --noheadings | wc -l)"'
ck "pacman keyring initialised" 'test -s /etc/pacman.d/gnupg/pubring.gpg'
ck "no failed units" 'test -z "$(systemctl --failed --no-legend --plain)"'
systemctl --failed --no-legend --plain | sed 's/^/QA failed unit: /'
echo "QA_RESULT=$f"
