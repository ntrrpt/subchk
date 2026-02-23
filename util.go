package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type base64Url struct {
	Port int    `json:"port"`
	Host string `json:"host"`
}

// for unused vars
func U(x ...any) {}

// {float,string} to integer
func (b *base64Url) UnmarshalJSON(data []byte) error {
	var aux struct {
		Add  string `json:"add"`
		Port any    `json:"port"`
		Host string `json:"host"`
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	b.Host = aux.Add
	if b.Host == "" {
		b.Host = aux.Host // should not happen
	}

	switch v := aux.Port.(type) {
	case int:
		b.Port = v
	case float64:
		b.Port = int(v)
	case string:
		p, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid port string: %w", err)
		}
		b.Port = p
	default:
		return fmt.Errorf("invalid port type")
	}

	return nil
}

// return "scheme://host:port" from proxy url
func urlFix(u string) string {
	ret := u
	isBase64 := ""
	pu, err := url.Parse(u)

	if err != nil {
		return ret
	}

	raw := strings.TrimPrefix(u, pu.Scheme+"://") // because url.Parse trimming "/"

	if d, err := base64.StdEncoding.DecodeString(raw); err == nil {
		isBase64 = string(d)
	}
	if d, err := base64.URLEncoding.DecodeString(raw); err == nil {
		isBase64 = string(d)
	}
	if d, err := base64.RawStdEncoding.DecodeString(raw); err == nil {
		isBase64 = string(d)
	}

	if isBase64 == "" {
		ret = pu.Host

		if pu.Scheme != "" {
			ret = fmt.Sprintf("%s://%s", pu.Scheme, pu.Host)
		}

		return ret
	}

	var b64Url base64Url
	err = json.Unmarshal([]byte(isBase64), &b64Url)
	if err != nil {
		log.Trace().
			Str("json", isBase64).
			Err(err).
			Send()
		return ret
	}

	ret = fmt.Sprintf("%s:%d", b64Url.Host, b64Url.Port)

	if pu.Scheme != "" {
		ret = fmt.Sprintf("%s://%s", pu.Scheme, ret)
	}

	return ret
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
