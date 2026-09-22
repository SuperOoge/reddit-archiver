package main

import (
	"errors"
	"strings"
	"testing"

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
