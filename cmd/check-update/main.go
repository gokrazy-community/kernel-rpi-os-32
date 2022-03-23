package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

func main() {
	if err := run(); err != nil {
		log.Println(err)

		os.Exit(1)
	}
}

const baseURL = "https://archive.raspberrypi.org/debian/"
const packagesURL = baseURL + "dists/buster/main/binary-armhf/Packages"

func run() error {
	log.Println("checking:", packagesURL)
	kernelPrefix := "Filename: pool/main/r/raspberrypi-firmware/raspberrypi-kernel_"
	version := ""
	versionPrefix := "Version: "
	err := scanOnlineTextFile(packagesURL, func(s string) bool {
		if strings.HasPrefix(s, versionPrefix) {
			version = s[len(versionPrefix):]
		}
		if strings.HasPrefix(s, kernelPrefix) {
			return true
		}
		return false
	})
	if version == "" {
		if err != nil {
			return err
		}
		return errors.New("could not find kernel version in package list")
	}

	before, after, found := strings.Cut(version, ":")
	if !found {
		after = before
	}
	tagName, _, _ := strings.Cut(after, "~")

	log.Println("latest version:", tagName)

	tags, err := gitTags("linux-sources")
	if err != nil {
		return err
	}
	for _, tag := range tags {
		if tagName == tag {
			log.Println("already up to date")
			return nil
		}
	}
	log.Println("outdated tag", tags)
	fmt.Println(tagName)

	return nil
}

func gitTags(folder string) ([]string, error) {
	current, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	defer os.Chdir(current)

	err = os.Chdir(folder)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("git", "tag", "--points-at", "HEAD")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n"), nil
}

func scanOnlineTextFile(url string, stopScanning func(string) bool) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if stopScanning(scanner.Text()) {
			break
		}
	}
	return scanner.Err()
}
