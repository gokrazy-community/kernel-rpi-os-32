package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/ulikunitz/xz"
)

func main() {
	if err := run(); err != nil {
		log.Println(err)

		os.Exit(1)
	}
}

const (
	baseURL       = "https://archive.raspberrypi.org/debian/"
	packagesGzURL = baseURL + "dists/trixie/main/binary-armhf/Packages.gz"
)

func run() error {
	log.Println("checking:", packagesGzURL)
	packageName := "Package: linux-image-rpi-v6"
	version := ""
	versionPrefix := "Version: "
	found := false
	err := fetchAndScanGzTextFile(packagesGzURL, func(s string) bool {
		if s == packageName {
			found = true
		} else if found && strings.HasPrefix(s, versionPrefix) {
			version = s[len(versionPrefix):]
			return true
		}
		return false
	})
	if !found {
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

	latestSha, err := commitFromTag(tagName)
	if err != nil {
		return err
	}
	log.Println("latest commit:", latestSha)

	currentSha, err := submoduleSha("linux-sources")
	if err != nil {
		return err
	}
	log.Println("submodule commit:", currentSha)

	if latestSha == currentSha {
		if _, err := os.Stat("force-update"); errors.Is(err, os.ErrNotExist) {
			log.Println("already up to date")
			return nil
		}
		os.Remove("force-update")
	}
	fmt.Println(latestSha)

	return nil
}

func commitFromTag(tagName string) (string, error) {
	xzURL := "https://archive.raspberrypi.org/debian/pool/main/l/linux/linux_" + tagName + ".debian.tar.xz"

	log.Println("checking", xzURL)
	return debianSourceCommitSha(xzURL)
}

func debianSourceCommitSha(xzURL string) (string, error) {
	resp, err := http.Get(xzURL)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return "", fmt.Errorf("could not download package source: %s", resp.Status)
	}
	xzFile := resp.Body

	//  for local testing
	// xzFile, err := os.Open("linux_6.12.62-1+rpt1.debian.tar.xz")
	// if err != nil {
	// 	return "", err
	// }
	defer xzFile.Close()

	tarFile, err := xz.NewReader(xzFile)
	if err != nil {
		return "", err
	}

	tr := tar.NewReader(tarFile)
	for {
		hdr, err := tr.Next()
		if err != nil {
			return "", fmt.Errorf("debian/changelog not found: %w", err)
		}
		if !strings.HasSuffix(hdr.Name, "debian/changelog") {
			continue
		}

		return debianChangelogCommitSha(tr)
	}
}

func debianChangelogCommitSha(tr io.Reader) (string, error) {
	re := regexp.MustCompile(`(?i)^\s*\*\s*Linux commit:\s*([0-9a-f]{7,40})\b`)
	scanner := bufio.NewScanner(tr)
	for scanner.Scan() {
		if m := re.FindStringSubmatch(scanner.Text()); m != nil {
			return m[1], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("debian/changelog does not contain 'Linux commit'")
}

func submoduleSha(submodule string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD:"+submodule)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func fetchAndScanGzTextFile(url string, stopScanning func(string) bool) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	unzipped, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(unzipped)
	for scanner.Scan() {
		if stopScanning(scanner.Text()) {
			break
		}
	}
	return scanner.Err()
}
