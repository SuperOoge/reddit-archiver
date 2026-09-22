package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuperOoge/reddit-archiver/internal/db"
	"github.com/SuperOoge/reddit-archiver/internal/models"
	tea "github.com/charmbracelet/bubbletea"
)

func TestApplyFilterEmptyQueryReturnsAll(t *testing.T) {
	posts := []models.Post{
		{Subreddit: "golang", Title: "first post"},
		{Subreddit: "rust", Title: "second post"},
	}

	got := applyFilter(posts, "")
	if len(got) != len(posts) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(posts))
	}
}

func TestApplyFilterMatchesSubredditOrTitle(t *testing.T) {
	posts := []models.Post{
		{Subreddit: "golang", Title: "learning generics"},
		{Subreddit: "rust", Title: "borrow checker woes"},
		{Subreddit: "programming", Title: "why I switched to Golang"},
	}

	got := applyFilter(posts, "GOLANG")
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
	}
	if got[0].Subreddit != "golang" || got[1].Title != "why I switched to Golang" {
		t.Errorf("unexpected matches: %+v", got)
	}
}

func TestApplyFilterNoMatches(t *testing.T) {
	posts := []models.Post{{Subreddit: "golang", Title: "first post"}}

	got := applyFilter(posts, "nonexistent")
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}

func testPosts() []models.Post {
	return []models.Post{
		{Subreddit: "golang", Title: "first post"},
		{Subreddit: "rust", Title: "second post"},
		{Subreddit: "golang", Title: "third post"},
	}
}

func TestUpdateFilterKeyEsc(t *testing.T) {
	m := model{filtering: true, query: "gol", posts: testPosts(), cursor: 2}
	m.filtered = applyFilter(m.posts, m.query)

	got, _ := m.updateFilterKey(tea.KeyMsg{Type: tea.KeyEsc})
	next := got.(model)

	if next.filtering {
		t.Error("filtering = true, want false")
	}
	if next.query != "" {
		t.Errorf("query = %q, want empty", next.query)
	}
	if len(next.filtered) != len(next.posts) {
		t.Errorf("filtered not reset to all posts: %+v", next.filtered)
	}
	if next.cursor != 0 {
		t.Errorf("cursor = %d, want 0", next.cursor)
	}
}

func TestUpdateFilterKeyEnter(t *testing.T) {
	m := model{filtering: true, query: "gol", posts: testPosts(), cursor: 2}
	m.filtered = applyFilter(m.posts, m.query)

	got, _ := m.updateFilterKey(tea.KeyMsg{Type: tea.KeyEnter})
	next := got.(model)

	if next.filtering {
		t.Error("filtering = true, want false")
	}
	if next.query != "gol" {
		t.Errorf("query = %q, want unchanged \"gol\"", next.query)
	}
	if next.cursor != 0 {
		t.Errorf("cursor = %d, want 0", next.cursor)
	}
}

func TestUpdateFilterKeyBackspace(t *testing.T) {
	m := model{filtering: true, query: "go", posts: testPosts()}
	m.filtered = applyFilter(m.posts, m.query)

	got, _ := m.updateFilterKey(tea.KeyMsg{Type: tea.KeyBackspace})
	next := got.(model)

	if next.query != "g" {
		t.Errorf("query = %q, want \"g\"", next.query)
	}
	if !next.filtering {
		t.Error("filtering = false, want true")
	}
}

func TestUpdateFilterKeyBackspaceOnEmptyQueryActsLikeEsc(t *testing.T) {
	m := model{filtering: true, query: "", posts: testPosts()}
	m.filtered = applyFilter(m.posts, m.query)

	got, _ := m.updateFilterKey(tea.KeyMsg{Type: tea.KeyBackspace})
	next := got.(model)

	if next.filtering {
		t.Error("filtering = true, want false")
	}
	if next.query != "" {
		t.Errorf("query = %q, want empty", next.query)
	}
}

