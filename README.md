# Linux-rt-arm

Realtime linux kernel for armv7h and aarch64.

## What is it

This repo has 2 PKGBUILDs and 1 installation script.

### PKGBUILDs

The two packages builds are located in `./PKGBUILDs/`.
1. One `linux-rt-arm` (builds from source the kernel)
2. And `linux-rt-arm-bin` which installs a pre-built kernel, at the latest version.

The prebuilt kernels are downloadable at <todo: insert URL>.

### Package aim

This package is mainly developped and aimed at Raspberry Pi 4.

Since 6.12 [citation needed], `PREEMPT_RT` is a build-time option, and not a 
patch anymore. Since the mainline linux kernel supports the RP4 and many other
devices, this rebuilt should work on most ARM devices.

#### On the Raspberry Pi 4 realtime project

This repository has a sibling project: an automated Raspberry Pi flashing
script, that enables rt automatically for you.

If you are interested in fast flashing of pre-configured headless Raspberry Pis
for realtime robotics or other needs, feel free to check it out at
<https://github.com/KM-mostoslavski/arch-linux-preempt-rt-rpi-flasher>

## Why you'd need this ?

Realtime became much easier. On x86-64, the arch linux community already provides
[linux-rt](https://archlinux.org/packages/extra/x86_64/linux-rt/), but
unfortunately, it is lacking on the ARM side.

Furthermore, on microcontrollers (MCU), or other RTOS devices, you might need to
run more complex processes, which requires an OS.
This void is present in Arch Linux ARM as of right now, and this project
attempts to solve this.

## End note

Feel free to contact the maintainer <todo: my email> or the company hosting
the prebuilt packages <todo: km-robot email> for requests or information.

This project is under MIT License, and is free to use for everyone.
