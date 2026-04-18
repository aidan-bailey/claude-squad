package app

import (
	"claude-squad/keys"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/git"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// lifecycleActions registers keys that mutate an instance's state
// machine: new, prompt, kill, submit (push), checkout (pause), resume.
// Preconditions gate the targets that must exist in a usable status;
// the error-returning capacity check for KeyNew/KeyPrompt stays in
// Run since it produces a user-visible error message.
func lifecycleActions() ActionRegistry {
	return ActionRegistry{
		keys.KeyPrompt:   {Run: runPromptNewInstance},
		keys.KeyNew:      {Run: runNewInstance},
		keys.KeyKill:     {Precondition: selectedNotBusyNotWorkspace, Run: runKillSelected},
		keys.KeySubmit:   {Precondition: selectedNotBusyNotWorkspace, Run: runSubmitSelected},
		keys.KeyCheckout: {Precondition: selectedNotBusyNotWorkspace, Run: runCheckoutSelected},
		keys.KeyResume:   {Precondition: selectedPausedNotWorkspace, Run: runResumeSelected},
	}
}

func runPromptNewInstance(m *home) (tea.Model, tea.Cmd) {
	if m.list.NumInstances() >= GlobalInstanceLimit {
		return m, m.handleError(
			fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
	}

	// Start a background fetch so branches are up to date by the time
	// the picker opens.
	repoDir := m.repoPath()
	fetchCmd := func() tea.Msg {
		git.FetchBranches(repoDir, nil)
		return nil
	}

	instance, err := session.NewInstance(session.InstanceOptions{
		Title:     "",
		Path:      repoDir,
		Program:   m.program,
		ConfigDir: m.configDir(),
	})
	if err != nil {
		return m, m.handleError(err)
	}

	m.newInstanceFinalizer = m.list.AddInstance(instance)
	m.list.SetSelectedInstance(m.list.NumInstances() - 1)
	m.state = stateNew
	m.menu.SetState(ui.StateNewInstance)
	m.promptAfterName = true

	return m, fetchCmd
}

func runNewInstance(m *home) (tea.Model, tea.Cmd) {
	if m.list.NumInstances() >= GlobalInstanceLimit {
		return m, m.handleError(
			fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
	}
	instance, err := session.NewInstance(session.InstanceOptions{
		Title:     "",
		Path:      m.repoPath(),
		Program:   m.program,
		ConfigDir: m.configDir(),
	})
	if err != nil {
		return m, m.handleError(err)
	}

	m.newInstanceFinalizer = m.list.AddInstance(instance)
	m.list.SetSelectedInstance(m.list.NumInstances() - 1)
	m.state = stateNew
	m.menu.SetState(ui.StateNewInstance)

	return m, nil
}

func runKillSelected(m *home) (tea.Model, tea.Cmd) {
	selected := m.list.GetSelectedInstance()
	preAction, killAction := killActionFor(m, selected)
	message := fmt.Sprintf("[!] Kill session '%s'?", selected.Title)
	return m, m.confirmTask(message, overlay.ConfirmationTask{
		Sync:  preAction,
		Async: killAction,
	})
}

// runKillSelectedNoConfirm mirrors runKillSelected but skips the
// confirmation overlay, running preAction inline before returning
// killAction. Used by cs.actions.kill_selected{confirm=false}.
func runKillSelectedNoConfirm(m *home) (tea.Model, tea.Cmd) {
	selected := m.list.GetSelectedInstance()
	preAction, killAction := killActionFor(m, selected)
	preAction()
	return m, killAction
}

// killActionFor returns the (synchronous pre-step, async body) pair
// that both runKillSelected variants share. preAction flips the
// instance to Deleting; killAction handles I/O off the update
// goroutine and returns the appropriate tea.Msg on completion.
func killActionFor(m *home, selected *session.Instance) (func(), tea.Cmd) {
	previousStatus := selected.GetStatus()
	title := selected.Title

	preAction := func() {
		if err := selected.TransitionTo(session.Deleting); err != nil {
			log.WarningLog.Printf("kill preAction transition: %v", err)
		}
	}

	killAction := func() tea.Msg {
		worktree, err := selected.GetGitWorktree()
		if err != nil {
			return transitionFailedMsg{title: title, op: "delete", previousStatus: previousStatus, err: err}
		}

		checkedOut, err := worktree.IsBranchCheckedOut()
		if err != nil {
			return transitionFailedMsg{title: title, op: "delete", previousStatus: previousStatus, err: err}
		}

		if checkedOut {
			return transitionFailedMsg{
				title:          title,
				op:             "delete",
				previousStatus: previousStatus,
				err:            fmt.Errorf("instance %s is currently checked out", selected.Title),
			}
		}

		if ts := m.splitPane.DetachTerminalForInstance(title); ts != nil {
			if err := ts.Close(); err != nil {
				log.ErrorLog.Printf("terminal pane: failed to close session for %s: %v", title, err)
			}
		}

		if err := selected.Kill(); err != nil {
			log.ErrorLog.Printf("could not kill instance: %v", err)
		}

		if err := m.storage.DeleteInstance(selected.Title); err != nil {
			return transitionFailedMsg{title: title, op: "delete", previousStatus: previousStatus, err: err}
		}

		return killInstanceMsg{title: title}
	}

	return preAction, killAction
}

func runSubmitSelected(m *home) (tea.Model, tea.Cmd) {
	selected := m.list.GetSelectedInstance()
	pushAction := pushActionFor(selected)
	message := fmt.Sprintf("[!] Push changes from session '%s'?", selected.Title)
	return m, m.confirmAction(message, pushAction)
}

// runSubmitSelectedNoConfirm mirrors runSubmitSelected but skips the
// confirmation overlay. Used by cs.actions.push_selected{confirm=false}.
func runSubmitSelectedNoConfirm(m *home) (tea.Model, tea.Cmd) {
	selected := m.list.GetSelectedInstance()
	return m, pushActionFor(selected)
}

func pushActionFor(selected *session.Instance) tea.Cmd {
	return func() tea.Msg {
		commitMsg := fmt.Sprintf("[claudesquad] update from '%s' on %s", selected.Title, time.Now().Format(time.RFC822))
		worktree, err := selected.GetGitWorktree()
		if err != nil {
			return err
		}
		if err = worktree.PushChanges(commitMsg, true); err != nil {
			return err
		}
		return nil
	}
}

func runCheckoutSelected(m *home) (tea.Model, tea.Cmd) {
	return runCheckoutSelectedOpts(m, true, true)
}

// runCheckoutSelectedOpts is the parameterized pause path. confirm
// gates the confirmation overlay; help gates the prerequisite help
// screen. Script callers use cs.actions.checkout_selected{confirm=,
// help=} to tune either; the legacy keymap always passes both true.
// Combinations that skip the confirm still trigger the Loading
// transition synchronously so the spinner renders immediately.
func runCheckoutSelectedOpts(m *home, confirm, help bool) (tea.Model, tea.Cmd) {
	selected := m.list.GetSelectedInstance()
	pauseAction := pauseActionFor(m, selected)

	startPause := func() tea.Cmd {
		if !confirm {
			if err := selected.TransitionTo(session.Loading); err != nil {
				log.WarningLog.Printf("pause preAction transition: %v", err)
			}
			return pauseAction
		}
		message := fmt.Sprintf("[!] Pause session '%s'?", selected.Title)
		return m.confirmTask(message, overlay.ConfirmationTask{
			Sync: func() {
				if err := selected.TransitionTo(session.Loading); err != nil {
					log.WarningLog.Printf("pause preAction transition: %v", err)
				}
			},
			Async: pauseAction,
		})
	}

	if help {
		return m.showHelpScreen(helpTypeInstanceCheckout{}, startPause)
	}
	return m, startPause()
}

func pauseActionFor(m *home, selected *session.Instance) tea.Cmd {
	previousStatus := selected.GetStatus()
	pauseTitle := selected.Title
	return func() tea.Msg {
		if ts := m.splitPane.DetachTerminalForInstance(pauseTitle); ts != nil {
			if err := ts.Close(); err != nil {
				log.ErrorLog.Printf("terminal pane: failed to close session for %s: %v", pauseTitle, err)
			}
		}
		saveFunc := func() error {
			return m.storage.SaveInstances(persistableInstances(m.list.GetInstances()))
		}
		if err := selected.Pause(saveFunc); err != nil {
			return transitionFailedMsg{title: pauseTitle, op: "pause", previousStatus: previousStatus, err: err}
		}
		return pauseInstanceMsg{title: pauseTitle}
	}
}

func runResumeSelected(m *home) (tea.Model, tea.Cmd) {
	selected := m.list.GetSelectedInstance()

	// Flip to Loading immediately so the list shows the spinner while
	// Resume's blocking worktree/tmux setup runs in a Cmd goroutine.
	// TransitionTo enforces Paused→Loading atomically, so a concurrent
	// reconcile flip between the precondition check and this write can't
	// leave us starting Resume on a non-Paused instance.
	if err := selected.TransitionTo(session.Loading); err != nil {
		log.WarningLog.Printf("skip resume: %v", err)
		return m, nil
	}
	saveFunc := func() error {
		return m.storage.SaveInstances(persistableInstances(m.list.GetInstances()))
	}
	resumeTitle := selected.Title
	resumeCmd := func() tea.Msg {
		if err := selected.Resume(saveFunc); err != nil {
			return transitionFailedMsg{title: resumeTitle, op: "resume", previousStatus: session.Paused, err: err}
		}
		return resumeDoneMsg{}
	}
	return m, tea.Batch(tea.WindowSize(), m.instanceChanged(), resumeCmd)
}
