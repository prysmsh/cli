package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/prysmsh/cli/internal/style"
)

var (
	confirmPromptStyle = lipgloss.NewStyle().Bold(true)
	confirmHintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	confirmYesStyle    = lipgloss.NewStyle().Foreground(style.Green).Bold(true)
	confirmNoStyle     = lipgloss.NewStyle().Foreground(style.Yellow).Bold(true)
)

type confirmModel struct {
	prompt    string
	confirmed bool
	done      bool
}

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			m.confirmed = true
			m.done = true
			return m, tea.Quit
		case "n", "N", "q", "esc", "ctrl+c":
			m.confirmed = false
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmModel) View() string {
	if m.done {
		if m.confirmed {
			return confirmPromptStyle.Render(m.prompt) + " " + confirmYesStyle.Render("yes") + "\n"
		}
		return confirmPromptStyle.Render(m.prompt) + " " + confirmNoStyle.Render("no") + "\n"
	}
	return confirmPromptStyle.Render(m.prompt) + " " + confirmHintStyle.Render("[y/N] ")
}

// Confirm shows an interactive y/N prompt. Returns true if the user pressed y.
func Confirm(prompt string) (bool, error) {
	m := confirmModel{prompt: prompt}
	p := tea.NewProgram(m)
	result, err := p.Run()
	if err != nil {
		return false, err
	}
	return result.(confirmModel).confirmed, nil
}

// --- Batch progress ---

type taskResult struct {
	Name    string
	Success bool
	Error   string
}

type batchDoneMsg struct{}

type batchModel struct {
	title   string
	tasks   []string
	results []taskResult
	current int
	done    bool
	runFn   func(name string) error
}

type batchTickMsg struct {
	idx int
	err error
}

func runBatchTask(m batchModel) tea.Cmd {
	return func() tea.Msg {
		idx := m.current
		if idx >= len(m.tasks) {
			return batchDoneMsg{}
		}
		err := m.runFn(m.tasks[idx])
		return batchTickMsg{idx: idx, err: err}
	}
}

func (m batchModel) Init() tea.Cmd {
	if len(m.tasks) == 0 {
		return func() tea.Msg { return batchDoneMsg{} }
	}
	return runBatchTask(m)
}

func (m batchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case batchTickMsg:
		r := taskResult{Name: m.tasks[msg.idx], Success: msg.err == nil}
		if msg.err != nil {
			r.Error = msg.err.Error()
		}
		m.results = append(m.results, r)
		m.current = msg.idx + 1
		if m.current >= len(m.tasks) {
			m.done = true
			return m, tea.Quit
		}
		return m, runBatchTask(m)
	case batchDoneMsg:
		m.done = true
		return m, tea.Quit
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m batchModel) View() string {
	var b strings.Builder

	b.WriteString(confirmPromptStyle.Render(m.title) + "\n\n")

	successMark := lipgloss.NewStyle().Foreground(style.Green).Render("✓")
	failMark := lipgloss.NewStyle().Foreground(style.Yellow).Render("✗")
	spinChar := lipgloss.NewStyle().Foreground(style.Cyan).Render("⠋")
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	for i, task := range m.tasks {
		if i < len(m.results) {
			r := m.results[i]
			if r.Success {
				b.WriteString(fmt.Sprintf("  %s %s\n", successMark, task))
			} else {
				b.WriteString(fmt.Sprintf("  %s %s  %s\n", failMark, task, dimStyle.Render(r.Error)))
			}
		} else if i == m.current {
			b.WriteString(fmt.Sprintf("  %s %s\n", spinChar, task))
		} else {
			b.WriteString(fmt.Sprintf("    %s\n", dimStyle.Render(task)))
		}
	}

	if m.done {
		succeeded := 0
		failed := 0
		for _, r := range m.results {
			if r.Success {
				succeeded++
			} else {
				failed++
			}
		}
		b.WriteString("\n")
		summary := fmt.Sprintf("Done: %d succeeded", succeeded)
		if failed > 0 {
			summary += fmt.Sprintf(", %d failed", failed)
		}
		b.WriteString(dimStyle.Render(summary) + "\n")
	}

	return b.String()
}

// RunBatch runs fn for each task name, showing live progress in the terminal.
// Returns the results.
func RunBatch(title string, tasks []string, fn func(name string) error) (succeeded int, failed int, err error) {
	if len(tasks) == 0 {
		return 0, 0, nil
	}

	m := batchModel{
		title: title,
		tasks: tasks,
		runFn: fn,
	}
	p := tea.NewProgram(m)
	result, err := p.Run()
	if err != nil {
		return 0, 0, err
	}

	final := result.(batchModel)
	for _, r := range final.results {
		if r.Success {
			succeeded++
		} else {
			failed++
		}
	}
	return succeeded, failed, nil
}

