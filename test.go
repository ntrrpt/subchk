package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/cnlangzi/proxyclient"

	_ "github.com/cnlangzi/proxyclient/xray"
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

	for {
		select {
		case <-ctx.Done():
			log.Warn().Msgf("worker %d shutting down...", id)
			return

		case job, ok := <-jobs:
			if !ok {
				return
			}

			data := runTest(job)

			select {
			case results <- data:
			case <-ctx.Done():
				return
			}
		}
	}
}

func WithClientTimeout(duration time.Duration) proxyclient.Option {
	client := &http.Client{
		Timeout: duration,
	}

	return func(o *proxyclient.Options) {
		o.Client = client
		o.Timeout = duration
	}
}

func runTest(job TestJob) TestResult {
	result := TestResult{
		ID:    job.ID,
		Url:   job.URL,
		Ping:  0,
		Speed: 0,
		Time:  0,
		dwLen: 0,
		Error: nil,
	}

	url, err := url.Parse(job.URL)
	if err != nil {
		result.Error = fmt.Errorf("[url] %v", err)
		log.Error().Err(result.Error).Msg(job.URL)
		return result
	}

	address := fmt.Sprintf("[%d] %s:%s", job.ID, url.Hostname(), url.Port())

	client, err := proxyclient.New(job.URL, WithClientTimeout(time.Duration(pingTimeout)*time.Second))
	if err != nil {
		result.Error = fmt.Errorf("[proxyclient] %v", err)
		log.Error().Err(result.Error).Msg(job.URL)
		return result
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
