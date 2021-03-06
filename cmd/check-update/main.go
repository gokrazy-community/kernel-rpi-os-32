package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
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

	latestSha, err := githubCommitSha(tagName)
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
		return "", errors.New("could not get sha: " + gr.Message)
	}
	return gr.Object.Sha, nil
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
