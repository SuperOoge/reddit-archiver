// Command tui is a terminal browser for the scraped post database.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/SuperOoge/reddit-archiver/internal/config"
	"github.com/SuperOoge/reddit-archiver/internal/db"
	"github.com/SuperOoge/reddit-archiver/internal/models"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gorm.io/gorm"
)

func main() {
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", "config.json", "path to config file")
	envPath := flag.String("env", ".env", "path to .env file")
	flag.Parse()

	cfg, err := config.Load(*configPath, *envPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return 1
	}

	gormDB, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		return 1
	}

	if _, err := tea.NewProgram(newModel(gormDB), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "run tui: %v\n", err)
		return 1
	}
	return 0
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	helpStyle     = lipgloss.NewStyle().Faint(true)
)

type model struct {
	db      *gorm.DB
	posts   []models.Post
	cursor  int
	loadErr error
}

func newModel(gormDB *gorm.DB) model {
	return model{db: gormDB}
}

type postsLoadedMsg struct {
	posts []models.Post
	err   error
}

func loadPosts(gormDB *gorm.DB) tea.Cmd {
	return func() tea.Msg {
		var posts []models.Post
		err := gormDB.Order("created_at DESC").Limit(200).Find(&posts).Error
		return postsLoadedMsg{posts: posts, err: err}
	}
}

func (m model) Init() tea.Cmd {
	return loadPosts(m.db)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case postsLoadedMsg:
		m.posts = msg.posts
		m.loadErr = msg.err
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.posts)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.loadErr != nil {
		return fmt.Sprintf("failed to load posts: %v\n", m.loadErr)
	}

	var b []byte
	b = append(b, titleStyle.Render(fmt.Sprintf("reddit-archiver — %d post(s)", len(m.posts)))...)
	b = append(b, '\n', '\n')

	if len(m.posts) == 0 {
		b = append(b, "no posts yet — run the scraper first\n"...)
	}

	for i, p := range m.posts {
		line := fmt.Sprintf("[%s] r/%-20s %s", p.Source, p.Subreddit, p.Title)
		if i == m.cursor {
			line = selectedStyle.Render(line)
		}
		b = append(b, line...)
		b = append(b, '\n')
	}

	b = append(b, '\n')
	b = append(b, helpStyle.Render("↑/↓ move  ·  q quit")...)

	return string(b)
}
