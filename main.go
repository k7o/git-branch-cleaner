package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type state int

const (
	stateList state = iota
	stateConfirm
	stateDetail
)

type Model struct {
	state        state
	table        table.Model
	branches     []Branch
	repoPath     string
	keys         keyMap
	err          error
	showError    bool
	selected     map[int]bool
	cursor       int
	lastWidth    int
	maxBranchLen int
	msgWidth     int
}

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Detail key.Binding
	Delete key.Binding
	Quit   key.Binding
	Help   key.Binding
	Yes    key.Binding
	No     key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "move down"),
		),
		Select: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "select/deselect"),
		),
		Detail: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "show details"),
		),
		Delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete selected"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "toggle help"),
		),
		Yes: key.NewBinding(
			key.WithKeys("y", "Y"),
			key.WithHelp("y", "yes"),
		),
		No: key.NewBinding(
			key.WithKeys("n", "N", "esc"),
			key.WithHelp("n/esc", "no"),
		),
	}
}

var (
	baseStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240"))

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("212")).
			Bold(true)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("170")).
			Bold(true)

	mergedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("34"))

	unmergedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208"))

	protectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Strikethrough(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	dialogStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(1, 2).
			Width(50)
)

func initialModel() Model {
	if err := validateGitEnvironment(); err != nil {
		return Model{
			err:       err,
			showError: true,
		}
	}

	branches, err := getAllBranches()
	if err != nil {
		return Model{
			err:       err,
			showError: true,
		}
	}

	repoPath, err := getRepositoryPath()
	if err != nil {
		repoPath = "Unknown"
	}

	sortBranches(branches)

	t := table.New(
		table.WithFocused(true),
		table.WithHeight(20),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	m := Model{
		state:    stateList,
		table:    t,
		branches: branches,
		repoPath: repoPath,
		keys:     defaultKeyMap(),
		selected: make(map[int]bool),
	}
	m.maxBranchLen = maxBranchNameLen(branches)
	return m.layoutTable(76)
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.state {
		case stateList:
			return m.updateList(msg)
		case stateConfirm:
			return m.updateConfirm(msg)
		case stateDetail:
			return m.updateDetail(msg)
		}
	case tea.WindowSizeMsg:
		m = m.layoutTable(msg.Width - 4)
		m.table.SetHeight(msg.Height - 8)
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Select):
		cursor := m.table.Cursor()
		if cursor >= 0 && cursor < len(m.branches) && !m.branches[cursor].IsDefault {
			m.branches[cursor].Selected = !m.branches[cursor].Selected
			m.selected[cursor] = m.branches[cursor].Selected
			m = m.updateTableRows()
		}
		return m, nil
	case key.Matches(msg, m.keys.Detail):
		if cursor := m.table.Cursor(); cursor >= 0 && cursor < len(m.branches) {
			m.state = stateDetail
		}
		return m, nil
	case key.Matches(msg, m.keys.Delete):
		selectedBranches := m.getSelectedBranches()
		if len(selectedBranches) > 0 {
			m.state = stateConfirm
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.No), key.Matches(msg, m.keys.Detail):
		m.state = stateList
	}
	return m, nil
}

func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Yes):
		selectedBranches := m.getSelectedBranches()
		branchNames := make([]string, len(selectedBranches))
		for i, branch := range selectedBranches {
			branchNames[i] = branch.Name
		}

		err := deleteBranches(branchNames)
		if err != nil {
			m.err = err
			m.showError = true
			m.state = stateList
			return m, nil
		}

		branches, err := getAllBranches()
		if err != nil {
			m.err = err
			m.showError = true
			m.state = stateList
			return m, nil
		}

		sortBranches(branches)
		m.branches = branches
		m.selected = make(map[int]bool)
		m.maxBranchLen = maxBranchNameLen(branches)
		m = m.layoutTable(m.lastWidth)
		m.state = stateList

	case key.Matches(msg, m.keys.No):
		m.state = stateList
	}

	return m, nil
}

func (m Model) View() string {
	if m.showError {
		return errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	}

	switch m.state {
	case stateList:
		return m.viewList()
	case stateConfirm:
		return m.viewConfirm()
	case stateDetail:
		return m.viewDetail()
	}

	return ""
}

