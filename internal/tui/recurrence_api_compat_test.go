package tui

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// olderDaemonAPI passes every request to the daemon but reports version as
// its API schema version, and counts recurrence creates.
type olderDaemonAPI struct {
	inner   *http.Client
	version string
	creates *atomic.Int32
}

func (o olderDaemonAPI) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/recurrences") {
		o.creates.Add(1)
	}
	resp, err := o.inner.Do(req)
	if err != nil || req.URL.Path != "/api/v1/health" {
		return resp, err
	}
	defer resp.Body.Close()
	var health map[string]any
	if err := json.UnmarshalRead(resp.Body, &health); err != nil {
		return nil, err
	}
	health["api_schema_version"] = o.version
	body, err := json.Marshal(health)
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Del("Content-Length")
	return resp, nil
}

func (h *recurrenceHarness) reportAPI(version string) *atomic.Int32 {
	h.t.Helper()
	creates := &atomic.Int32{}
	transport := olderDaemonAPI{inner: h.env.HTTP, version: version, creates: creates}
	h.m.api = newConnectedUndoClient(h.t, NewClient(h.env.URL, &http.Client{Transport: transport}))
	return creates
}

// A daemon before API 0.37.0 ignores If-Match on a recurrence create, so the
// start would overwrite an issue edit made after the form read the issue.
func TestRecurrenceStartRequiresTheIfMatchAPI(t *testing.T) {
	for _, version := range []string{"0.36.0", "", "bad"} {
		t.Run("refused on API "+version, func(t *testing.T) {
			h := newRecurrenceHarness(t, viewDetail)
			h.open()
			creates := h.reportAPI(version)
			h.fill("FREQ=DAILY", "2026-11-09", "Europe/Paris")

			h.save()

			require.Equal(t, inputRecurrenceForm, h.m.input.kind, "the form stays open")
			assert.Contains(t, h.m.input.err, "upgrade the daemon")
			assert.Contains(t, h.m.input.err, "0.37.0")
			assert.Zero(t, creates.Load(), "nothing is sent to a daemon that would ignore If-Match")
			assert.Empty(t, h.rows())
			rule, _, _ := h.values()
			assert.Equal(t, "FREQ=DAILY", rule, "the input stays")
		})
	}
	t.Run("a supported API still refuses a start on a changed issue", func(t *testing.T) {
		h := newRecurrenceHarness(t, viewDetail)
		h.open()
		creates := h.reportAPI("0.37.0")
		h.patchMetadata(map[string]string{"scheduled_on": "2026-12-01"})
		h.fill("FREQ=DAILY", "2026-11-09", "Europe/Paris")

		h.save()

		require.Equal(t, inputRecurrenceForm, h.m.input.kind)
		assert.Contains(t, h.m.input.err, "changed since the form opened")
		assert.Empty(t, h.rows(), "nothing was written")

		h.save()

		assert.Equal(t, inputNone, h.m.input.kind)
		assert.Len(t, h.rows(), 1)
		assert.Equal(t, int32(1), creates.Load())
	})
	t.Run("change and stop still work on an older API", func(t *testing.T) {
		h := newRecurrenceHarness(t, viewDetail)
		h.startBehindTheTUI("FREQ=WEEKLY", "2026-11-09", "Europe/Paris")
		h.reportAPI("0.36.0")
		h.open()
		h.fill("FREQ=DAILY", "2026-11-09", "Europe/Paris")

		h.save()

		require.Equal(t, inputNone, h.m.input.kind, h.m.input.err)
		assert.Equal(t, "FREQ=DAILY", h.rows()[0].RRule)

		h.reload()
		h.open()
		h.key(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
		h.send(h.key(runeKey('y')))

		assert.Empty(t, h.rows())
	})
}
