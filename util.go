package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/process"
)

// for unused vars
func U(x ...any) {}

// finding xray.exe path
func xrayWhich(customPath string) string {
	if customPath != "" {
		// absolute path
		if abs, err := filepath.Abs(customPath); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}

		// windows need ".exe" postfix
		if runtime.GOOS == "windows" && filepath.Ext(customPath) == "" {
			withExe := customPath + ".exe"
			if abs, err := filepath.Abs(withExe); err == nil {
				if _, err := os.Stat(abs); err == nil {
					return abs
				}
			}
		}
	}

	// looking in PATH
	names := []string{"xray", "v2ray"}

	if runtime.GOOS == "windows" {
		for i := range names {
			names[i] += ".exe"
		}
	}

	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}

	// default xray paths
	candidates := []string{
		"xray",
		"v2ray",
		filepath.Join("bin", "xray"),
		filepath.Join("bin", "v2ray"),
	}

	if runtime.GOOS == "windows" {
		var winCandidates []string
		for _, c := range candidates {
			winCandidates = append(winCandidates, c+".exe")
		}
		candidates = append(candidates, winCandidates...)
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			if absPath, err := filepath.Abs(c); err == nil {
				return absPath
			}
		}
	}

	return ""
}

// checking xray version
func xrayVersion(xrayPath string) (string, error) {
	cmd := exec.Command(xrayPath, "version")

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run xray: %v", err)
	}
	cmd.Wait()

	// check for "Xray" string
	parsedOutput := strings.Split(string(output), " ")
	if parsedOutput[0] != "Xray" {
		return "", fmt.Errorf("wrong xray binary (parsedOutput[0] != \"Xray\")")
	}

	// check for correct version
	for i := range strings.Split(parsedOutput[1], ".") {
		_, err := strconv.Atoi(fmt.Sprint(i))
		if err != nil {
			return "", fmt.Errorf("wrong xray version: %s - %v", parsedOutput[1], err)
		}
	}

	return parsedOutput[1], nil
}

func KillProcessesByName(target string) (int, error) {
	processes, err := process.Processes()
	if err != nil {
		return 0, err
	}

	target = strings.ToLower(strings.TrimSuffix(target, ".exe"))
	killed := 0

	for _, p := range processes {
		name, err := p.Name()
		if err != nil {
			continue
		}

		base := strings.ToLower(strings.TrimSuffix(name, ".exe"))
		if base == target {
			if err := p.Kill(); err == nil {
				killed++
			}
		}
	}

	return killed, nil
}

func readFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		if err = file.Close(); err != nil {
			log.Panic().
				Err(err).
				Str("path", path).
				Msg("failed to close file")
		}
	}()

	buf, err := io.ReadAll(file)
	return string(buf), err
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func isPortInUse(port int) bool {
	address := fmt.Sprintf("127.0.0.1:%d", port)

	conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
	if err != nil {
		return false
	}
	defer conn.Close()

	return true
}

func urlGet(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d - %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}
