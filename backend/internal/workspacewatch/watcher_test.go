package workspacewatch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestHandleEventIgnoresMetadataOnlyChmod(t *testing.T) {
	root := t.TempDir()
	if handleEvent(context.Background(), nil, root, gitWorkspace{}, fsnotify.Event{
		Name: filepath.Join(root, "README.md"),
		Op:   fsnotify.Chmod,
	}) {
		t.Fatal("metadata-only chmod was treated as a workspace content change")
	}
}

func TestWatchReportsExistingAndNewDirectoryChanges(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "src")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatalf("mkdir existing directory: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes, err := Watch(ctx, root)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	if err := os.WriteFile(filepath.Join(existing, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write existing-directory file: %v", err)
	}
	waitForChange(t, changes)
	drainChanges(changes)

	created := filepath.Join(root, "docs")
	if err := os.Mkdir(created, 0o755); err != nil {
		t.Fatalf("mkdir new directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(created, "guide.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write new-directory file: %v", err)
	}
	waitForChange(t, changes)
}

func TestWatchReportsChangesAcrossWorkspaceRoots(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes, err := Watch(ctx, first, second)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	if err := os.WriteFile(filepath.Join(second, "child.txt"), []byte("updated\n"), 0o644); err != nil {
		t.Fatalf("write second workspace file: %v", err)
	}
	waitForChange(t, changes)
}

func TestWatchClosesWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	changes, err := Watch(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	cancel()

	select {
	case _, ok := <-changes:
		if ok {
			t.Fatal("received a change after cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch channel did not close after cancellation")
	}
}

func TestWatchDoesNotTurnGitStatusIndexRefreshIntoAChange(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "ao@example.com")
	runGit(t, root, "config", "user.name", "AO Tests")
	tracked := filepath.Join(root, "README.md")
	if err := os.WriteFile(tracked, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "initial")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes, err := Watch(ctx, root)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	runGit(t, root, "status", "--porcelain")
	select {
	case <-changes:
		t.Fatal("read-only git status produced a workspace change")
	case <-time.After(250 * time.Millisecond):
	}

	if err := os.WriteFile(tracked, []byte("updated\n"), 0o644); err != nil {
		t.Fatalf("update tracked file: %v", err)
	}
	waitForChange(t, changes)
}

func waitForChange(t *testing.T, changes <-chan struct{}) {
	t.Helper()
	select {
	case _, ok := <-changes:
		if !ok {
			t.Fatal("watch channel closed before change")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for workspace change")
	}
}

func drainChanges(changes <-chan struct{}) {
	for {
		select {
		case _, ok := <-changes:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestAddInitialDirectoriesSkipsMissingGitListedDirectories(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "src")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatalf("mkdir existing directory: %v", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("new watcher: %v", err)
	}
	defer func() { _ = watcher.Close() }()

	rel, relErr := filepath.Rel(root, existing)
	if relErr != nil {
		t.Fatalf("rel: %v", relErr)
	}
	git := gitWorkspace{
		available: true,
		files:     []string{"missing-dir/file.txt", filepath.ToSlash(rel) + "/main.go"},
	}
	limiter := newDirWatchLimiter(watcher, maxWatchedDirectories)
	if err := addInitialDirectories(context.Background(), limiter, root, git); err != nil {
		t.Fatalf("addInitialDirectories with a stale git-listed directory: %v", err)
	}
}

func TestAddInitialDirectoriesBoundsWatchCount(t *testing.T) {
	root := t.TempDir()
	for i := range 8 {
		if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("dir-%02d", i), "nested", "deeper"), 0o755); err != nil {
			t.Fatalf("mkdir tree: %v", err)
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("new watcher: %v", err)
	}
	defer func() { _ = watcher.Close() }()

	previous := maxWatchedDirectories
	maxWatchedDirectories = 4
	defer func() { maxWatchedDirectories = previous }()

	limiter := newDirWatchLimiter(watcher, maxWatchedDirectories)
	if err := addInitialDirectories(context.Background(), limiter, root, gitWorkspace{}); err != nil {
		t.Fatalf("addInitialDirectories over the watch cap: %v", err)
	}

	watched := watcher.WatchList()
	if len(watched) != maxWatchedDirectories {
		t.Fatalf("watched directories = %d (%v), want exactly cap %d", len(watched), watched, maxWatchedDirectories)
	}
	watchedSet := make(map[string]struct{}, len(watched))
	for _, dir := range watched {
		watchedSet[dir] = struct{}{}
	}
	for _, want := range []string{
		root,
		filepath.Join(root, "dir-00"),
		filepath.Join(root, "dir-01"),
		filepath.Join(root, "dir-02"),
	} {
		if _, ok := watchedSet[want]; !ok {
			t.Fatalf("watched directories = %v, want shallow directory %q retained", watched, want)
		}
	}
	for _, unexpected := range []string{
		filepath.Join(root, "dir-00", "nested"),
		filepath.Join(root, "dir-00", "nested", "deeper"),
		filepath.Join(root, "dir-03"),
	} {
		if _, ok := watchedSet[unexpected]; ok {
			t.Fatalf("watched directories = %v, did not want capped directory %q retained", watched, unexpected)
		}
	}
}

func TestAddCreatedTreeSharesWatchBudgetWithInitialWalk(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "initial")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatalf("mkdir initial directory: %v", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("new watcher: %v", err)
	}
	defer func() { _ = watcher.Close() }()

	limiter := newDirWatchLimiter(watcher, 2)
	if err := addInitialDirectories(context.Background(), limiter, root, gitWorkspace{}); err != nil {
		t.Fatalf("addInitialDirectories: %v", err)
	}

	created := filepath.Join(root, "created")
	if err := os.Mkdir(created, 0o755); err != nil {
		t.Fatalf("mkdir created directory: %v", err)
	}
	if err := addCreatedTree(context.Background(), limiter, root, gitWorkspace{}, created); err != nil {
		t.Fatalf("addCreatedTree over the watch cap: %v", err)
	}

	watched := watcher.WatchList()
	if len(watched) != 2 {
		t.Fatalf("watched directories = %d (%v), want total watches bounded by cap 2", len(watched), watched)
	}
	watchedSet := make(map[string]struct{}, len(watched))
	for _, dir := range watched {
		watchedSet[dir] = struct{}{}
	}
	if _, ok := watchedSet[created]; ok {
		t.Fatalf("watched directories = %v, did not want over-cap created directory %q watched", watched, created)
	}
}

func TestChangeInUnwatchedDirectoryStillNotifies(t *testing.T) {
	root := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	previous := maxWatchedDirectories
	maxWatchedDirectories = 1
	defer func() { maxWatchedDirectories = previous }()

	changes, err := Watch(ctx, root)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// The only watch is the root, so the created directory itself stays
	// unwatched. Its creation must still surface through the watched root's
	// Create event so the read model refresh remains reachable even when
	// per-directory coverage degrades.
	unwatched := filepath.Join(root, "beyond-cap")
	if err := os.Mkdir(unwatched, 0o755); err != nil {
		t.Fatalf("mkdir beyond-cap directory: %v", err)
	}
	waitForChange(t, changes)
}