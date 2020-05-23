// Copyright 2018 The conprof Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package pprofui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/conprof/conprof/scrape"
	"github.com/conprof/conprof/trace"
	"github.com/conprof/tsdb"
	"github.com/conprof/tsdb/labels"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/google/pprof/driver"
	"github.com/google/pprof/profile"
	"github.com/julienschmidt/httprouter"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/promql"
	"github.com/spf13/pflag"
)

type pprofUI struct {
	logger log.Logger
	db     *tsdb.DB
}

// NewServer creates a new Server backed by the supplied Storage.
func New(logger log.Logger, db *tsdb.DB) *pprofUI {
	s := &pprofUI{
		logger: logger,
		db:     db,
	}

	return s
}

func parsePath(reqPath string) (series, timestamp, remainingPath string) {
	parts := strings.Split(path.Clean(strings.TrimPrefix(reqPath, "/pprof/")), "/")
	if len(parts) < 2 {
		return "", "", ""
	}
	return parts[0], parts[1], strings.Join(parts[2:], "/")
}

func (p *pprofUI) PprofView(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	series, timestamp, remainingPath := parsePath(r.URL.Path)
	if !strings.HasPrefix(remainingPath, "/") {
		remainingPath = "/" + remainingPath
	}
	level.Debug(p.logger).Log("msg", "parsed path", "series", series, "timestamp", timestamp, "remainingPath", remainingPath)
	decodedSeriesName, err := base64.URLEncoding.DecodeString(series)
	if err != nil {
		msg := fmt.Sprintf("could not decode series name: %s", err)
		http.Error(w, msg, http.StatusNotFound)
		return
	}
	seriesLabelsString := string(decodedSeriesName)
	seriesLabels, err := promql.ParseMetricSelector(seriesLabelsString)
	if err != nil {
		msg := fmt.Sprintf("failed to parse series labels %v with error %v", seriesLabelsString, err)
		http.Error(w, msg, http.StatusNotFound)
		return
	}

	m := make(labels.Selector, len(seriesLabels))
	var profType string
	for i, l := range seriesLabels {
		m[i] = labels.NewEqualMatcher(l.Name, l.Value)
		if l.Name == scrape.ProfileType {
			profType = l.Value
		}
	}

	switch profType {
	case scrape.ProfileTraceType:
		q, err := p.db.Querier(0, math.MaxInt64)
		if err != nil {
			level.Error(p.logger).Log("err", err)
		}

		ss, err := q.Select(m...)
		if err != nil {
			level.Error(p.logger).Log("err", err)
			return
		}

		ok := ss.Next()
		if !ok {
			level.Error(p.logger).Log("msg", "err no seriesset")
			return
		}
		s := ss.At()
		i := s.Iterator()
		t, err := stringToInt(timestamp)
		if err != nil {
			level.Error(p.logger).Log("err timestamp", err)
			return
		}
		ok = i.Seek(t)
		if !ok {
			level.Error(p.logger).Log("err seek", err)
			return
		}
		_, buf := i.At()
		res, err := trace.Parse(bytes.NewBuffer(buf), "")
		if err != nil {
			level.Error(p.logger).Log("err parse trace", err)
			return
		}
		switch remainingPath {
		case "/usertasks":
			httpUserTasks(w, r, &res)
			return
		case "/usertask":
			httpUserTask(w, r, &res)
			return
		case "/userregions":
			httpUserRegions(w, r, &res)
			return
		case "/userregion":
			httpUserRegion(w, r, &res)
			return
		case "/goroutines":
			httpGoroutines(w, r, res.Events)
			return
		case "/goroutine":
			httpGoroutine(w, r, res.Events)
			return
		case "/trace":
			httpTrace(w, r)
			return
		case "/jsontrace":
			httpJsonTrace(w, r, &res)
			return
		case "/trace_viewer_html":
			httpTraceViewerHTML(w, r)
			return
		case "/webcomponents.min.js":
			webcomponentsJS(w, r)
			return
		default:
			ranges, err := splitTrace(res)
			if err != nil {
				level.Error(p.logger).Log("err split trace", err)
				return
			}

			traceUIhttpMain(w, r, ranges, series, timestamp)
			return
		}

	default:
		server := func(args *driver.HTTPServerArgs) error {
			handler, ok := args.Handlers[remainingPath]
			if !ok {
				return errors.Errorf("unknown endpoint %s", remainingPath)
			}
			handler.ServeHTTP(w, r)
			return nil
		}

		storageFetcher := func(_ string, _, _ time.Duration) (*profile.Profile, string, error) {
			var prof *profile.Profile

			q, err := p.db.Querier(0, math.MaxInt64)
			if err != nil {
				level.Error(p.logger).Log("err", err)
			}

			ss, err := q.Select(m...)
			if err != nil {
				level.Error(p.logger).Log("err", err)
				return nil, "", err
			}

			ok := ss.Next()
			if !ok {
				return nil, "", errors.New("could not get series set")
			}
			s := ss.At()
			i := s.Iterator()
			t, err := stringToInt(timestamp)
			if err != nil {
				return nil, "", err
			}
			ok = i.Seek(t)
			if !ok {
				return nil, "", errors.New("could not seek series")
			}
			_, buf := i.At()
			prof, err = profile.Parse(bytes.NewReader(buf))
			return prof, "", err
		}

		// Invoke the (library version) of `pprof` with a number of stubs.
		// Specifically, we pass a fake FlagSet that plumbs through the
		// given args, a UI that logs any errors pprof may emit, a fetcher
		// that simply reads the profile we downloaded earlier, and a
		// HTTPServer that pprof will pass the web ui handlers to at the
		// end (and we let it handle this client request).
		if err := driver.PProf(&driver.Options{
			Flagset: &pprofFlags{
				FlagSet: pflag.NewFlagSet("pprof", pflag.ExitOnError),
				args: []string{
					"--symbolize", "none",
					"--http", "localhost:0",
					"", // we inject our own target
				},
			},
			UI:         &fakeUI{},
			Fetch:      fetcherFn(storageFetcher),
			HTTPServer: server,
		}); err != nil {
			_, _ = w.Write([]byte(err.Error()))
		}
	}
}

type fetcherFn func(_ string, _, _ time.Duration) (*profile.Profile, string, error)

func (f fetcherFn) Fetch(s string, d, t time.Duration) (*profile.Profile, string, error) {
	return f(s, d, t)
}

func stringToInt(s string) (int64, error) {
	i, err := strconv.ParseInt(s, 10, 64)
	return int64(i), err
}
