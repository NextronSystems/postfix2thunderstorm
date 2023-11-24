package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"sync"
	"time"

	"github.com/antonmedv/expr"
	"github.com/antonmedv/expr/vm"
	"github.com/emersion/go-milter"

	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

type Env struct {
	FullMatch ThorThunderStormMatch
	Matches   []ThorThunderStormMatchItem
}

type MilterService struct {
	srv        milter.Server
	host, port string
	logger     *logrus.Entry
}

func NewMilterService(config Config, logger *logrus.Entry) *MilterService {
	srv := milter.Server{
		NewMilter: func() milter.Milter {
			prog, _ := expr.Compile(config.Expression, expr.Env(Env{})) // we checked the error @ init
			return &MilterSession{
				traceID: newTraceID(),
				config:  config,
				logger:  logger,
				skip:    false,
				start:   time.Now(),
				prog:    prog,
				headers: make(map[string][]string),
			}
		},
		Actions:  milter.OptQuarantine,
		Protocol: milter.OptNoUnknown | milter.OptNoHelo,
	}
	return &MilterService{srv: srv, host: config.MilterHost, port: config.MilterPort, logger: logger}
}

func (t *MilterService) Run() error {
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%s", t.host, t.port))
	if err != nil {
		return fmt.Errorf("was not able to start milter listener: %w", err)
	}
	t.logger.Info("milter-service started")
	return t.srv.Serve(l)
}

func (t *MilterService) Stop() {
	t.srv.Close()
	t.logger.Info("milter-service finished")
}

type MilterSession struct {
	traceID string
	config  Config
	prog    *vm.Program
	mux     sync.Mutex
	logger  *logrus.Entry

	// session properties
	start time.Time
	// mail properties
	from, rcptTo string
	raw          []byte
	headers      map[string][]string
	skip         bool
}

func (t *MilterSession) Connect(host string, family string, port uint16, addr net.IP, m *milter.Modifier) (milter.Response, error) {
	t.logger.WithFields(log.Fields{
		"host":     host,
		"family":   family,
		"port":     port,
		"ip":       addr.String(),
		"trace_id": t.traceID,
	}).Debug("Connection")
	return milter.RespContinue, nil
}

func (t *MilterSession) Helo(name string, m *milter.Modifier) (milter.Response, error) {
	t.logger.WithFields(log.Fields{
		"name":     name,
		"trace_id": t.traceID,
	}).Debug("Helo")
	return milter.RespContinue, nil
}

func (t *MilterSession) MailFrom(from string, m *milter.Modifier) (milter.Response, error) {
	if from == "" {
		t.logger.WithFields(log.Fields{
			"trace_id": t.traceID,
		}).Warn("Empty 'from'")
		return milter.RespContinue, nil
	}
	t.from = from
	t.logger.WithFields(log.Fields{
		"from":     t.from,
		"trace_id": t.traceID,
	}).Debug("MailFrom")
	return milter.RespContinue, nil
}

func (t *MilterSession) RcptTo(rcptTo string, m *milter.Modifier) (milter.Response, error) {
	if rcptTo == "" {
		t.logger.WithFields(log.Fields{
			"from":     t.from,
			"trace_id": t.traceID,
		}).Warn("Empty 'RcptTo'")
		return milter.RespContinue, nil
	}
	t.rcptTo = rcptTo
	t.logger.WithFields(log.Fields{
		"from":     t.from,
		"rcptTo":   rcptTo,
		"trace_id": t.traceID,
	}).Debug("RcptTo")
	return milter.RespContinue, nil
}

func (t *MilterSession) Header(name string, value string, m *milter.Modifier) (milter.Response, error) {
	t.logger.Debugf("header: %s: %s", name, value)
	t.headers[name] = []string{value}
	return milter.RespContinue, nil
}

func (t *MilterSession) Headers(h textproto.MIMEHeader, m *milter.Modifier) (milter.Response, error) {
	t.logger.Debugf("headers: %+v", h)
	for k, v := range h {
		t.headers[k] = v
	}
	return milter.RespContinue, nil
}

