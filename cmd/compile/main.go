package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/fs"
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

func run() error {
	flag.Parse()

	// TODO new version check

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
		"ghcr.io/gokrazy-community/crossbuild-armhf:impish-20220316",
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
	if err := chown(".config"); err != nil {
		return err
	}

	// adjust config to add CONFIG_SQUASHFS
	configPath := filepath.Join(kernelFolder, ".config")
	err = adjustTextFile(configPath, func(line string) bool {
		return strings.HasPrefix(line, "CONFIG_SQUASHFS=")
	}, []string{
		"CONFIG_SQUASHFS=y",
	})
	if err != nil {
		return err
	}

	// compile kernel and dtbs
	if err := dockerRun("make", "zImage", "dtbs", "-j"+strconv.Itoa(runtime.NumCPU())); err != nil {
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

func adjustTextFile(path string, skipLine func(string) bool, appendLines []string) error {
	b, stat, err := readFile(path) // read the whole file in memory, since we are going to overwrite it
	if err != nil {
		return err
	}

	dst, err := os.OpenFile(path, os.O_TRUNC|os.O_WRONLY, stat.Mode())
	if err != nil {
		return err
	}
	defer dst.Close()
	w := bufio.NewWriter(dst)

	for _, line := range strings.Split(string(b), "\n") {
		if skipLine(line) {
			continue
		}
		_, err = w.WriteString(line + "\n")
		if err != nil {
			return err
		}
	}
	for _, line := range appendLines {
		_, err = w.WriteString(line + "\n")
		if err != nil {
			return err
		}
	}
	err = w.Flush()
	if err != nil {
		return err
	}
	return dst.Close()
}

func readFile(path string) ([]byte, fs.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return b, nil, err
	}

	stats, err := f.Stat()
	return b, stats, err
}
