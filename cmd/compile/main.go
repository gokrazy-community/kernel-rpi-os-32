package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

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
var makeConfigFlag = flag.String("make-config", "bcmrpi_defconfig", "arguments to pass to the initial 'make' config step")
var distFolderFlag = flag.String("dist", "./dist", "folder to place compiled artifacts")

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
	user, err := user.Current()
	if err != nil {
		return err
	}
	// change the owner of the files inside docker to the current user
	chown := func(folder string) error {
		return dockerRun("chown", "-R", user.Uid+":"+user.Gid, folder)
	}

	// bcmrpi_defconfig:  default raspberry pi config for 32 bit Pi 1, Zero
	// bcm2709_defconfig: default raspberry pi config for 32 bit Pi 2, 3, and Zero 2
	// See: https://www.raspberrypi.com/documentation/computers/linux_kernel.html#native-build-configuration
	configArgs := strings.Fields(*makeConfigFlag)
	// mod2noconfig: disable all modules
	configArgs = append(configArgs, "mod2noconfig")
	configArgs = append([]string{"make"}, configArgs...)
	if err := dockerRun(configArgs...); err != nil {
		return err
	}

	// https://stackoverflow.com/a/56515886
	// it doesn't check the validity of the .config file
	// so we run make olddefconfig afterwards
	args := []string{"./scripts/config",
		// Basics
		"--enable", "SQUASHFS",
		"--enable", "IPV6",
		"--enable", "MODULES",

		// Disable module compression (wifi needs this)
		"--disable", "MODULE_COMPRESS",
		"--disable", "MODULE_COMPRESS_GZIP",
		"--disable", "MODULE_COMPRESS_XZ",
		"--disable", "MODULE_COMPRESS_ZSTD",

		// WiFi
		"--enable", "RFKILL",
		"--enable", "CFG80211",
		"--module", "BRCMFMAC",

		// Bluetooth
		"--enable", "NLMON",
		"--module", "BT",
		"--module", "BT_BCM",
		"--module", "BT_HCIUART",
		"--enable", "BT_HCIUART_BCM",

		// For nftables:
		"--enable", "NF_TABLES",
		"--enable", "NF_NAT_IPV4",
		"--enable", "NF_NAT_MASQUERADE_IPV4",
		"--enable", "NFT_PAYLOAD",
		"--enable", "NFT_EXTHDR",
		"--enable", "NFT_META",
		"--enable", "NFT_CT",
		"--enable", "NFT_RBTREE",
		"--enable", "NFT_HASH",
		"--enable", "NFT_COUNTER",
		"--enable", "NFT_LOG",
		"--enable", "NFT_LIMIT",
		"--enable", "NFT_NAT",
		"--enable", "NFT_COMPAT",
		"--enable", "NFT_MASQ",
		"--enable", "NFT_MASQ_IPV4",
		"--enable", "NFT_REDIR",
		"--enable", "NFT_REJECT",
		"--enable", "NF_TABLES_IPV4",
		"--enable", "NFT_REJECT_IPV4",
		"--enable", "NFT_CHAIN_ROUTE_IPV4",
		"--enable", "NFT_CHAIN_NAT_IPV4",
		"--enable", "NF_TABLES_IPV6",
		"--enable", "NFT_CHAIN_ROUTE_IPV6",
		"--enable", "NFT_OBJREF",

		// WireGuard VPN
		"--enable", "NET_UDP_TUNNEL",
		"--enable", "WIREGUARD",
		"--enable", "TUN",

		// Enable policy routing (required for Tailscale)
		"--enable", "IP_MULTIPLE_TABLES",
		"--enable", "IPV6_MULTIPLE_TABLES",

		// USB
		"--enable", "USB_SERIAL",
		"--enable", "USB_SERIAL_FTDI_SIO",
		"--enable", "USB_SERIAL_CP210X",
		"--enable", "USB_ACM",
		"--enable", "USB_PRINTER", //printer support /dev/usb/lp0

		"--enable", "USB_SERIAL_GENERIC",
		"--module", "USB_SERIAL_CH341",
		"--module", "USB_SERIAL_PL2303",
		"--module", "USB_SERIAL_SAFE",
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

	releaseRaw, err := os.ReadFile(filepath.Join(kernelFolder, "include", "config", "kernel.release"))
	if err != nil {
		return err
	}
	release := strings.TrimSpace(string(releaseRaw))

	bootFolder := filepath.Join(kernelFolder, "arch", "arm", "boot")
	dstFolder := filepath.Join(".", *distFolderFlag)
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
	if err := os.Rename(filepath.Join(kernelFolder, "modules_out", "lib"), filepath.Join(dstFolder, "lib")); err != nil {
		return err
	}
	// remove unused symlinks
	if err := os.Remove(filepath.Join(dstFolder, "lib", "modules", release, "build")); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(filepath.Join(dstFolder, "lib", "modules", release, "source")); err != nil && !os.IsNotExist(err) {
		return err
	}

	// copy dtb files
	files, err := filepath.Glob(filepath.Join(bootFolder, "dts", "**/bcm*-rpi-*.dtb"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no dtb files found under %s", filepath.Join(bootFolder, "dts"))
	}
	// copy config and cmdline files
	files = append(files, "./gokrazy/cmdline.txt", "./gokrazy/config.txt")
	for _, file := range files {
		dtbName := filepath.Base(file)
		if err = sh.Copy(filepath.Join(dstFolder, dtbName), file); err != nil {
			return err
		}
	}

	packageName := filepath.Base(dstFolder)
	content := []byte(fmt.Sprintf("package %s\n\n// empty package so we can use the go tool with this repository\n", packageName))
	if err = os.WriteFile(filepath.Join(dstFolder, "placeholder.go"), content, 0644); err != nil {
		return err
	}

	return nil
}
