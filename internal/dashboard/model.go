package dashboard

import (
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/seanpham99/dbtools/internal/config"
)

type Model struct {
	cfg     *config.Config
	collect CollectFunc
	table   table.Model
	// refreshID monotonically increases per refresh so a slower, older
	// BuildRows result can never overwrite a newer one (bubbletea runs
	// Cmds on their own goroutines; two in-flight refreshes race).
	refreshID int
}

// rowsMsg carries the refresh it answers, so Update can drop stale results.
type rowsMsg struct {
	id   int
	rows []Row
}

func NewModel(cfg *config.Config, collect CollectFunc) Model {
	columns := []table.Column{
		{Title: "Target", Width: 16},
		{Title: "Status", Width: 48},
	}
	t := table.New(table.WithColumns(columns), table.WithFocused(true))
	return Model{cfg: cfg, collect: collect, table: t, refreshID: 1}
}

func (m Model) Init() tea.Cmd {
	return m.refreshCmd()
}

func (m Model) refreshCmd() tea.Cmd {
	cfg, collect, id := m.cfg, m.collect, m.refreshID
	return func() tea.Msg {
		return rowsMsg{id: id, rows: BuildRows(cfg, collect)}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.refreshID++
			return m, m.refreshCmd()
		}
	case rowsMsg:
		if msg.id == m.refreshID {
			m.table.SetRows(ToTableRows(msg.rows))
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	return m.table.View() + "\n(r: refresh, q: quit — read-only, no actions run from here)\n"
}
