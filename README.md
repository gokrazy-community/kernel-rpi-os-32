# Kernel for Raspberry Pi 32 bits, for usage in gokrazy

Usage

```
GOARCH=arm ./gokr-packer \
		-kernel_package=github.com/oliverpool/kernel-rpi-os-32/dist \
		-firmware_package=github.com/oliverpool/firmware-rpi/dist \
		-serial_console=disabled \
		github.com/gokrazy/hello
```

## Manual compilation

```
go run cmd/compile/main.go
```

It will compile the kernel located in `linux-sources` and copy the resulting files in the `dist` folder.
