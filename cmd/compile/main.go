package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/magefile/mage/sh"
)

func main() {
	if err := run(); err != nil {
		log.Println(err)

		os.Exit(1)
	}
}

func execCmd(env map[string]string, stdout io.Writer, stderr io.Writer, cmd string, args ...string) func(args ...string) error {
	return func(args2 ...string) error {
		fmt.Println(cmd, args2)
		_, err := sh.Exec(env, stdout, stderr, cmd, append(args, args2...)...)
		return err
	}
}

var kernelFolderFlag = flag.String("kernel", "./linux-sources", "folder containing the kernel to compile")

const configAddendum = `
# Basics
CONFIG_SQUASHFS=y
CONFIG_IPV6=y
CONFIG_MODULES=y

# Disable module compression (wifi needs this)
CONFIG_MODULE_COMPRESS_NONE=y
CONFIG_MODULE_COMPRESS_GZIP=n
CONFIG_MODULE_COMPRESS_XZ=n
CONFIG_MODULE_COMPRESS_ZSTD=n

# WiFi
CONFIG_RFKILL=y
CONFIG_CFG80211=y
CONFIG_BRCMFMAC=m

# Bluetooth
CONFIG_NLMON=y
CONFIG_BT=m
CONFIG_BT_BCM=m
CONFIG_BT_HCIUART=m
CONFIG_BT_HCIUART_BCM=y
`

func run() error {
	flag.Parse()

	kernelFolder, err := filepath.Abs(*kernelFolderFlag)
	if err != nil {
		return err
	}

	fmt.Println("[kernel]", kernelFolder)

	dockerRun := execCmd(nil, os.Stdout, os.Stderr,
		"docker",
		"run",
		"--rm", // cleanup afterwards
		"-v", kernelFolder+":/root/armhf",
		"ghcr.io/gokrazy-community/crossbuild-armhf:jammy-20220815",
	)
	// change the owner of the files inside docker to the current user
	chown := func(folder string) error {
		user, err := user.Current()
		if err != nil {
			return err
		}
		return dockerRun("chown", "-R", user.Uid+":"+user.Gid, folder)
	}

	// default raspberry pi config according to https://www.raspberrypi.com/documentation/computers/linux_kernel.html#cross-compiling-the-kernel
	if err := dockerRun("make", "bcmrpi_defconfig"); err != nil {
		return err
	}
	// disable all modules (TODO: replace with mod2noconfig once we have 5.17 or newer)
	if err := dockerRun("sed", "s/=m$/=n/i", "-i", ".config"); err != nil {
		return err
	}
	if err := dockerRun("make", "olddefconfig"); err != nil {
		return err
	}
	if err := chown(".config"); err != nil {
		return err
	}

	// write additions to config that we need
	configPath := filepath.Join(kernelFolder, ".config")
	f, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte(configAddendum)); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	// merge the config with our addendums
	if err := dockerRun("./scripts/kconfig/merge_config.sh", ".config", ".config.gokrazy"); err != nil {
		return err
	}

	// compile kernel and dtbs
	if err := dockerRun("make", "zImage", "dtbs", "modules", "-j"+strconv.Itoa(runtime.NumCPU())); err != nil {
		return err
	}
	if err := chown("arch/arm/boot"); err != nil {
		return err
	}

	bootFolder := filepath.Join(kernelFolder, "arch", "arm", "boot")
	dstFolder := filepath.Join(".", "dist")
	os.RemoveAll(dstFolder) // ignore any error
	if err = os.MkdirAll(dstFolder, 0755); err != nil {
		return err
	}

	if err := dockerRun("make", "INSTALL_MOD_PATH=modules_out", "modules_install", "-j"+strconv.Itoa(runtime.NumCPU())); err != nil {
		return err
	}

	// copy and rename kernel
	if err = sh.Copy(filepath.Join(dstFolder, "vmlinuz"), filepath.Join(bootFolder, "zImage")); err != nil {
		return err
	}

	// copy modules
	os.Rename(filepath.Join(kernelFolder, "modules_out/lib"), path.Join(dstFolder, "lib"))

	// copy dtb files
	files, err := filepath.Glob(filepath.Join(bootFolder, "dts", "bcm*-rpi-*.dtb"))
	if err != nil {
		return err
	}
	// copy config and cmdline files
	files = append(files, "./gokrazy/cmdline.txt", "./gokrazy/config.txt")
	for _, file := range files {
		dtbName := filepath.Base(file)
		if err = sh.Copy(filepath.Join(dstFolder, dtbName), file); err != nil {
			return err
		}
	}

	if err = os.WriteFile(filepath.Join(dstFolder, "placeholder.go"), []byte(`package dist

// empty package so we can use the go tool with this repository
`), 0755); err != nil {
		return err
	}

	return nil
}