func (m Model) viewList() string {
	title := titleStyle.Render("Git Branch Cleaner")
	repo := fmt.Sprintf("Repository: %s", m.repoPath)

	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("space: select • enter: details • d: delete selected • q: quit • ↑/↓: navigate")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		repo,
		"",
		baseStyle.Render(m.table.View()),
		"",
		help,
	)

	return content
}

func (m Model) viewDetail() string {
	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.branches) {
		return m.viewList()
	}
	branch := m.branches[cursor]

	status := "Unmerged"
	if branch.IsMerged {
		status = "Merged"
	}
	if branch.IsDefault {
		status = "Default branch (protected)"
	}

	ds := dialogStyle
	ds = ds.Width(m.clampedDialogWidth(60))

	content := lipgloss.JoinVertical(lipgloss.Left,
		fmt.Sprintf("Branch:  %s", branch.Name),
		fmt.Sprintf("Status:  %s", status),
		fmt.Sprintf("Author:  %s", branch.Author),
		fmt.Sprintf("Date:    %s", branch.LastCommitDate.Format("2006-01-02 15:04")),
		fmt.Sprintf("Ahead:   %d commits", branch.CommitsAhead),
		"",
		"Last commit:",
		branch.LastCommitMsg,
		"",
		"(enter/esc/n: back)",
	)

	// Render the dialog on top of the list instead of replacing it.
	base := m.viewList()
	return overlay(base, ds.Render(content))
}

// clampedDialogWidth returns a dialog width that fits the terminal
// (accounting for the border) with a sane minimum.
func (m Model) clampedDialogWidth(defaultWidth int) int {
	w := defaultWidth
	if m.lastWidth > 0 && m.lastWidth-6 < w {
		w = m.lastWidth - 6
	}
	if w < 20 {
		w = 20
	}
	return w
}

