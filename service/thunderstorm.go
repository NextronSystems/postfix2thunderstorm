package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"
)

var client = http.DefaultClient

func init() {
	client.Timeout = 60 * time.Second
}

var ErrThorRetryExceeded = fmt.Errorf("THOR retry exceeded - THOR seems to be down")

func ScanFile(tturl string, fname string, body []byte, retry int) ([]ThorThunderStormMatch, error) {
	var matches []ThorThunderStormMatch

	if retry < 0 {
		return matches, ErrThorRetryExceeded
	}

	req, err := newFileUploadRequest(tturl, fname, body)
	if err != nil {
		return nil, fmt.Errorf("creating file upload HTTP req: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending HTTP req: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		time.Sleep(getRetryHeader(req.Header))
		return ScanFile(tturl, fname, body, retry-1)
	}

	if resp.StatusCode != http.StatusOK {
		time.Sleep(getRetryHeader(req.Header))
		return ScanFile(tturl, fname, body, retry-1)
	}

	responseBody, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(responseBody, &matches); err != nil {
		return matches, fmt.Errorf("parsing json resp: %w", err)
	}

	return matches, nil

}

func getRetryHeader(header http.Header) time.Duration {
	is := header.Get("Retry-After")
	i, err := strconv.ParseInt(is, 10, 0)
	if err != nil {
		return 10 // sec hardcoded
	}
	return time.Duration(int(i)) * time.Second
}

func newFileUploadRequest(url string, fname string, raw []byte) (*http.Request, error) {
	file := bytes.NewReader(raw)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", fname)
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(part, file)
	err = writer.Close()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, err
}

type ThorThunderStormMatch struct {
	Level   string `json:"level"`
	Module  string `json:"module"`
	Message string `json:"message"`
	Score   int    `json:"score"`
	Context struct {
		Ext          string `json:"ext"`
		File         string `json:"file"`
		Firstbytes   string `json:"firstbytes"`
		Md5          string `json:"md5"`
		ReasonsCount int    `json:"reasons_count"`
		SampleID     int    `json:"sample_id"`
		Sha1         string `json:"sha1"`
		Sha256       string `json:"sha256"`
		Size         int    `json:"size"`
		Type         string `json:"type"`
	} `json:"context"`
	Matches []ThorThunderStormMatchItem `json:"matches"`
}

type ThorThunderStormMatchItem struct {
	Author   string   `json:"author"`
	Matched  []string `json:"matched"`
	Reason   string   `json:"reason"`
	Ref      string   `json:"ref"`
	Ruledate string   `json:"ruledate"`
	Rulename string   `json:"rulename"`
	Sigclass string   `json:"sigclass"`
	Sigtype  string   `json:"sigtype"`
	Subscore int      `json:"subscore"`
	Tags     []string `json:"tags"`
}