func TestUpdateFilterKeyAppendsPrintableRune(t *testing.T) {
	m := model{filtering: true, query: "go", posts: testPosts()}
	m.filtered = applyFilter(m.posts, m.query)

	got, _ := m.updateFilterKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	next := got.(model)

	if next.query != "gol" {
		t.Errorf("query = %q, want \"gol\"", next.query)
	}
	if len(next.filtered) != 2 {
		t.Errorf("len(filtered) = %d, want 2", len(next.filtered))
	}
}

func TestUpdateFilterKeyIgnoresNonPrintable(t *testing.T) {
	m := model{filtering: true, query: "go", posts: testPosts()}
	m.filtered = applyFilter(m.posts, m.query)

	got, _ := m.updateFilterKey(tea.KeyMsg{Type: tea.KeyUp})
	next := got.(model)

	if next.query != "go" {
		t.Errorf("query = %q, want unchanged \"go\"", next.query)
	}
}

func TestUpdateNavigationKeyQuit(t *testing.T) {
	m := model{}

	_, cmd := m.updateNavigationKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("cmd = nil, want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("cmd() = %T, want tea.QuitMsg", cmd())
	}
}

func TestUpdateNavigationKeyOpensFilter(t *testing.T) {
	m := model{query: "stale", cursor: 3}

	got, _ := m.updateNavigationKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	next := got.(model)

	if !next.filtering {
		t.Error("filtering = false, want true")
	}
	if next.query != "" {
		t.Errorf("query = %q, want empty", next.query)
	}
	if next.cursor != 0 {
		t.Errorf("cursor = %d, want 0", next.cursor)
	}
}

func TestUpdateNavigationKeyEscClearsActiveFilter(t *testing.T) {
	m := model{query: "gol", posts: testPosts(), cursor: 1}
	m.filtered = applyFilter(m.posts, m.query)

	got, _ := m.updateNavigationKey(tea.KeyMsg{Type: tea.KeyEsc})
	next := got.(model)

	if next.query != "" {
		t.Errorf("query = %q, want empty", next.query)
	}
	if len(next.filtered) != len(next.posts) {
		t.Errorf("filtered not reset to all posts: %+v", next.filtered)
	}
}

func TestUpdateNavigationKeyCursorMovement(t *testing.T) {
	posts := testPosts()

	tests := []struct {
		name       string
		startCur   int
		key        tea.KeyMsg
		wantCursor int
	}{
		{"up decrements", 1, tea.KeyMsg{Type: tea.KeyUp}, 0},
		{"up floors at 0", 0, tea.KeyMsg{Type: tea.KeyUp}, 0},
		{"k decrements", 1, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}, 0},
		{"down increments", 0, tea.KeyMsg{Type: tea.KeyDown}, 1},
		{"down caps at len-1", 2, tea.KeyMsg{Type: tea.KeyDown}, 2},
		{"j increments", 0, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, 1},
		{"pgup floors at 0", 1, tea.KeyMsg{Type: tea.KeyPgUp}, 0},
		{"pgdown caps at len-1", 0, tea.KeyMsg{Type: tea.KeyPgDown}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{posts: posts, filtered: posts, cursor: tt.startCur}
			got, _ := m.updateNavigationKey(tt.key)
			next := got.(model)
			if next.cursor != tt.wantCursor {
				t.Errorf("cursor = %d, want %d", next.cursor, tt.wantCursor)
			}
		})
	}
}

func TestViewShowsLoadError(t *testing.T) {
	m := model{loadErr: errors.New("boom")}

	out := m.View()
	if !strings.Contains(out, "failed to load posts") || !strings.Contains(out, "boom") {
		t.Errorf("View() = %q, want it to mention the load error", out)
	}
}

func TestViewEmptyNoPosts(t *testing.T) {
	m := model{}

	out := m.View()
	if !strings.Contains(out, "no posts yet") {
		t.Errorf("View() = %q, want the empty-database message", out)
	}
}

