package tui

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.kenn.io/kata/internal/recurrence/recurrenceclient"
	"go.kenn.io/kata/pkg/client/generated"
	"go.kenn.io/kit/tui/splitlayout"
)

// latestEvent reads the newest event of eventType from the daemon's feed and
// decodes it the way the SSE reader does.
func (h *recurrenceHarness) latestEvent(eventType string) eventReceivedMsg {
	h.t.Helper()
	resp, err := h.api.PollEventsWithResponse(h.t.Context(), &generated.PollEventsRequestOptions{})
	require.NoError(h.t, err)
	var body struct {
		Events []jsontext.Value `json:"events"`
	}
	require.NoError(h.t, json.Unmarshal(resp.Body, &body))
	for _, raw := range slices.Backward(body.Events) {
		got := decodeEventReceived(frame{kind: frameEvent, data: raw})
		if got.eventType == eventType {
			return got
		}
	}
	h.t.Fatalf("no %s event in the daemon's feed", eventType)
	return eventReceivedMsg{}
}

// runDetailRefresh runs a scheduled detail refresh and feeds every read it
// makes to the model, as the program would once the debounce fires.
func (h *recurrenceHarness) runDetailRefresh(cmd tea.Cmd) {
	h.t.Helper()
	ticks := msgsOfType[detailRefreshTickMsg](cmd)
	require.Len(h.t, ticks, 1, "the detail pane schedules one refresh")
	next, work := h.m.handleDetailRefreshTick(ticks[0])
	h.m = next
	require.NotNil(h.t, work)
	batch, ok := work().(tea.BatchMsg)
	require.True(h.t, ok)
	for _, read := range batch {
		out, _ := h.m.Update(read())
		h.m = out.(Model)
	}
}

// sendCmd runs the one request cmd stands for, feeds the answer to the model
// and returns what the model does next.
func (h *recurrenceHarness) sendCmd(cmd tea.Cmd) tea.Cmd {
	h.t.Helper()
	require.NotNil(h.t, cmd)
	out, next := h.m.Update(msgOfType[mutationDoneMsg](h.t, cmd))
	h.m = out.(Model)
	return next
}

func TestRecurrenceDetailRefreshesWhenAnotherClientChangesOrStopsTheRecurrence(t *testing.T) {
	h := newRecurrenceHarness(t, viewDetail)
	rec := h.startBehindTheTUI("FREQ=WEEKLY", "2026-11-09", "Europe/Paris")
	h.refreshDetail()
	require.Contains(t, h.page(), "repeats: FREQ=WEEKLY")

	require.NoError(t, recurrenceclient.Change(t.Context(), h.api, h.issue.ProjectID, rec,
		recurrenceclient.Terms{Rule: "FREQ=DAILY", Start: "2026-11-09", Zone: "Europe/Paris", Actor: "bob"}))
	h.runDetailRefresh(h.m.maybeRefetchOpenDetail(h.latestEvent("recurrence.updated")))
	assert.Contains(t, h.page(), "repeats: FREQ=DAILY", "a change made elsewhere reaches the open detail")

	changed := h.rows()
	require.Len(t, changed, 1)
	require.NoError(t, recurrenceclient.Stop(t.Context(), h.api, h.issue.ProjectID, changed[0], "bob"))
	h.runDetailRefresh(h.m.maybeRefetchOpenDetail(h.latestEvent("recurrence.deleted")))
	assert.NotContains(t, h.page(), "repeats:", "a stop made elsewhere clears the open detail")
}

func TestRecurrenceEventsForOtherRecurrencesLeaveTheDetailAlone(t *testing.T) {
	h := newRecurrenceHarness(t, viewDetail)
	h.startBehindTheTUI("FREQ=WEEKLY", "2026-11-09", "Europe/Paris")
	h.refreshDetail()
	shown := h.m.detail.recurrence
	require.NotNil(t, shown)

	t.Run("another recurrence in the same project", func(t *testing.T) {
		msg := eventReceivedMsg{eventType: "recurrence.updated", projectID: h.issue.ProjectID, recurrenceUID: "01OTHERRECURRENCE00000000"}
		assert.Nil(t, h.m.maybeRefetchOpenDetail(msg))
	})
	t.Run("the same recurrence UID reported for another project", func(t *testing.T) {
		msg := eventReceivedMsg{eventType: "recurrence.updated", projectID: h.issue.ProjectID + 1, recurrenceUID: shown.UID}
		assert.Nil(t, h.m.maybeRefetchOpenDetail(msg))
	})
}

func TestRecurrenceEditFromTheListRefreshesTheSplitDetail(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(h *recurrenceHarness)
		check func(t *testing.T, page string)
	}{
		{"change", func(h *recurrenceHarness) {
			h.fill("FREQ=DAILY", "2026-11-09", "Europe/Paris")
			h.after = h.sendCmd(h.key(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}))
		}, func(t *testing.T, page string) { assert.Contains(t, page, "repeats: FREQ=DAILY") }},
		{"stop", func(h *recurrenceHarness) {
			h.key(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
			h.after = h.sendCmd(h.key(runeKey('y')))
		}, func(t *testing.T, page string) { assert.NotContains(t, page, "repeats:") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRecurrenceHarness(t, viewList)
			h.startBehindTheTUI("FREQ=WEEKLY", "2026-11-09", "Europe/Paris")
			h.m.layout, h.m.focus = splitlayout.Split, focusList
			h.refreshDetail()
			require.Contains(t, h.page(), "repeats: FREQ=WEEKLY")

			h.open()
			require.Equal(t, "list", h.m.input.target.origin, "the form opens from the list with the selection kept")
			tc.edit(h)

			require.Equal(t, inputNone, h.m.input.kind, "the save succeeded")
			h.runDetailRefresh(h.after)
			tc.check(t, h.page())
		})
	}
}
