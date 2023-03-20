package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/ulikunitz/xz"
)

func main() {
	if err := run(); err != nil {
		log.Println(err)

		os.Exit(1)
	}
}

const baseURL = "https://archive.raspberrypi.org/debian/"
const packagesGzURL = baseURL + "dists/bullseye/main/binary-armhf/Packages.gz"

func run() error {
	log.Println("checking:", packagesGzURL)
	kernelPrefix := "Filename: pool/main/r/raspberrypi-firmware/raspberrypi-kernel_"
	version := ""
	versionPrefix := "Version: "
	found := false
	err := fetchAndScanGzTextFile(packagesGzURL, func(s string) bool {
		if strings.HasPrefix(s, versionPrefix) {
			version = s[len(versionPrefix):]
		}
		if strings.HasPrefix(s, kernelPrefix) {
			found = true
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
	tagName, _, _ := strings.Cut(after, "-")
	tagName, _, _ = strings.Cut(tagName, "~")

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
		log.Println("already up to date")
		return nil
	}
	fmt.Println(latestSha)

	return nil
}

func commitFromTag(tagName string) (string, error) {
	latestSha, err := githubCommitSha(tagName)
	log.Println("checking https://github.com/raspberrypi/linux tags")
	if err == nil {
		return latestSha, nil
	}
	log.Println(err)

	xzURL := "https://archive.raspberrypi.org/debian/pool/main/r/raspberrypi-firmware/raspberrypi-firmware_" + tagName + ".orig.tar.xz"

	log.Println("checking", xzURL)
	return debianSourceCommitSha(xzURL)
}

func githubCommitSha(tagName string) (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/raspberrypi/linux/git/ref/tags/"+tagName, nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Accept", "application/vnd.github.v3+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	type githubResponse struct {
		Message string `json:"message"`
		Object  struct {
			Sha string `json:"sha"`
		} `json:"object"`
	}
	var gr githubResponse
	err = json.NewDecoder(resp.Body).Decode(&gr)
	if err != nil {
		return "", err
	}
	if gr.Object.Sha == "" {
		return "", fmt.Errorf("could not get sha for tag %q: %s", tagName, gr.Message)
	}
	return gr.Object.Sha, nil
}

func debianSourceCommitSha(xzURL string) (string, error) {
	resp, err := http.Get(xzURL)
	if err != nil {
		return "", err
	}
	xzFile := resp.Body

	//  for local testing
	// xzFile, err := os.Open("raspberrypi-firmware_1.20230317.orig.tar.xz")
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
			return "", fmt.Errorf("extra/git_hash not found: %w", err)
		}
		if !strings.HasSuffix(hdr.Name, "/extra/git_hash") {
			continue
		}

		buf, err := io.ReadAll(tr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(buf)), nil
	}
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
