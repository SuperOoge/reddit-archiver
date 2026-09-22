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

// screen selects which top-level view model.View renders.
type screen int

const (
	screenPosts screen = iota
	screenHistory
)

type model struct {
	db       *gorm.DB
	posts    []models.Post // all loaded posts
	filtered []models.Post // currently visible (derived from posts + filter)
	cursor   int
	loadErr  error

	filtering bool   // true when the filter input is active
	query     string // current filter text

	screen        screen
	runs          []models.ScrapeRun // recent scrape history, newest first
	runsErr       error
	historyCursor int
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

type runsLoadedMsg struct {
	runs []models.ScrapeRun
	err  error
}

func loadRuns(gormDB *gorm.DB) tea.Cmd {
	return func() tea.Msg {
		var runs []models.ScrapeRun
		err := gormDB.Order("started_at DESC").Limit(50).Find(&runs).Error
		return runsLoadedMsg{runs: runs, err: err}
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
	return tea.Batch(loadPosts(m.db), loadRuns(m.db))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case postsLoadedMsg:
		m.posts = msg.posts
		m.filtered = applyFilter(m.posts, m.query)
		m.loadErr = msg.err
		return m, nil
	case runsLoadedMsg:
		m.runs = msg.runs
		m.runsErr = msg.err
		return m, nil
	case tea.KeyMsg:
		if m.screen == screenHistory {
			return m.updateHistoryKey(msg)
		}
		if m.filtering {
			return m.updateFilterKey(msg)
		}
		return m.updateNavigationKey(msg)
	}
	return m, nil
}

// updateFilterKey handles a key press while the filter input is active.
func (m model) updateFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Close the filter input, clear the query, show all posts.
		m.filtering = false
		m.query = ""
		m.filtered = applyFilter(m.posts, m.query)
		m.cursor = 0
	case "enter":
		// Accept the current filter and return to list navigation.
		m.filtering = false
		m.cursor = 0
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
	default:
		// Only accept printable characters (len 1 after RuneMsg).
		if len(msg.String()) == 1 {
			m.query += msg.String()
			m.filtered = applyFilter(m.posts, m.query)
			m.cursor = 0
		}
	}
	return m, nil
}

// updateNavigationKey handles a key press while browsing the post list.
func (m model) updateNavigationKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "/":
		// Open filter input.
		m.filtering = true
		m.query = ""
		m.cursor = 0
	case "h":
		// Switch to the scrape-history screen.
		m.screen = screenHistory
		m.historyCursor = 0
	case "esc":
		// If there's an active filter, clear it.
		if m.query != "" {
			m.query = ""
			m.filtered = applyFilter(m.posts, m.query)
			m.cursor = 0
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
	return m, nil
}

// updateHistoryKey handles a key press on the scrape-history screen.
func (m model) updateHistoryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "h", "esc":
		// Return to the post list.
		m.screen = screenPosts
	case "up", "k":
		if m.historyCursor > 0 {
			m.historyCursor--
		}
	case "down", "j":
		if m.historyCursor < len(m.runs)-1 {
			m.historyCursor++
		}
	case "pgup":
		m.historyCursor -= 10
		if m.historyCursor < 0 {
			m.historyCursor = 0
		}
	case "pgdown":
		m.historyCursor += 10
		if m.historyCursor >= len(m.runs) {
			m.historyCursor = len(m.runs) - 1
		}
		if m.historyCursor < 0 {
			m.historyCursor = 0
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.screen == screenHistory {
		return m.viewHistory()
	}
	return m.viewPosts()
}

func (m model) viewPosts() string {
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
		b = append(b, helpStyle.Render("↑/↓ move  ·  / filter  ·  h history  ·  esc clear  ·  q quit")...)
	}

	return string(b)
}

// viewHistory renders the scrape-history screen: recent ScrapeRun rows,
// newest first.
func (m model) viewHistory() string {
	if m.runsErr != nil {
		return fmt.Sprintf("failed to load scrape history: %v\n", m.runsErr)
	}

	var b []byte

	b = append(b, titleStyle.Render(fmt.Sprintf("reddit-archiver — scrape history (%d run(s))", len(m.runs)))...)
	b = append(b, '\n', '\n')

	if len(m.runs) == 0 {
		b = append(b, "no scrape runs yet\n"...)
	}

	for i, run := range m.runs {
		finished := "running"
		if run.FinishedAt != nil {
			finished = run.FinishedAt.Local().Format("2006-01-02 15:04")
		}
		line := fmt.Sprintf("[%-9s] %-20s %4d post(s)  started %s  finished %s",
			run.Status, run.Target, run.PostsFound, run.StartedAt.Local().Format("2006-01-02 15:04"), finished)
		if run.Status == models.ScrapeStatusFailed && run.Error != "" {
			line += "  error: " + run.Error
		}
		if i == m.historyCursor {
			line = selectedStyle.Render(line)
		}
		b = append(b, line...)
		b = append(b, '\n')
	}

	b = append(b, '\n')
	b = append(b, helpStyle.Render("↑/↓ move  ·  h/esc back  ·  q quit")...)

	return string(b)
}
