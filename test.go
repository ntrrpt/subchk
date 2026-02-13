package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

type TestJob struct {
	ID  int
	URL string
}

type TestResult struct {
	ID    int
	Error error
	Url   string
	Ping  int64   // milliseconds
	Speed float64 // MB/s
	Time  float64 // seconds
	dwLen float64 // MB
}

func worker(ctx context.Context, id int, jobs <-chan TestJob, results chan<- TestResult, wg *sync.WaitGroup) {
	defer wg.Done()

	// todo: check ports for availability
	port := 10000 + id

	for {
		select {
		case <-ctx.Done():
			log.Warn().Msgf("worker %d shutting down...", id)
			return

		case job, ok := <-jobs:
			if !ok {
				return
			}

			data := runTest(job, port)

			select {
			case results <- data:
			case <-ctx.Done():
				return
			}
		}
	}
}

func runTest(job TestJob, port int) TestResult {
	result := TestResult{
		ID:    job.ID,
		Url:   job.URL,
		Ping:  0,
		Speed: 0,
		Time:  0,
		dwLen: 0,
		Error: nil,
	}

	if isPortInUse(port) {
		result.Error = fmt.Errorf("failed to allocate port: %d", port)
		log.Error().Err(result.Error).Msg(job.URL)
		return result
	}

	url, err := url.Parse(job.URL)
	if err != nil {
		result.Error = fmt.Errorf("failed to parse url: %v", err)
		log.Error().Err(result.Error).Msg(job.URL)
		return result
	}

	address := fmt.Sprintf("[%d] %s:%s", job.ID, url.Hostname(), url.Port())

	// converting url to xray json config
	config, err := parseUrl(job.URL, port, "", "")
	if err != nil {
		result.Error = fmt.Errorf("failed to convert url to json: %v", err)
		log.Error().Err(result.Error).Msg(address)
		return result
	}

	// marshaling json
	jsonData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		result.Error = fmt.Errorf("error parsing JSON: %v", err)
		log.Error().Err(result.Error).Msg(address)
		return result
	}

	// create tmp file
	tmpFile, err := os.CreateTemp(tmpFolder, fmt.Sprintf("subchk-%d-*.json", job.ID))
	if err != nil {
		result.Error = fmt.Errorf("failed to create tmp config: %v", err)
		log.Error().Err(result.Error).Msg(address)
		return result
	}

	// writing config
	tmpFile.WriteString(string(jsonData))
	tmpFile.Close()

	// creating xray command
	cmd := exec.Command(corePath, "-c", tmpFile.Name())

	// starting xray in background
	err = cmd.Start()
	if err != nil {
		result.Error = fmt.Errorf("failed to start xray: %v", err)
		log.Error().Err(result.Error).Msg(address)
		return result
	}

	// killing xray in the end
	defer func() {
		err = cmd.Process.Kill()
		if err != nil {
			log.Printf("failed to kill xray: %v", err)
		} else {
			cmd.Process.Wait()
		}
	}()

	// waiting for xray to booting
	time.Sleep(1 * time.Second)

	// creating socks5 dialer
	dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", port), nil, proxy.Direct)
	if err != nil {
		result.Error = fmt.Errorf("failed to create socks5 dialer: %v", err)
		log.Error().Err(result.Error).Msg(address)
		return result
	}

	// http client from socks5
	transport := &http.Transport{
		Dial: dialer.Dial,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(pingTimeout) * time.Second,
	}

	// ping
	startPing := time.Now()
	presp, err := client.Get(pingUrl)

	if err != nil {
		result.Error = fmt.Errorf("[ping] %v", err)
		log.Error().Err(result.Error).Msg(address)
		return result
	}
	defer presp.Body.Close()

	result.Ping = time.Since(startPing).Milliseconds()

	if speedTest {
		// downloading test file
		client.Timeout = time.Duration(speedTimeout) * time.Second
		sresp, err := client.Get(speedUrl)

		if err != nil {
			result.Error = fmt.Errorf("[speed] %v", err)
			log.Error().Err(result.Error).Msg(address)
			return result
		}
		defer sresp.Body.Close()

		startDownload := time.Now()
		n, err := io.Copy(io.Discard, sresp.Body)

		result.dwLen = float64(n)

		if err != nil && result.dwLen == 0 {
			result.Error = fmt.Errorf("[speed] %v", err)
			log.Error().
				Str("transfered", fmt.Sprintf("%.2fMB", result.dwLen/1024/1024)).
				Err(result.Error).
				Msg(address)
			return result
		}

		// mesaure download speed
		result.Time = time.Since(startDownload).Seconds()
		result.Speed = result.dwLen / result.Time / 1024 / 1024 // MB/s
		log.Info().
			Int64("ping", result.Ping).
			Str("transfered", fmt.Sprintf("%.2f MB", result.dwLen/1024/1024)).
			Str("duration", fmt.Sprintf("%.2fs", result.Time)).
			Str("speed", fmt.Sprintf("%.2f MB/s", result.Speed)).
			Msg(address)
	} else {
		log.Info().
			Int64("ping", result.Ping).
			Msg(address)
	}

	return result

}
