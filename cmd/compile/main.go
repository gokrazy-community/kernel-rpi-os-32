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

	// bcmrpi_defconfig: default raspberry pi config according to https://www.raspberrypi.com/documentation/computers/linux_kernel.html#cross-compiling-the-kernel
	// mod2noconfig: disable all modules
	if err := dockerRun("make", "bcmrpi_defconfig", "mod2noconfig"); err != nil {
		return err
	}

	// https://stackoverflow.com/a/56515886
	// it doesn't check the validity of the .config file
	// so we run make olddefconfig afterwards
	args := []string{"./scripts/config",
		// Basics
		"--set-val", "SQUASHFS", "y",
		"--set-val", "IPV6", "y",
		"--set-val", "MODULES", "y",

		// Disable module compression (wifi needs this)
		"--set-val", "MODULE_COMPRESS_NONE", "y",
		"--set-val", "MODULE_COMPRESS_GZIP", "n",
		"--set-val", "MODULE_COMPRESS_XZ", "n",
		"--set-val", "MODULE_COMPRESS_ZSTD", "n",

		// WiFi
		"--set-val", "RFKILL", "y",
		"--set-val", "CFG80211", "y",
		"--set-val", "BRCMFMAC", "m",

		// Bluetooth
		"--set-val", "NLMON", "y",
		"--set-val", "BT", "m",
		"--set-val", "BT_BCM", "m",
		"--set-val", "BT_HCIUART", "m",
		"--set-val", "BT_HCIUART_BCM", "y",
	}

	if err := dockerRun(args...); err != nil {
		return err
	}
	if err := dockerRun("make", "olddefconfig"); err != nil {
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

	// copy and rename kernel
	if err = sh.Copy(filepath.Join(dstFolder, "vmlinuz"), filepath.Join(bootFolder, "zImage")); err != nil {
		return err
	}

	// compile and move modules to dist
	if err := dockerRun("make", "INSTALL_MOD_PATH=modules_out", "modules_install", "-j"+strconv.Itoa(runtime.NumCPU())); err != nil {
		return err
	}
	if err := chown("modules_out"); err != nil {
		return err
	}
	err = os.Rename(filepath.Join(kernelFolder, "modules_out/lib"), path.Join(dstFolder, "lib"))
	if err != nil {
		return err
	}

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
