package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/tonobo/calterm/internal/model"
)

// RenderDayPane lists the events on one day, for the pane shown beneath the
// month and week grids. It reuses renderAgendaRow so its rows match the
// agenda exactly, and never emits more than height lines or width cells --
// the caller's structural guarantee, not something reasoned about case by
// case.
//
// occs must be sorted by Start: model.OnDay filters but does not sort, so
// the rows below appear in occs' own order, not necessarily chronological.
// Production is fine -- the store's index is sorted -- but this function
// itself does no sorting of its own.
func RenderDayPane(occs []model.Occurrence, day time.Time, width, height int, now time.Time, loc *time.Location, st Styles, names map[string]string) string {
	if loc == nil {
		loc = time.UTC
	}
	if width <= 0 || height <= 0 {
		return ""
	}

	local := day.In(loc)
	_, week := local.ISOWeek()
	label := fmt.Sprintf("%s · Wk %d", local.Format("Mon 2 January"), week)
	lines := []string{st.Dim.Render(label)}

	dayOccs := model.OnDay(occs, day, loc)
	// budget is the room left for event lines once the date line is paid for.
	budget := height - 1

	switch {
	case len(dayOccs) == 0:
		if budget > 0 {
			lines = append(lines, st.Dim.Render("No events"))
		}
	case len(dayOccs) <= budget:
		for _, o := range dayOccs {
			lines = append(lines, renderAgendaRow(o, false, width, loc, st, names))
		}
	default:
		// Reserve one line to report what didn't fit, mirroring the "+N"
		// overflow the month cells and the which-key popup already use.
		shown := budget - 1
		if shown < 0 {
			shown = 0
		}
		for _, o := range dayOccs[:shown] {
			lines = append(lines, renderAgendaRow(o, false, width, loc, st, names))
		}
		if budget > 0 {
			lines = append(lines, st.Dim.Render(fmt.Sprintf("+%d more", len(dayOccs)-shown)))
		}
	}

	// Structural guarantee: clip every line to width and the block to
	// height, no matter what the arithmetic above produced.
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = truncate(lines[i], width)
	}
	return strings.Join(lines, "\n")
}
