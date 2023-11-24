package service

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"

	"github.com/sirupsen/logrus"
)

type parser struct {
	parts       map[string][]byte
	bodyCounter int
	traceID     string
	logger      *logrus.Entry
}

func NewParser(traceID string, logger *logrus.Entry) *parser {
	p := parser{
		logger: logger,
	}
	p.parts = make(map[string][]byte)
	p.bodyCounter = 0
	p.traceID = traceID
	return &p
}

func randString(length int) string {
	var charset = "abcdefghijklmnopqrstuvwxyz" +
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func (p *parser) parsePart(mime_data io.Reader, boundary string) {
	p.logger.Debugf("boundary: %s", boundary)
	reader := multipart.NewReader(mime_data, boundary)
	if reader == nil {
		return
	}

	for {
		new_part, err := reader.NextPart()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			} else if strings.Contains(fmt.Sprintf("%v", err), "multipart: NextPart: EOF") {
				break // stmt above does not catch this? bug in go/src/mime/multipart/multipart.go:346 %v should be %w
				// seems to be fixed with: https://github.com/golang/go/commit/1e7e160d070443147ee38d4de530ce904637a4f3
			}
			p.parts["error"] = []byte(fmt.Errorf("was not able to reader.NextPart(): %v", err).Error())
			break
		}

		mediaType, params, err := mime.ParseMediaType(new_part.Header.Get("Content-Type"))
		if err != nil {
			p.parts["error"] = []byte(fmt.Errorf("was not able to parse media type: %v", err).Error())
			break
		}

		p.logger.Debugf("got mime type %s", mediaType)
		if strings.HasPrefix(mediaType, "multipart/") {
			p.parsePart(new_part, params["boundary"])

		} else if strings.HasPrefix(mediaType, "message/rfc822") {
			m, err := mail.ReadMessage(new_part)
			if err != nil {
				p.parts["error"] = []byte(fmt.Errorf("was not able to read 'message/rfc822' message: %v", err).Error())
				break
			}
			_, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
			if err != nil {
				p.parts["error"] = []byte(fmt.Errorf("was not able to read 'message/rfc822' header: %v", err).Error())
				break
			}
			p.parsePart(m.Body, params["boundary"])

		} else {
			part_data, err := io.ReadAll(new_part)
			if err != nil {
				p.parts["error"] = []byte(fmt.Errorf("was not able to io.ReadAll(new_part): %v", err).Error())
				break
			}

			filename := new_part.FileName()
			p.logger.Debugf("filename: '%s'", filename)
			if filename == "" {
				if strings.HasPrefix(mediaType, "text/plain") {
					name := fmt.Sprintf("text/plain-%d", p.bodyCounter)
					p.bodyCounter++
					p.addToResult(name, part_data)
					continue
				}

				if strings.HasPrefix(mediaType, "text/html") {
					name := fmt.Sprintf("text/html-%d", p.bodyCounter)
					p.bodyCounter++
					p.addToResult(name, part_data)
					continue
				}

				p.logger.
					WithField("trace_id", p.traceID).
					Warnf("unknown mime type: %s - scanning raw", mediaType)

				p.bodyCounter++
				p.addToResult(fmt.Sprintf("unknown-%d", p.bodyCounter), part_data)
				continue
			}

			content_transfer_encoding := strings.ToUpper(new_part.Header.Get("Content-Transfer-Encoding"))
			switch {
			case content_transfer_encoding == "BASE64":
				decoded_content, err := base64.StdEncoding.DecodeString(string(part_data))
				if err != nil {
					p.parts["error"] = []byte(fmt.Errorf("was not able to base64 decode: %v", err).Error())
					break
				} else {
					p.addToResult(filename, decoded_content)
				}
			case content_transfer_encoding == "QUOTED-PRINTABLE": // this most likely not needed as this is done transparently in mime/multipart/multipart.go:newPart(..)
				decoded_content, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(part_data)))
				if err != nil {
					p.parts["error"] = []byte(fmt.Errorf("was not able to quotedprintable decode: %v", err).Error())
					break
				} else {
					p.addToResult(filename, decoded_content)
				}
			default:
				p.addToResult(filename, part_data)
			}
		}
	}
}

func (p *parser) addToResult(key string, body []byte) {
	if _, ok := p.parts[key]; ok {
		p.parts[key+"_"+randString(3)] = body
	} else {
		p.parts[key] = body
	}
}

var ErrNoMultiPart error = fmt.Errorf("not multipart")

func (p *parser) ParseWithHeader(headers map[string][]string, body []byte) (map[string][]byte, error) {
	mediaType, params, err := mime.ParseMediaType(strings.Join(headers["Content-Type"], ""))
	if err != nil {
		return p.parts, fmt.Errorf("was not able to parse via mime.ParseMediaType(): %w", err)
	}

	if !strings.HasPrefix(mediaType, "multipart/") {
		p.addToResult("body-0", body)
		return p.parts, ErrNoMultiPart
	}

	return p.Parse(params["boundary"], body)
}

func (p *parser) Parse(boundary string, body []byte) (map[string][]byte, error) {
	p.parsePart(bytes.NewReader(body), boundary)
	if val, ok := p.parts["error"]; ok {
		err := fmt.Errorf(string(val))
		return p.parts, err
	}
	return p.parts, nil
}
