package dashboard

import (
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dbtools/dbtools/internal/config"
)

type Model struct {
	cfg     *config.Config
	collect CollectFunc
	table   table.Model
}

type rowsMsg []Row

func NewModel(cfg *config.Config, collect CollectFunc) Model {
	columns := []table.Column{
		{Title: "Target", Width: 16},
		{Title: "Status", Width: 48},
	}
	t := table.New(table.WithColumns(columns), table.WithFocused(true))
	return Model{cfg: cfg, collect: collect, table: t}
}

func (m Model) Init() tea.Cmd {
	return m.refreshCmd()
}

func (m Model) refreshCmd() tea.Cmd {
	cfg, collect := m.cfg, m.collect
	return func() tea.Msg {
		return rowsMsg(BuildRows(cfg, collect))
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, m.refreshCmd()
		}
	case rowsMsg:
		m.table.SetRows(ToTableRows(msg))
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	return m.table.View() + "\n(r: refresh, q: quit — read-only, no actions run from here)\n"
}