// --- Batch with detail (e.g. "3 node(s), agent 2/2") ---

type batchDetailResult struct {
	Name    string
	Detail  string
	Success bool
	Error   string
}

type batchDetailModel struct {
	title        string
	tasks        []string
	results      []batchDetailResult
	current      int
	done         bool
	runFn        func(name string) (detail string, err error)
	spinnerFrame int
}

type batchDetailTickMsg struct {
	idx   int
	detail string
	err   error
}

func runBatchDetailTask(m batchDetailModel) tea.Cmd {
	return func() tea.Msg {
		idx := m.current
		if idx >= len(m.tasks) {
			return batchDoneMsg{}
		}
		detail, err := m.runFn(m.tasks[idx])
		return batchDetailTickMsg{idx: idx, detail: detail, err: err}
	}
}

func (m batchDetailModel) Init() tea.Cmd {
	if len(m.tasks) == 0 {
		return func() tea.Msg { return batchDoneMsg{} }
	}
	// Start task and spinner tick so the spinner animates while the task runs
	return tea.Batch(runBatchDetailTask(m), tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return t }))
}

func (m batchDetailModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case batchDetailTickMsg:
		r := batchDetailResult{Name: m.tasks[msg.idx], Detail: msg.detail, Success: msg.err == nil}
		if msg.err != nil {
			r.Error = msg.err.Error()
		}
		m.results = append(m.results, r)
		m.current = msg.idx + 1
		if m.current >= len(m.tasks) {
			m.done = true
			return m, tea.Quit
		}
		return m, runBatchDetailTask(m)
	case batchDoneMsg:
		m.done = true
		return m, tea.Quit
	case time.Time:
		// Spinner tick: advance frame and request next tick so spinner animates while task runs
		m.spinnerFrame++
		if m.done {
			return m, nil
		}
		return m, tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return t })
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// truncateError returns a one-line summary of err for display (avoids kubectl stderr flood).
func truncateError(err string) string {
	const maxLen = 100
	firstLine := strings.SplitN(err, "\n", 2)[0]
	firstLine = strings.TrimSpace(firstLine)
	if len(firstLine) > maxLen {
		return firstLine[:maxLen] + "..."
	}
	return firstLine
}

func (m batchDetailModel) View() string {
	var b strings.Builder

	b.WriteString(confirmPromptStyle.Render(m.title) + "\n\n")

	successMark := lipgloss.NewStyle().Foreground(style.Green).Render("✓")
	failMark := lipgloss.NewStyle().Foreground(style.Yellow).Render("✗")
	spinChar := lipgloss.NewStyle().Foreground(style.Cyan).Render(spinnerFrames[m.spinnerFrame%len(spinnerFrames)])
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	for i, task := range m.tasks {
		if i < len(m.results) {
			r := m.results[i]
			if r.Success {
				if r.Detail != "" {
					b.WriteString(fmt.Sprintf("  %s %s  %s\n", successMark, task, dimStyle.Render(r.Detail)))
				} else {
					b.WriteString(fmt.Sprintf("  %s %s\n", successMark, task))
				}
			} else {
				b.WriteString(fmt.Sprintf("  %s %s  %s\n", failMark, task, dimStyle.Render(truncateError(r.Error))))
			}
		} else if i == m.current {
			b.WriteString(fmt.Sprintf("  %s %s\n", spinChar, task))
		} else {
			b.WriteString(fmt.Sprintf("    %s\n", dimStyle.Render(task)))
		}
	}

	if m.done {
		succeeded := 0
		failed := 0
		for _, r := range m.results {
			if r.Success {
				succeeded++
			} else {
				failed++
			}
		}
		b.WriteString("\n")
		summary := fmt.Sprintf("Done: %d succeeded", succeeded)
		if failed > 0 {
			summary += fmt.Sprintf(", %d failed", failed)
		}
		b.WriteString(dimStyle.Render(summary) + "\n")
	}

	return b.String()
}

// RunBatchWithDetail runs fn for each task name; on success the detail string is shown next to the task (e.g. "3 node(s), agent 2/2").
func RunBatchWithDetail(title string, tasks []string, fn func(name string) (detail string, err error)) (succeeded int, failed int, err error) {
	if len(tasks) == 0 {
		return 0, 0, nil
	}

	m := batchDetailModel{
		title: title,
		tasks: tasks,
		runFn: fn,
	}
	p := tea.NewProgram(m)
	result, err := p.Run()
	if err != nil {
		return 0, 0, err
	}

	final := result.(batchDetailModel)
	for _, r := range final.results {
		if r.Success {
			succeeded++
		} else {
			failed++
		}
	}
	return succeeded, failed, nil
}