func (t *MilterSession) BodyChunk(chunk []byte, m *milter.Modifier) (milter.Response, error) {
	if t.skip {
		return milter.RespAccept, nil
	}

	t.mux.Lock()
	defer t.mux.Unlock()
	if len(t.raw)+len(chunk) > t.config.MaxFileSizeBytes {
		t.skip = true
		t.logger.
			WithField("trace_id", t.traceID).
			WithField("from", t.from).
			WithField("rcptTo", t.rcptTo).
			Warnf("filesize limit (%d) was exceeded - skip", t.config.MaxFileSizeBytes)
	} else {
		t.raw = append(t.raw, chunk...)
	}
	return milter.RespContinue, nil
}

func (t *MilterSession) Body(m *milter.Modifier) (milter.Response, error) {
	if t.skip {
		return milter.RespAccept, nil
	}

	t.mux.Lock()
	defer t.mux.Unlock()

	parts, err := NewParser(t.traceID, t.logger).ParseWithHeader(t.headers, t.raw)
	if err != nil && !errors.Is(err, ErrNoMultiPart) { // if its "no multipart" we still try to scan the raw mail
		t.logger.
			WithField("trace_id", t.traceID).
			WithField("from", t.from).
			WithField("rcptTo", t.rcptTo).
			WithField("error", err).
			Error("error parsing mail")
		return milter.RespAccept, nil
	}

	t.logger.Debugf("Got %d files", len(parts))
	for k, v := range parts {
		t.logger.Debugf("%s %d", k, len(v))
	}

	findingsCount := 0
	do_quarantine := false
	for fname, data := range parts {
		matches, err := ScanFile(t.config.ThorThunderStorm, fname, data, 1)
		if err != nil {
			t.logger.
				WithField("trace_id", t.traceID).
				WithField("from", t.from).
				WithField("rcptTo", t.rcptTo).
				WithField("error", err).
				Error("error scanning file with THOR Thunderstorm")
			continue
		}

		for _, m := range matches {
			findingsCount++
			subscores := []int{}
			for _, r := range m.Matches {
				subscores = append(subscores, r.Subscore)
			}

			returned, err := expr.Run(t.prog, Env{FullMatch: m, Matches: m.Matches})
			if err != nil {
				t.logger.
					WithField("trace_id", t.traceID).
					WithField("from", t.from).
					WithField("rcptTo", t.rcptTo).
					WithField("error", err).
					Error("failed to run expression")
			} else {
				quarantine, ok := returned.(bool)
				if ok && quarantine {
					do_quarantine = true
				}
			}

			raw, _ := json.Marshal(m)
			t.logger.WithFields(log.Fields{
				"from":           t.from,
				"rcptTo":         t.rcptTo,
				"trace_id":       t.traceID,
				"filename":       m.Context.File,
				"thor_level":     m.Level,
				"thor_module":    m.Module,
				"thor_msg":       m.Message,
				"thor_score":     m.Score,
				"thor_subscores": subscores,
				"thor_raw":       string(json.RawMessage(raw)),
			}).Warn("Finding")
		}
	}

	if do_quarantine {
		if t.config.ActiveMode {
			if err := m.Quarantine("Quarantine"); err != nil {
				t.logger.
					WithField("trace_id", t.traceID).
					WithField("from", t.from).
					WithField("rcptTo", t.rcptTo).
					WithField("error", err).
					Error("failed to quarantine email")
				do_quarantine = false
			}
		} else {
			t.logger.
				WithField("trace_id", t.traceID).
				WithField("from", t.from).
				WithField("rcptTo", t.rcptTo).
				Info("this mail would have been quarantined (use active mode)")
			do_quarantine = false
		}
	}

	l := t.logger.WithFields(log.Fields{
		"from":           t.from,
		"rcptTo":         t.rcptTo,
		"duration_ns":    time.Since(t.start),
		"size":           len(t.raw),
		"file_count":     len(parts),
		"findings_count": findingsCount,
		"trace_id":       t.traceID,
		"quarantined":    do_quarantine,
	})
	if do_quarantine {
		l.Warn("Quarantined email")
	} else {
		l.Info("Scanned email")
	}

	return milter.RespAccept, nil
}
