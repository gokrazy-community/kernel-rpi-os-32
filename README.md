# Linux kernel for Raspberry Pi 32 bits, for usage in gokrazy

Usage

```
GOARCH=arm ./gokr-packer \
		-kernel_package=github.com/oliverpool/kernel-rpi-os-32/dist \
		-firmware_package=github.com/oliverpool/firmware-rpi/dist \
		github.com/gokrazy/hello
```

## Manual compilation

```
go run cmd/compile/main.go
```

It will compile the kernel located in `linux-sources` and copy the resulting files in the `dist` folder.

## Licenses

- The `vmlinuz` and `*.dtb` files are built from [Linux kernel sources](https://github.com/raspberrypi/linux), released under the GPL (see `linux-sources/COPYING`)
- The rest of the repository is released under BSD 3-Clause License (see `LICENSE`)
