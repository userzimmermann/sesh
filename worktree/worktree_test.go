package worktree

import (
	"os"
	"testing"

	"github.com/joshmedeski/sesh/v2/browser"
	"github.com/joshmedeski/sesh/v2/connector"
	"github.com/joshmedeski/sesh/v2/forge"
	"github.com/joshmedeski/sesh/v2/git"
	"github.com/joshmedeski/sesh/v2/home"
	"github.com/joshmedeski/sesh/v2/model"
	"github.com/joshmedeski/sesh/v2/oswrap"
	"github.com/joshmedeski/sesh/v2/pathwrap"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func falsePtr() *bool { b := false; return &b }

func forgeResolver(f forge.Forge) forge.Resolver {
	return forge.NewResolver(
		func(cfg model.WorktreeConfig) (forge.Forge, error) {
			return f, nil
		},
		func(path string) (forge.Forge, error) {
			return f, nil
		},
	)
}

func nuConfig() model.Config {
	return model.Config{
		WorktreeConfigs: []model.WorktreeConfig{{
			Repo:           "nutiliti/nutiliti",
			Path:           "/repo",
			WorktreeDir:    "w",
			BranchTemplate: "jam/{number}-1",
			BaseBranch:     "origin/main",
			Fetch:          falsePtr(),
			// Deliberately different values: every Connect test asserts which of
			// the two the connector was handed, which a shared value would hide.
			StartupCommand: "nu_setup",
			CreateCommand:  "nu_install",
		}},
	}
}

func TestConnectIssueWithRepoOverride(t *testing.T) {
	mGit := git.NewMockGit(t)
	mGh := forge.NewMockForge(t)
	mConn := connector.NewMockConnector(t)
	mBrowser := browser.NewMockBrowser(t)
	mOs := oswrap.NewMockOs(t)
	h := home.NewHome(mOs)
	p := pathwrap.NewPath()

	wRoot := p.FromSlash("/repo/w")
	wPath := p.FromSlash("/repo/w/2345")

	// path expansion: "/repo" has no ~, but home.ExpandPath calls os.UserHomeDir
	mOs.EXPECT().UserHomeDir().Return("/home/me", nil).Maybe()
	mOs.EXPECT().ExpandEnv("/repo").Return("/repo").Maybe()

	// Not a PR => issue path
	mGh.EXPECT().PrView("github.com", "nutiliti/nutiliti", 2345).Return(forge.PullRequest{}, false, nil)
	mGh.EXPECT().WorktreeName(2345, false).Return("2345")

	// target does not exist yet
	mOs.EXPECT().Stat(wPath).Return(nil, os.ErrNotExist)
	mOs.EXPECT().MkdirAll(wRoot, mock.Anything).Return(nil)

	mGit.EXPECT().WorktreeAdd("/repo", wPath, "jam/2345-1", "origin/main").Return("", nil)

	mConn.EXPECT().
		Connect(wPath, model.ConnectOpts{Switch: true, Command: "nu_install"}).
		Return("", nil)

	w := NewWorktree(nuConfig(), mGit, forgeResolver(mGh), mConn, mBrowser, h, mOs, p, testIssueCache(t))
	_, err := w.Connect(model.WorktreeConnectOpts{Number: 2345, Repo: "nutiliti/nutiliti", Switch: true})
	require.NoError(t, err)
}

func TestConnectOwnPrWithClosingIssue(t *testing.T) {
	mGit := git.NewMockGit(t)
	mGh := forge.NewMockForge(t)
	mConn := connector.NewMockConnector(t)
	mBrowser := browser.NewMockBrowser(t)
	mOs := oswrap.NewMockOs(t)
	h := home.NewHome(mOs)
	p := pathwrap.NewPath()

	wRoot := p.FromSlash("/repo/w")
	wPath := p.FromSlash("/repo/w/42")

	mOs.EXPECT().UserHomeDir().Return("/home/me", nil).Maybe()
	mOs.EXPECT().ExpandEnv("/repo").Return("/repo").Maybe()

	// It is a PR authored by me, closing issue 42
	mGh.EXPECT().PrView("github.com", "nutiliti/nutiliti", 100).
		Return(forge.PullRequest{Author: "me", ClosingIssues: []int{42}}, true, nil)
	mGh.EXPECT().CurrentUser("github.com").Return("me", nil)
	mGh.EXPECT().WorktreeName(42, false).Return("42")

	// Keyed on the issue (42), normal branch create
	mOs.EXPECT().Stat(wPath).Return(nil, os.ErrNotExist)
	mOs.EXPECT().MkdirAll(wRoot, mock.Anything).Return(nil)
	mGit.EXPECT().WorktreeAdd("/repo", wPath, "jam/42-1", "origin/main").Return("", nil)
	mConn.EXPECT().Connect(wPath, model.ConnectOpts{Switch: true, Command: "nu_install"}).Return("", nil)

	w := NewWorktree(nuConfig(), mGit, forgeResolver(mGh), mConn, mBrowser, h, mOs, p, testIssueCache(t))
	_, err := w.Connect(model.WorktreeConnectOpts{Number: 100, Repo: "nutiliti/nutiliti", Switch: true})
	require.NoError(t, err)
}

func TestConnectForeignPrDetachCheckout(t *testing.T) {
	mGit := git.NewMockGit(t)
	mGh := forge.NewMockForge(t)
	mConn := connector.NewMockConnector(t)
	mBrowser := browser.NewMockBrowser(t)
	mOs := oswrap.NewMockOs(t)
	h := home.NewHome(mOs)
	p := pathwrap.NewPath()

	wRoot := p.FromSlash("/repo/w")
	wPath := p.FromSlash("/repo/w/200")

	mOs.EXPECT().UserHomeDir().Return("/home/me", nil).Maybe()
	mOs.EXPECT().ExpandEnv("/repo").Return("/repo").Maybe()

	// PR authored by someone else
	mGh.EXPECT().PrView("github.com", "nutiliti/nutiliti", 200).
		Return(forge.PullRequest{Author: "octocat"}, true, nil)
	mGh.EXPECT().CurrentUser("github.com").Return("me", nil)
	mGh.EXPECT().WorktreeName(200, true).Return("200")

	// Keyed on PR number, detached worktree + gh pr checkout
	mOs.EXPECT().Stat(wPath).Return(nil, os.ErrNotExist)
	mOs.EXPECT().MkdirAll(wRoot, mock.Anything).Return(nil)
	mGit.EXPECT().WorktreeAddDetached("/repo", wPath, "origin/main").Return("", nil)
	mGh.EXPECT().PrCheckout(wPath, "github.com", "nutiliti/nutiliti", 200).Return("", nil)
	mConn.EXPECT().Connect(wPath, model.ConnectOpts{Switch: true, Command: "nu_install"}).Return("", nil)

	w := NewWorktree(nuConfig(), mGit, forgeResolver(mGh), mConn, mBrowser, h, mOs, p, testIssueCache(t))
	_, err := w.Connect(model.WorktreeConnectOpts{Number: 200, Repo: "nutiliti/nutiliti", Switch: true})
	require.NoError(t, err)
}

func TestConnectResolvesConfigFromCwd(t *testing.T) {
	mGit := git.NewMockGit(t)
	mGh := forge.NewMockForge(t)
	mConn := connector.NewMockConnector(t)
	mBrowser := browser.NewMockBrowser(t)
	mOs := oswrap.NewMockOs(t)
	h := home.NewHome(mOs)
	p := pathwrap.NewPath()

	wRoot := p.FromSlash("/repo/w")
	wPath := p.FromSlash("/repo/w/2345")

	mOs.EXPECT().UserHomeDir().Return("/home/me", nil).Maybe()
	mOs.EXPECT().ExpandEnv("/repo").Return("/repo").Maybe()

	// No --repo override: resolveConfig detects the repo from cwd via
	// `git worktree list --porcelain` on the main worktree.
	mOs.EXPECT().Getwd().Return(wPath, nil)
	mGit.EXPECT().WorktreeList(wPath).Return(true,
		"worktree /repo\nHEAD abc\nbranch refs/heads/main\n\n"+
			"worktree /repo/w/2345\nHEAD def\nbranch refs/heads/jam/2345-1\n",
		nil)

	// Not a PR => issue path
	mGh.EXPECT().PrView("github.com", "nutiliti/nutiliti", 2345).Return(forge.PullRequest{}, false, nil)
	mGh.EXPECT().WorktreeName(2345, false).Return("2345")

	mOs.EXPECT().Stat(wPath).Return(nil, os.ErrNotExist)
	mOs.EXPECT().MkdirAll(wRoot, mock.Anything).Return(nil)
	mGit.EXPECT().WorktreeAdd("/repo", wPath, "jam/2345-1", "origin/main").Return("", nil)
	mConn.EXPECT().Connect(wPath, model.ConnectOpts{Switch: true, Command: "nu_install"}).Return("", nil)

	w := NewWorktree(nuConfig(), mGit, forgeResolver(mGh), mConn, mBrowser, h, mOs, p, testIssueCache(t))
	_, err := w.Connect(model.WorktreeConnectOpts{Number: 2345, Switch: true})
	require.NoError(t, err)
}

func TestConnectOwnPrFirstIssueRefFallback(t *testing.T) {
	mGit := git.NewMockGit(t)
	mGh := forge.NewMockForge(t)
	mConn := connector.NewMockConnector(t)
	mBrowser := browser.NewMockBrowser(t)
	mOs := oswrap.NewMockOs(t)
	h := home.NewHome(mOs)
	p := pathwrap.NewPath()

	wRoot := p.FromSlash("/repo/w")
	wPath := p.FromSlash("/repo/w/7")

	mOs.EXPECT().UserHomeDir().Return("/home/me", nil).Maybe()
	mOs.EXPECT().ExpandEnv("/repo").Return("/repo").Maybe()

	// PR authored by me, no ClosingIssues, but body references "#7"
	mGh.EXPECT().PrView("github.com", "nutiliti/nutiliti", 100).
		Return(forge.PullRequest{Author: "me", Body: "fixes #7"}, true, nil)
	mGh.EXPECT().CurrentUser("github.com").Return("me", nil)
	mGh.EXPECT().WorktreeName(7, false).Return("7")

	// Keyed on issue 7 scanned from title+body, normal branch create (not detached/PrCheckout)
	mOs.EXPECT().Stat(wPath).Return(nil, os.ErrNotExist)
	mOs.EXPECT().MkdirAll(wRoot, mock.Anything).Return(nil)
	mGit.EXPECT().WorktreeAdd("/repo", wPath, "jam/7-1", "origin/main").Return("", nil)
	mConn.EXPECT().Connect(wPath, model.ConnectOpts{Switch: true, Command: "nu_install"}).Return("", nil)

	w := NewWorktree(nuConfig(), mGit, forgeResolver(mGh), mConn, mBrowser, h, mOs, p, testIssueCache(t))
	_, err := w.Connect(model.WorktreeConnectOpts{Number: 100, Repo: "nutiliti/nutiliti", Switch: true})
	require.NoError(t, err)
}

func TestConnectExistingWorktreeRunsStartupCommand(t *testing.T) {
	mGit := git.NewMockGit(t)
	mGh := forge.NewMockForge(t)
	mConn := connector.NewMockConnector(t)
	mBrowser := browser.NewMockBrowser(t)
	mOs := oswrap.NewMockOs(t)
	h := home.NewHome(mOs)
	p := pathwrap.NewPath()

	wPath := p.FromSlash("/repo/w/2345")

	mOs.EXPECT().UserHomeDir().Return("/home/me", nil).Maybe()
	mOs.EXPECT().ExpandEnv("/repo").Return("/repo").Maybe()
	mGh.EXPECT().PrView("github.com", "nutiliti/nutiliti", 2345).Return(forge.PullRequest{}, false, nil)
	mGh.EXPECT().WorktreeName(2345, false).Return("2345")

	// The worktree is already there, so nothing is created — and the command is
	// the startup one, not the setup that already ran when it was created.
	mOs.EXPECT().Stat(wPath).Return(nil, nil)
	mConn.EXPECT().
		Connect(wPath, model.ConnectOpts{Switch: true, Command: "nu_setup"}).
		Return("", nil)

	w := NewWorktree(nuConfig(), mGit, forgeResolver(mGh), mConn, mBrowser, h, mOs, p, testIssueCache(t))
	_, err := w.Connect(model.WorktreeConnectOpts{Number: 2345, Repo: "nutiliti/nutiliti", Switch: true})
	require.NoError(t, err)
}

func TestConnectExistingPrWorktreeRunsStartupCommand(t *testing.T) {
	mGit := git.NewMockGit(t)
	mGh := forge.NewMockForge(t)
	mConn := connector.NewMockConnector(t)
	mBrowser := browser.NewMockBrowser(t)
	mOs := oswrap.NewMockOs(t)
	h := home.NewHome(mOs)
	p := pathwrap.NewPath()

	wPath := p.FromSlash("/repo/w/200")

	mOs.EXPECT().UserHomeDir().Return("/home/me", nil).Maybe()
	mOs.EXPECT().ExpandEnv("/repo").Return("/repo").Maybe()

	// Foreign PR whose worktree already exists: the checkout is refreshed, which
	// is not the same thing as creating it.
	mGh.EXPECT().PrView("github.com", "nutiliti/nutiliti", 200).
		Return(forge.PullRequest{Author: "someone-else"}, true, nil)
	mGh.EXPECT().CurrentUser("github.com").Return("me", nil)
	mGh.EXPECT().WorktreeName(200, true).Return("200")
	mOs.EXPECT().Stat(wPath).Return(nil, nil)
	mGh.EXPECT().PrCheckout(wPath, "github.com", "nutiliti/nutiliti", 200).Return("", nil)
	mGit.EXPECT().Pull(wPath).Return("", nil)
	mConn.EXPECT().
		Connect(wPath, model.ConnectOpts{Switch: true, Command: "nu_setup"}).
		Return("", nil)

	w := NewWorktree(nuConfig(), mGit, forgeResolver(mGh), mConn, mBrowser, h, mOs, p, testIssueCache(t))
	_, err := w.Connect(model.WorktreeConnectOpts{Number: 200, Repo: "nutiliti/nutiliti", Switch: true})
	require.NoError(t, err)
}

func TestConnectCreateWithoutCreateCommandRunsNothing(t *testing.T) {
	mGit := git.NewMockGit(t)
	mGh := forge.NewMockForge(t)
	mConn := connector.NewMockConnector(t)
	mBrowser := browser.NewMockBrowser(t)
	mOs := oswrap.NewMockOs(t)
	h := home.NewHome(mOs)
	p := pathwrap.NewPath()

	wRoot := p.FromSlash("/repo/w")
	wPath := p.FromSlash("/repo/w/2345")

	cfg := nuConfig()
	cfg.WorktreeConfigs[0].CreateCommand = ""

	mOs.EXPECT().UserHomeDir().Return("/home/me", nil).Maybe()
	mOs.EXPECT().ExpandEnv("/repo").Return("/repo").Maybe()
	mGh.EXPECT().PrView("github.com", "nutiliti/nutiliti", 2345).Return(forge.PullRequest{}, false, nil)
	mGh.EXPECT().WorktreeName(2345, false).Return("2345")
	mOs.EXPECT().Stat(wPath).Return(nil, os.ErrNotExist)
	mOs.EXPECT().MkdirAll(wRoot, mock.Anything).Return(nil)
	mGit.EXPECT().WorktreeAdd("/repo", wPath, "jam/2345-1", "origin/main").Return("", nil)

	// startup_command does not stand in for an unset create_command: creating is
	// the one time it is deliberately not run.
	mConn.EXPECT().
		Connect(wPath, model.ConnectOpts{Switch: true, Command: ""}).
		Return("", nil)

	w := NewWorktree(cfg, mGit, forgeResolver(mGh), mConn, mBrowser, h, mOs, p, testIssueCache(t))
	_, err := w.Connect(model.WorktreeConnectOpts{Number: 2345, Repo: "nutiliti/nutiliti", Switch: true})
	require.NoError(t, err)
}

func TestConnectNoConfigMatch(t *testing.T) {
	mGit := git.NewMockGit(t)
	mGh := forge.NewMockForge(t)
	mConn := connector.NewMockConnector(t)
	mBrowser := browser.NewMockBrowser(t)
	mOs := oswrap.NewMockOs(t)
	h := home.NewHome(mOs)
	p := pathwrap.NewPath()

	w := NewWorktree(nuConfig(), mGit, forgeResolver(mGh), mConn, mBrowser, h, mOs, p, testIssueCache(t))
	_, err := w.Connect(model.WorktreeConnectOpts{Number: 1, Repo: "unknown/repo"})
	require.Error(t, err)
}

func TestConnectWithCommandOpensNewWindow(t *testing.T) {
	for _, background := range []bool{false, true} {
		mGit := git.NewMockGit(t)
		mGh := forge.NewMockForge(t)
		mConn := connector.NewMockConnector(t)
		mBrowser := browser.NewMockBrowser(t)
		mOs := oswrap.NewMockOs(t)
		h := home.NewHome(mOs)
		p := pathwrap.NewPath()

		wPath := p.FromSlash("/repo/w/2345")

		mOs.EXPECT().UserHomeDir().Return("/home/me", nil).Maybe()
		mOs.EXPECT().ExpandEnv("/repo").Return("/repo").Maybe()
		mGh.EXPECT().PrView("github.com", "nutiliti/nutiliti", 2345).Return(forge.PullRequest{}, false, nil)
		mGh.EXPECT().WorktreeName(2345, false).Return("2345")
		mOs.EXPECT().Stat(wPath).Return(nil, nil)

		mConn.EXPECT().
			Connect(wPath, model.ConnectOpts{Command: "nu_setup", Background: true}).
			Return("nutiliti/2345", nil)
		mConn.EXPECT().
			ConnectWindow(model.WindowConnectOpts{
				Name:       "claude",
				New:        true,
				Session:    "nutiliti/2345",
				Command:    `/usr/bin/claude "Implement #2345"`,
				Background: background,
			}).
			Return("nutiliti/2345:3", nil)

		w := NewWorktree(nuConfig(), mGit, forgeResolver(mGh), mConn, mBrowser, h, mOs, p, testIssueCache(t))
		_, err := w.Connect(model.WorktreeConnectOpts{
			Number:     2345,
			Repo:       "nutiliti/nutiliti",
			Command:    `/usr/bin/claude "Implement #2345"`,
			Background: background,
		})
		require.NoError(t, err)
	}
}

func TestConnectBackgroundWithoutCommand(t *testing.T) {
	mGit := git.NewMockGit(t)
	mGh := forge.NewMockForge(t)
	mConn := connector.NewMockConnector(t)
	mBrowser := browser.NewMockBrowser(t)
	mOs := oswrap.NewMockOs(t)
	h := home.NewHome(mOs)
	p := pathwrap.NewPath()

	wRoot := p.FromSlash("/repo/w")
	wPath := p.FromSlash("/repo/w/2345")

	mOs.EXPECT().UserHomeDir().Return("/home/me", nil).Maybe()
	mOs.EXPECT().ExpandEnv("/repo").Return("/repo").Maybe()
	mGh.EXPECT().PrView("github.com", "nutiliti/nutiliti", 2345).Return(forge.PullRequest{}, false, nil)
	mGh.EXPECT().WorktreeName(2345, false).Return("2345")
	mOs.EXPECT().Stat(wPath).Return(nil, os.ErrNotExist)
	mOs.EXPECT().MkdirAll(wRoot, mock.Anything).Return(nil)
	mGit.EXPECT().WorktreeAdd("/repo", wPath, "jam/2345-1", "origin/main").Return("", nil)
	mConn.EXPECT().
		Connect(wPath, model.ConnectOpts{Command: "nu_install", Background: true}).
		Return("nutiliti/2345", nil)

	w := NewWorktree(nuConfig(), mGit, forgeResolver(mGh), mConn, mBrowser, h, mOs, p, testIssueCache(t))
	_, err := w.Connect(model.WorktreeConnectOpts{Number: 2345, Repo: "nutiliti/nutiliti", Background: true})
	require.NoError(t, err)
}

func TestConnectGitHubCustomHost(t *testing.T) {
	mGit := git.NewMockGit(t)
	mGh := forge.NewMockForge(t)
	mConn := connector.NewMockConnector(t)
	mBrowser := browser.NewMockBrowser(t)
	mOs := oswrap.NewMockOs(t)
	h := home.NewHome(mOs)
	p := pathwrap.NewPath()

	cfg := model.Config{
		WorktreeConfigs: []model.WorktreeConfig{{
			Repo:           "enterprise/repo",
			Host:           "github.example.com",
			Path:           "/enterprise-repo",
			WorktreeDir:    ".wk",
			BranchTemplate: "feature/{number}",
			BaseBranch:     "origin/main",
			Fetch:          falsePtr(),
			CreateCommand:  "init_script",
		}},
	}

	wRoot := p.FromSlash("/enterprise-repo/.wk")
	wPath := p.FromSlash("/enterprise-repo/.wk/55")

	mOs.EXPECT().UserHomeDir().Return("/home/me", nil).Maybe()
	mOs.EXPECT().ExpandEnv("/enterprise-repo").Return("/enterprise-repo").Maybe()

	mGh.EXPECT().PrView("github.example.com", "enterprise/repo", 55).Return(forge.PullRequest{}, false, nil)
	mGh.EXPECT().WorktreeName(55, false).Return("55")

	mOs.EXPECT().Stat(wPath).Return(nil, os.ErrNotExist)
	mOs.EXPECT().MkdirAll(wRoot, mock.Anything).Return(nil)
	mGit.EXPECT().WorktreeAdd("/enterprise-repo", wPath, "feature/55", "origin/main").Return("", nil)
	mConn.EXPECT().Connect(wPath, model.ConnectOpts{Switch: true, Command: "init_script"}).Return("", nil)

	w := NewWorktree(cfg, mGit, forgeResolver(mGh), mConn, mBrowser, h, mOs, p, testIssueCache(t))
	_, err := w.Connect(model.WorktreeConnectOpts{Number: 55, Repo: "enterprise/repo", Switch: true})
	require.NoError(t, err)
}

func TestConnectGitLabCustomHost(t *testing.T) {
	mGit := git.NewMockGit(t)
	mGl := forge.NewMockForge(t)
	mConn := connector.NewMockConnector(t)
	mBrowser := browser.NewMockBrowser(t)
	mOs := oswrap.NewMockOs(t)
	h := home.NewHome(mOs)
	p := pathwrap.NewPath()

	cfg := model.Config{
		WorktreeConfigs: []model.WorktreeConfig{{
			Repo:           "internal/repo",
			Forge:          "gitlab",
			Host:           "gitlab.example.com",
			Path:           "/internal-repo",
			WorktreeDir:    ".wk",
			BranchTemplate: "feature/{number}",
			BaseBranch:     "origin/main",
			Fetch:          falsePtr(),
			CreateCommand:  "make setup",
		}},
	}

	wRoot := p.FromSlash("/internal-repo/.wk")
	wPath := p.FromSlash("/internal-repo/.wk/99")

	mOs.EXPECT().UserHomeDir().Return("/home/me", nil).Maybe()
	mOs.EXPECT().ExpandEnv("/internal-repo").Return("/internal-repo").Maybe()

	mGl.EXPECT().PrView("gitlab.example.com", "internal/repo", 99).Return(forge.PullRequest{}, false, nil)
	mGl.EXPECT().WorktreeName(99, false).Return("99")

	mOs.EXPECT().Stat(wPath).Return(nil, os.ErrNotExist)
	mOs.EXPECT().MkdirAll(wRoot, mock.Anything).Return(nil)
	mGit.EXPECT().WorktreeAdd("/internal-repo", wPath, "feature/99", "origin/main").Return("", nil)
	mConn.EXPECT().Connect(wPath, model.ConnectOpts{Switch: true, Command: "make setup"}).Return("", nil)

	w := NewWorktree(cfg, mGit, forgeResolver(mGl), mConn, mBrowser, h, mOs, p, testIssueCache(t))
	_, err := w.Connect(model.WorktreeConnectOpts{Number: 99, Repo: "internal/repo", Switch: true})
	require.NoError(t, err)
}

func TestConnectGitLabMergeRequest(t *testing.T) {
	mGit := git.NewMockGit(t)
	mGl := forge.NewMockForge(t)
	mConn := connector.NewMockConnector(t)
	mBrowser := browser.NewMockBrowser(t)
	mOs := oswrap.NewMockOs(t)
	h := home.NewHome(mOs)
	p := pathwrap.NewPath()

	cfg := model.Config{
		WorktreeConfigs: []model.WorktreeConfig{{
			Repo:          "inkscape/inkscape",
			Forge:         "gitlab",
			Path:          "/gitlab-repo",
			WorktreeDir:   ".wk",
			BaseBranch:    "origin/master",
			Fetch:         falsePtr(),
			CreateCommand: "gl_setup",
		}},
	}

	wRoot := p.FromSlash("/gitlab-repo/.wk")
	wPath := p.FromSlash("/gitlab-repo/.wk/!101")

	mOs.EXPECT().UserHomeDir().Return("/home/me", nil).Maybe()
	mOs.EXPECT().ExpandEnv("/gitlab-repo").Return("/gitlab-repo").Maybe()

	mGl.EXPECT().PrView("gitlab.com", "inkscape/inkscape", 101).
		Return(forge.PullRequest{Author: "someone-else"}, true, nil)
	mGl.EXPECT().CurrentUser("gitlab.com").Return("me", nil)
	mGl.EXPECT().WorktreeName(101, true).Return("!101")

	mOs.EXPECT().Stat(wPath).Return(nil, os.ErrNotExist)
	mOs.EXPECT().MkdirAll(wRoot, mock.Anything).Return(nil)
	mGit.EXPECT().WorktreeAddDetached("/gitlab-repo", wPath, "origin/master").Return("", nil)
	mGl.EXPECT().PrCheckout(wPath, "gitlab.com", "inkscape/inkscape", 101).Return("", nil)
	mConn.EXPECT().Connect(wPath, model.ConnectOpts{Switch: true, Command: "gl_setup"}).Return("", nil)

	w := NewWorktree(cfg, mGit, forgeResolver(mGl), mConn, mBrowser, h, mOs, p, testIssueCache(t))
	_, err := w.Connect(model.WorktreeConnectOpts{Number: 101, Repo: "inkscape/inkscape", Switch: true})
	require.NoError(t, err)
}
