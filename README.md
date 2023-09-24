# Kernel for Raspberry Pi 32 bits (in sync with official bullseye repo)

This repository holds a pre-built 32 bits Linux bits kernel image for the Raspberry Pi, compiled from https://github.com/raspberrypi/linux, for usage by the [gokrazy](https://github.com/gokrazy/gokrazy) project.

To use the files in this repository, adjust the `-kernel_package`
of `gokr-packer`:

```
GOARCH=arm gokr-packer \
    -kernel_package=github.com/gokrazy-community/kernel-rpi-os-32/dist \
    github.com/gokrazy/hello
```

## How does it differ from https://github.com/gokrazy/kernel ?

|              | gokrazy-community/kernel-rpi-os-32 | gokrazy/kernel |
| ------------ | ---------------------------------- | -------------- |
| architecture | ARMv6 32-bit                       | ARMv8 64-bit   |
| upstream     | linux Raspberry Pi OS fork         | linux mainline |

This kernel can be used by the oldest Raspberry Pi and follows the release cycles of the Raspberry Pi foundation.

## Manual compilation

```
go run cmd/compile/main.go
```

It will compile the kernel located in `linux-sources` using a [crossbuild docker image](https://github.com/gokrazy-community/crossbuild-armhf) and copy the resulting files in the `dist` folder.

It uses default kernel config (`make bcmrpi_defconfig`), as recommended by the [official documentation](https://www.raspberrypi.com/documentation/computers/linux_kernel.html#cross-compiling-the-kernel), with the addition of the SquashFS module (`CONFIG_SQUASHFS`, which is required for gokrazy) and `CONFIG_IPV6`.

## Update check

```
go run cmd/check-update/main.go
```

It will compare the kernel version distributed on https://archive.raspberrypi.org/debian/ with the `linux-sources` submodule current HEAD.

## Licenses

- The `vmlinuz` and `*.dtb` files are built from [Linux kernel sources](https://github.com/raspberrypi/linux), released under the GPL (see `linux-sources/COPYING`)
- The rest of the repository is released under BSD 3-Clause License (see `LICENSE`)