func TestViewEmptyFilterNoMatches(t *testing.T) {
	m := model{posts: testPosts(), query: "zzz"}
	m.filtered = applyFilter(m.posts, m.query)

	out := m.View()
	if !strings.Contains(out, `no posts match "zzz"`) {
		t.Errorf("View() = %q, want a no-matches message", out)
	}
}

func TestViewListsPostsAndFilterBar(t *testing.T) {
	m := model{posts: testPosts(), query: "gol", cursor: 0}
	m.filtered = applyFilter(m.posts, m.query)

	out := m.View()
	if !strings.Contains(out, "filter: gol") {
		t.Error("View() missing filter bar")
	}
	if !strings.Contains(out, "r/golang") {
		t.Error("View() missing subreddit in listing")
	}
	if !strings.Contains(out, "matches") {
		t.Error("View() missing match count")
	}
}

func TestViewFilteringMode(t *testing.T) {
	m := model{filtering: true, query: "go"}

	out := m.View()
	if !strings.Contains(out, "filter: go") {
		t.Error("View() missing filter input line")
	}
	if !strings.Contains(out, "type to filter") {
		t.Error("View() missing filter-mode help text")
	}
}

func testRuns() []models.ScrapeRun {
	started := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	finished := started.Add(5 * time.Minute)
	return []models.ScrapeRun{
		{Target: "golang", Status: models.ScrapeStatusCompleted, PostsFound: 42, StartedAt: started, FinishedAt: &finished},
		{Target: "rust", Status: models.ScrapeStatusFailed, PostsFound: 0, StartedAt: started, FinishedAt: &finished, Error: "fetch page 0: unexpected status 429"},
		{Target: "python", Status: models.ScrapeStatusRunning, PostsFound: 5, StartedAt: started},
	}
}

func TestUpdateNavigationKeyOpensHistory(t *testing.T) {
	m := model{historyCursor: 3}

	got, _ := m.updateNavigationKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	next := got.(model)

	if next.screen != screenHistory {
		t.Errorf("screen = %v, want screenHistory", next.screen)
	}
	if next.historyCursor != 0 {
		t.Errorf("historyCursor = %d, want 0", next.historyCursor)
	}
}

func TestUpdateHistoryKeyGoesBackToPosts(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyRunes, Runes: []rune{'h'}}} {
		m := model{screen: screenHistory}
		got, _ := m.updateHistoryKey(key)
		next := got.(model)
		if next.screen != screenPosts {
			t.Errorf("key %q: screen = %v, want screenPosts", key.String(), next.screen)
		}
	}
}

func TestUpdateHistoryKeyQuit(t *testing.T) {
	m := model{screen: screenHistory}

	_, cmd := m.updateHistoryKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("cmd = nil, want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("cmd() = %T, want tea.QuitMsg", cmd())
	}
}

func TestUpdateHistoryKeyCursorMovement(t *testing.T) {
	runs := testRuns()

	tests := []struct {
		name       string
		startCur   int
		key        tea.KeyMsg
		wantCursor int
	}{
		{"up decrements", 1, tea.KeyMsg{Type: tea.KeyUp}, 0},
		{"up floors at 0", 0, tea.KeyMsg{Type: tea.KeyUp}, 0},
		{"down increments", 0, tea.KeyMsg{Type: tea.KeyDown}, 1},
		{"down caps at len-1", 2, tea.KeyMsg{Type: tea.KeyDown}, 2},
		{"pgup floors at 0", 1, tea.KeyMsg{Type: tea.KeyPgUp}, 0},
		{"pgdown caps at len-1", 0, tea.KeyMsg{Type: tea.KeyPgDown}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{screen: screenHistory, runs: runs, historyCursor: tt.startCur}
			got, _ := m.updateHistoryKey(tt.key)
			next := got.(model)
			if next.historyCursor != tt.wantCursor {
				t.Errorf("historyCursor = %d, want %d", next.historyCursor, tt.wantCursor)
			}
		})
	}
}

