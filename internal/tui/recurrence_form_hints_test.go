package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.kenn.io/kata/internal/recurrence/recurrenceclient"
)

func TestRecurrenceFormNamesTheRefusedActionAndKeepsTheDaemonDetail(t *testing.T) {
	makeCronParent := func(h *recurrenceHarness) {
		_, err := h.env.DB.ExecContext(t.Context(),
			`UPDATE issues SET metadata='{"cron.progress.01ARZ3NDEKTSV4RRFFQ69G5FAV":{}}' WHERE id=?`, h.issue.ID)
		require.NoError(t, err)
	}
	t.Run("start", func(t *testing.T) {
		h := newRecurrenceHarness(t, viewDetail)
		h.open()
		h.fill("every other tuesday", "2026-11-09", "Europe/Paris")

		h.save()

		assert.True(t, strings.HasPrefix(h.m.input.err, "Could not start repeat: "), h.m.input.err)
		assert.Contains(t, h.m.input.err, "every other tuesday", "the daemon's refusal stays")
		assert.NotContains(t, h.page(), "recurrence.start")
	})
	t.Run("change", func(t *testing.T) {
		h := newRecurrenceHarness(t, viewDetail)
		h.startBehindTheTUI("FREQ=WEEKLY", "2026-11-09", "Europe/Paris")
		h.open()
		h.fill("every other tuesday", "2026-11-09", "Europe/Paris")

		h.save()

		assert.True(t, strings.HasPrefix(h.m.input.err, "Could not change repeat: "), h.m.input.err)
		assert.Contains(t, h.m.input.err, "every other tuesday", "the daemon's refusal stays")
		assert.NotContains(t, h.page(), "recurrence.change")
	})
	t.Run("stop", func(t *testing.T) {
		h := newRecurrenceHarness(t, viewDetail)
		h.startBehindTheTUI("FREQ=WEEKLY", "2026-11-09", "Europe/Paris")
		h.open()
		makeCronParent(h)

		h.key(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
		h.send(h.key(runeKey('y')))

		assert.True(t, strings.HasPrefix(h.m.input.err, "Could not stop repeat: "), h.m.input.err)
		assert.Contains(t, h.m.input.err, recurrenceclient.CronParentRefusal, "the refusal stays")
		assert.NotContains(t, h.page(), "recurrence.stop")
		assert.Len(t, h.rows(), 1, "nothing was stopped")
	})
}

// TestRecurrenceActionHintsShowOnce checks that the stop shortcut and the
// stop confirmation's answers appear exactly once on screen: in the footer
// when the footer is visible, in the form or modal when it is not.
func TestRecurrenceActionHintsShowOnce(t *testing.T) {
	sizes := []struct {
		name          string
		width, height int
		footer        bool
	}{
		{"wide and tall", 120, 40, true},
		{"split-sized", 200, 50, true},
		{"too narrow for a footer", 79, 40, false},
		{"narrowest form", 57, 40, false},
		{"the shortest form", 120, 12, true},
	}
	for _, view := range []viewID{viewList, viewDetail} {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("view%d %s", view, size.name), func(t *testing.T) {
				h := newRecurrenceHarness(t, view)
				h.startBehindTheTUI("FREQ=WEEKLY", "2026-11-09", "Europe/Paris")
				h.m.width, h.m.height = size.width, size.height
				h.open()

				page := h.page()
				assert.Equal(t, 1, strings.Count(page, "stop repeating"), "form hints at %dx%d:\n%s", size.width, size.height, page)
				footerHasStop := strings.Contains(lastLines(page, 3), "stop repeating")
				assert.Equal(t, size.footer, footerHasStop, "the footer carries the shortcut only when visible:\n%s", page)

				h.key(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
				require.Equal(t, modalStopRecurrence, h.m.modal)
				page = h.page()
				if size.footer {
					assert.NotContains(t, page, "[Y]", "the footer already lists y and n:\n%s", page)
					assert.Contains(t, lastLines(page, 3), "stop repeating")
				} else {
					assert.Contains(t, page, "[Y] Stop repeating    [N] Keep", "no footer, so the modal lists the answers:\n%s", page)
				}
			})
		}
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return strings.Join(lines[max(len(lines)-n, 0):], "\n")
}

// A refusal adds lines to the form; at the shortest height the form then
// covers the footer, so the form and the stop confirmation list their keys.
func TestRecurrenceActionHintsMoveIntoTheFormWhenItCoversTheFooter(t *testing.T) {
	h := newRecurrenceHarness(t, viewDetail)
	h.startBehindTheTUI("FREQ=WEEKLY", "2026-11-09", "Europe/Paris")
	h.m.width, h.m.height = 120, 12
	h.open()
	h.fill("every other tuesday", "2026-11-09", "Europe/Paris")
	h.save()
	require.NotEmpty(t, h.m.input.err)

	page := h.page()
	assert.Equal(t, 1, strings.Count(page, "stop repeating"), page)
	assert.Contains(t, page, "· ctrl+x stop repeating", "the form lists the shortcut itself:\n%s", page)

	h.key(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	require.Equal(t, modalStopRecurrence, h.m.modal)
	assert.Contains(t, h.page(), "[Y] Stop repeating    [N] Keep", "the form hides the footer's y and n:\n%s", h.page())
}

// A form tall enough to reach the footer rows on a wide terminal still leaves
// the footer's keys visible beside it, so the form does not repeat them.
func TestRecurrenceActionHintsStayInAFooterTheFormDoesNotReach(t *testing.T) {
	h := newRecurrenceHarness(t, viewDetail)
	h.startBehindTheTUI("FREQ=WEEKLY", "2026-11-09", "Europe/Paris")
	h.m.width, h.m.height = 240, 12
	h.open()
	h.fill("every other tuesday", "2026-11-09", "Europe/Paris")
	h.save()
	require.NotEmpty(t, h.m.input.err)

	page := h.page()
	assert.Equal(t, 1, strings.Count(page, "stop repeating"), page)
	assert.Contains(t, lastLines(page, 1), "ctrl+x stop repeating", "the footer keeps the shortcut:\n%s", page)

	h.key(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	require.Equal(t, modalStopRecurrence, h.m.modal)
	assert.NotContains(t, h.page(), "[Y]", "the footer lists y and n:\n%s", h.page())
}

// Below the form's minimum size the form collapses to its active field, and
// no footer is drawn, so the collapsed form still names the stop shortcut.
func TestRecurrenceTinyFormKeepsTheStopShortcut(t *testing.T) {
	for _, size := range []struct{ width, height int }{{79, 11}, {120, 9}, {39, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			h := newRecurrenceHarness(t, viewDetail)
			h.startBehindTheTUI("FREQ=WEEKLY", "2026-11-09", "Europe/Paris")
			h.m.width, h.m.height = size.width, size.height
			h.open()

			page := h.page()
			assert.Equal(t, 1, strings.Count(page, "stop repeating"), page)
		})
	}
}