// overlay composites a dialog block on top of the base screen, centered.
// Base and dialog lines contain ANSI styling, so plain string splicing
// would corrupt the layout; the ansi.Truncate helpers are cell-width aware
// and preserve styles across the cut points. Note: lipgloss.Width (not
// ansi.StringWidth) is used for measuring — ansi.StringWidth overcounts
// styled lipgloss output.
func overlay(base, dialog string) string {
	width := lipgloss.Width(base)
	height := lipgloss.Height(base)
	dW := lipgloss.Width(dialog)
	dH := lipgloss.Height(dialog)
	x := (width - dW) / 2
	y := (height - dH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	baseLines := strings.Split(base, "\n")
	dialogLines := strings.Split(dialog, "\n")
	out := make([]string, len(baseLines))
	for i, line := range baseLines {
		// Pad to full width so the splice never runs past the line end.
		if w := lipgloss.Width(line); w < width {
			line += strings.Repeat(" ", width-w)
		}
		if i >= y && i < y+dH && i-y < len(dialogLines) {
			left := ansi.Truncate(line, x, "")
			right := ansi.TruncateLeft(line, x+dW, "")
			out[i] = left + dialogLines[i-y] + right
		} else {
			out[i] = line
		}
	}
	return strings.Join(out, "\n")
}

func (m Model) viewConfirm() string {
	selectedBranches := m.getSelectedBranches()

	var content strings.Builder
	content.WriteString("The following branches will be deleted:\n\n")

	hasUnmerged := false
	for _, branch := range selectedBranches {
		status := "Merged"
		if !branch.IsMerged {
			status = "Unmerged"
			hasUnmerged = true
		}
		content.WriteString(fmt.Sprintf("• %s (%s)\n", branch.Name, status))
	}

	if hasUnmerged {
		content.WriteString("\n⚠️  Warning: Some branches are unmerged!\n")
	}

	content.WriteString("\nContinue? (y/N)")

	dialog := dialogStyle.Render(content.String())

	base := m.viewList()
	return overlay(base, dialog)
}

func (m Model) getSelectedBranches() []Branch {
	var selected []Branch
	for _, branch := range m.branches {
		if branch.Selected {
			selected = append(selected, branch)
		}
	}
	return selected
}

func (m Model) updateTableRows() Model {
	m.table.SetRows(m.buildRows())
	return m
}

func (m Model) buildRows() []table.Row {
	rows := make([]table.Row, len(m.branches))
	for i, branch := range m.branches {
		checkbox := "[ ]"
		if branch.Selected {
			checkbox = "[x]"
		}
		if branch.IsDefault {
			checkbox = "[-]"
		}

		status := "Unmerged"
		if branch.IsMerged {
			status = "Merged"
		}
		if branch.IsDefault {
			status = "-"
		}

		commitsStr := fmt.Sprintf("%d↑", branch.CommitsAhead)
		if branch.IsDefault {
			commitsStr = "-"
		}

		rows[i] = table.Row{
			checkbox,
			branch.Name,
			status,
			branch.LastCommitDate.Format("2006-01-02"),
			commitsStr,
			truncateString(branch.LastCommitMsg, m.msgWidth),
		}
	}
	return rows
}

// sortBranches orders branches oldest-commit first, so stale branches
// (the usual deletion targets) sit at the top of the list. The default
// branch is pinned to the bottom so it stays out of the way.
func sortBranches(branches []Branch) {
	sort.Slice(branches, func(i, j int) bool {
		if branches[i].IsDefault != branches[j].IsDefault {
			return branches[j].IsDefault // default branch last
		}
		return branches[i].LastCommitDate.Before(branches[j].LastCommitDate)
	})
}

// maxBranchNameLen returns the length of the longest branch name so the
// Branch column can always fit it without clipping.
func maxBranchNameLen(branches []Branch) int {
	maxLen := 0
	for _, b := range branches {
		if len(b.Name) > maxLen {
			maxLen = len(b.Name)
		}
	}
	return maxLen
}

// layoutTable sizes the columns to the terminal width: the Branch column
// is wide enough for the longest branch name, and the leftover space goes
// to the Message column instead of the other way around.
//
// bubbles/table renders each cell and header with 1 space of padding on
// both sides (2 per column), and baseStyle adds a 2-char border, so the
// rendered table is sum(column widths) + 2*len(cols) + 2 wide. All of that
// must fit inside the terminal width or the UI wraps and looks stretched.
func (m Model) layoutTable(width int) Model {
	if width <= 0 {
		width = 76
	}
	m.lastWidth = width

	const (
		padding   = 2 * 6           // 1 space left + right per cell/header, 6 columns
		border    = 2               // baseStyle NormalBorder around the table
		fixedCols = 8 + 10 + 12 + 8 // Select, Status, Date, Commits
		minBranch = 6
		minMsg    = 6
	)

	budget := width - padding - border
	if budget < 0 {
		budget = 0
	}

	branchWidth := m.maxBranchLen + 2
	if branchWidth < minBranch {
		branchWidth = minBranch
	}
	msgWidth := budget - fixedCols - branchWidth
	if msgWidth < minMsg {
		// Not enough room for full branch names: keep the message readable
		// and let the branch column truncate ("…") instead of overflowing.
		msgWidth = minMsg
		branchWidth = budget - fixedCols - msgWidth
		if branchWidth < minBranch {
			branchWidth = minBranch
		}
	}

	cols := []table.Column{
		{Title: "Select", Width: 8},
		{Title: "Branch", Width: branchWidth},
		{Title: "Status", Width: 10},
		{Title: "Date", Width: 12},
		{Title: "Commits", Width: 8},
		{Title: "Message", Width: msgWidth},
	}
	// On very narrow terminals the desired widths still exceed the budget;
	// shrink the widest columns (floor of 1) so the table never overflows.
	total := 0
	for _, c := range cols {
		total += c.Width
	}
	for total > budget {
		widest := -1
		idx := -1
		for j, c := range cols {
			if c.Width > widest {
				widest = c.Width
				idx = j
			}
		}
		if cols[idx].Width <= 1 {
			break
		}
		cols[idx].Width--
		total--
	}
	m.msgWidth = cols[5].Width

	m.table.SetWidth(width)
	m.table.SetColumns(cols)
	m.table.SetRows(m.buildRows())
	return m
}

func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
