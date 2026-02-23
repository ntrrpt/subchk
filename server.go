package main

import (
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

func parseAddr(input string) (string, error) {
	if !strings.Contains(input, ":") {
		if _, err := strconv.Atoi(input); err != nil {
			return "", errors.New("invalid port")
		}
		return ":" + input, nil
	}

	host, port, err := net.SplitHostPort(input)
	if err != nil {
		return "", err
	}

	if port == "" {
		return "", errors.New("empty port")
	}

	if _, err := strconv.Atoi(port); err != nil {
		return "", errors.New("invalid port")
	}

	if host == "" {
		return ":" + port, nil
	}

	return net.JoinHostPort(host, port), nil
}

func serveFile(filePath string, addrArg string) error {
	addr, err := parseAddr(addrArg)
	if err != nil {
		log.Fatal().
			Err(err).
			Str("addrArg", addrArg).
			Msg("failed to parse addr")
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			log.Error().
				Str("method", r.Method).
				Str("addr", r.RemoteAddr).
				Str("path", r.URL.String()).
				Msg("method not allowed")
			return
		}

		file, err := os.Open(filePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			log.Error().
				Err(err).
				Str("addr", r.RemoteAddr).
				Str("path", r.URL.String()).
				Str("filePath", filePath).
				Msg("failed to open file")
			return
		}
		defer file.Close()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		io.Copy(w, file)

		log.Info().
			Str("addr", r.RemoteAddr).
			Str("path", r.URL.String()).
			Msg("get")
	})

	log.Info().
		Str("filePath", filePath).
		Str("addr", addr).
		Msg("server started")

	return http.ListenAndServe(addr, nil)
}
