// Command tui is a terminal browser for the scraped post database.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

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
	filterStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
)

type model struct {
	db       *gorm.DB
	posts    []models.Post // all loaded posts
	filtered []models.Post // currently visible (derived from posts + filter)
	cursor   int
	loadErr  error

	filtering bool   // true when the filter input is active
	query     string // current filter text
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

// applyFilter narrows m.posts to those whose Subreddit or Title contains
// the query string (case-insensitive). When query is empty it returns all.
func applyFilter(posts []models.Post, query string) []models.Post {
	if query == "" {
		return posts
	}
	q := strings.ToLower(query)
	out := make([]models.Post, 0, len(posts))
	for _, p := range posts {
		if strings.Contains(strings.ToLower(p.Subreddit), q) ||
			strings.Contains(strings.ToLower(p.Title), q) {
			out = append(out, p)
		}
	}
	return out
}

func (m model) Init() tea.Cmd {
	return loadPosts(m.db)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case postsLoadedMsg:
		m.posts = msg.posts
		m.filtered = applyFilter(m.posts, m.query)
		m.loadErr = msg.err
		return m, nil
	case tea.KeyMsg:
		// --- filter mode key handling --------------------------------------
		if m.filtering {
			switch msg.String() {
			case "esc":
				// Close the filter input, clear the query, show all posts.
				m.filtering = false
				m.query = ""
				m.filtered = applyFilter(m.posts, m.query)
				m.cursor = 0
				return m, nil
			case "enter":
				// Accept the current filter and return to list navigation.
				m.filtering = false
				m.cursor = 0
				return m, nil
			case "backspace":
				if len(m.query) > 0 {
					m.query = m.query[:len(m.query)-1]
				} else {
					// Empty query + backspace acts like esc.
					m.filtering = false
					m.query = ""
				}
				m.filtered = applyFilter(m.posts, m.query)
				m.cursor = 0
				return m, nil
			default:
				// Only accept printable characters (len 1 after RuneMsg).
				if len(msg.String()) == 1 {
					m.query += msg.String()
					m.filtered = applyFilter(m.posts, m.query)
					m.cursor = 0
					return m, nil
				}
				return m, nil
			}
		}

		// --- normal navigation key handling ---------------------------------
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "/":
			// Open filter input.
			m.filtering = true
			m.query = ""
			m.cursor = 0
			return m, nil
		case "esc":
			// If there's an active filter, clear it.
			if m.query != "" {
				m.query = ""
				m.filtered = applyFilter(m.posts, m.query)
				m.cursor = 0
				return m, nil
			}
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		case "pgup":
			m.cursor -= 10
			if m.cursor < 0 {
				m.cursor = 0
			}
		case "pgdown":
			m.cursor += 10
			if m.cursor >= len(m.filtered) {
				m.cursor = len(m.filtered) - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
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

	// --- filter bar -------------------------------------------------------
	if m.filtering {
		b = append(b, filterStyle.Render(fmt.Sprintf("  filter: %s█", m.query))...)
		b = append(b, '\n', '\n')
	} else if m.query != "" {
		b = append(b, filterStyle.Render(fmt.Sprintf("  filter: %s  (%d matches, / to edit, esc to clear)", m.query, len(m.filtered)))...)
		b = append(b, '\n', '\n')
	}

	// --- title ------------------------------------------------------------
	visible := len(m.filtered)
	total := len(m.posts)
	if m.query != "" {
		b = append(b, titleStyle.Render(fmt.Sprintf("reddit-archiver — %d/%d post(s)", visible, total))...)
	} else {
		b = append(b, titleStyle.Render(fmt.Sprintf("reddit-archiver — %d post(s)", total))...)
	}
	b = append(b, '\n', '\n')

	// --- post list --------------------------------------------------------
	if visible == 0 {
		if m.query != "" {
			b = append(b, fmt.Sprintf("no posts match %q\n", m.query)...)
		} else {
			b = append(b, "no posts yet — run the scraper first\n"...)
		}
	}

	for i, p := range m.filtered {
		line := fmt.Sprintf("[%s] r/%-20s %s", p.Source, p.Subreddit, p.Title)
		if i == m.cursor {
			line = selectedStyle.Render(line)
		}
		b = append(b, line...)
		b = append(b, '\n')
	}

	// --- help -------------------------------------------------------------
	b = append(b, '\n')
	if m.filtering {
		b = append(b, helpStyle.Render("type to filter  ·  enter accept  ·  esc cancel")...)
	} else {
		b = append(b, helpStyle.Render("↑/↓ move  ·  / filter  ·  esc clear  ·  q quit")...)
	}

	return string(b)
}