func TestUpdateRoutesKeysToHistoryScreen(t *testing.T) {
	m := model{screen: screenHistory, runs: testRuns()}

	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next := got.(model)
	if next.screen != screenPosts {
		t.Errorf("screen = %v, want screenPosts", next.screen)
	}
}

func TestUpdateRoutesKeysToFilterAndNavigation(t *testing.T) {
	filtering := model{filtering: true, query: "go"}
	got, _ := filtering.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if next := got.(model); next.query != "gol" {
		t.Errorf("filtering: query = %q, want \"gol\"", next.query)
	}

	navigating := model{}
	got, _ = navigating.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if next := got.(model); next.screen != screenHistory {
		t.Errorf("navigating: screen = %v, want screenHistory", next.screen)
	}
}

func TestUpdatePostsLoadedMsg(t *testing.T) {
	m := model{query: "gol"}

	got, _ := m.Update(postsLoadedMsg{posts: testPosts()})
	next := got.(model)

	if len(next.posts) != 3 {
		t.Errorf("len(posts) = %d, want 3", len(next.posts))
	}
	if len(next.filtered) != 2 {
		t.Errorf("len(filtered) = %d, want 2 (filter applied to loaded posts)", len(next.filtered))
	}
}

func TestUpdateRunsLoadedMsg(t *testing.T) {
	m := model{}

	got, _ := m.Update(runsLoadedMsg{runs: testRuns()})
	next := got.(model)

	if len(next.runs) != 3 {
		t.Errorf("len(runs) = %d, want 3", len(next.runs))
	}
}

func TestViewHistoryEmpty(t *testing.T) {
	m := model{screen: screenHistory}

	out := m.View()
	if !strings.Contains(out, "no scrape runs yet") {
		t.Errorf("View() = %q, want the empty-history message", out)
	}
}

func TestViewHistoryListsRuns(t *testing.T) {
	m := model{screen: screenHistory, runs: testRuns()}

	out := m.View()
	if !strings.Contains(out, "golang") || !strings.Contains(out, "rust") || !strings.Contains(out, "python") {
		t.Errorf("View() missing run targets: %q", out)
	}
	if !strings.Contains(out, "error: fetch page 0") {
		t.Errorf("View() missing failed run's error message: %q", out)
	}
	if !strings.Contains(out, "running") {
		t.Errorf("View() missing running status: %q", out)
	}
}

func TestViewHistoryLoadError(t *testing.T) {
	m := model{screen: screenHistory, runsErr: errors.New("boom")}

	out := m.View()
	if !strings.Contains(out, "failed to load scrape history") || !strings.Contains(out, "boom") {
		t.Errorf("View() = %q, want it to mention the load error", out)
	}
}

func TestInitLoadsPostsAndRuns(t *testing.T) {
	gormDB, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := gormDB.Create(&models.Post{Source: models.SourceReddit, ExternalID: "abc", URL: "https://example.com/x.png"}).Error; err != nil {
		t.Fatalf("seed post: %v", err)
	}
	if err := gormDB.Create(&models.ScrapeRun{Target: "golang", Status: models.ScrapeStatusCompleted, StartedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatalf("seed run: %v", err)
	}

	m := newModel(gormDB)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() cmd = nil")
	}

	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init() cmd() = %T, want tea.BatchMsg", cmd())
	}
	if len(batch) != 2 {
		t.Fatalf("len(batch) = %d, want 2", len(batch))
	}

	posts, ok := batch[0]().(postsLoadedMsg)
	if !ok {
		t.Fatalf("batch[0]() = %T, want postsLoadedMsg", batch[0]())
	}
	if len(posts.posts) != 1 {
		t.Errorf("len(posts.posts) = %d, want 1", len(posts.posts))
	}

	runs, ok := batch[1]().(runsLoadedMsg)
	if !ok {
		t.Fatalf("batch[1]() = %T, want runsLoadedMsg", batch[1]())
	}
	if len(runs.runs) != 1 {
		t.Errorf("len(runs.runs) = %d, want 1", len(runs.runs))
	}
}
