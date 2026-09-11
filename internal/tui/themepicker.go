package tui

// RenderThemePicker draws the list of every theme the registry knows about,
// highlighting the one under the cursor. Like RenderCalendars, it is a pure
// function of its arguments so the layout can be asserted without driving a
// terminal.
func RenderThemePicker(names []string, cursor, width, height int, st Styles) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	header := st.DayHeader.Render("Theme")

	if len(names) == 0 {
		lines := []string{header, "", st.Dim.Render("No themes registered.")}
		return clampBlock(lines, width, height)
	}

	footer := st.Dim.Render("j/k preview · enter apply · esc cancel")

	// Same shape as RenderCalendars: build every scrollable line first,
	// remembering which one the cursor landed on, then window the result so
	// a long list never scrolls the cursor (or the footer) off-screen.
	var body []string
	cursorLine := 0
	for i, name := range names {
		if i == cursor {
			cursorLine = len(body)
		}
		body = append(body, renderThemeRow(name, i == cursor, width, st))
	}

	budget := height - 3 // header, blank, footer
	if budget < 1 {
		budget = 1
	}
	lines := append([]string{header}, scrollWindow(body, cursorLine, budget)...)
	lines = append(lines, "", footer)
	return clampBlock(lines, width, height)
}

func renderThemeRow(name string, selected bool, width int, st Styles) string {
	marker := "  "
	style := st.Summary
	if selected {
		marker = cursorMarker + " "
		style = st.Selected
	}
	return truncate(marker+style.Render(name), width)
}

// indexOf returns the index of name in names, or 0 if it is not present --
// a picker cursor has to start somewhere, and 0 is as good a default as any
// when the committed theme has somehow dropped out of the registry.
func indexOf(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return 0
}
