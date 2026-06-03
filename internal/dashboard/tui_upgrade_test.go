package dashboard

import (
	"strings"
	"testing"
)

func TestUpgradeRender_InProgressShowsMessage(t *testing.T) {
	m := newUpgradeModel(t)

	// Transition to InProgress with a status message.
	updated, _ := m.Update(upgradeProgressMsg{Progress: 50, Message: "Downloading..."})
	model := updated.(Model)
	model.upgrade.state = UpgradeStateInProgress

	out := model.renderUpdateNotification()
	if !strings.Contains(out, "Downloading...") {
		t.Errorf("InProgress render missing upgradeMessage; got:\n%s", out)
	}
}

func TestUpgradeRender_InProgress_NoMessageNoExtraLine(t *testing.T) {
	m := newUpgradeModel(t)

	updated, _ := m.Update(upgradeProgressMsg{Progress: 30, Message: ""})
	model := updated.(Model)
	model.upgrade.state = UpgradeStateInProgress

	out := model.renderUpdateNotification()
	if strings.Contains(out, "\n  \n") {
		t.Errorf("InProgress render should not add blank line when upgradeMessage is empty; got:\n%s", out)
	}
}

func TestUpgradeRender_CompleteNoRestarting(t *testing.T) {
	m := newUpgradeModel(t)

	updated, _ := m.Update(upgradeCompleteMsg{Success: true})
	model := updated.(Model)

	out := model.renderUpdateNotification()
	if strings.Contains(out, "Restarting") {
		t.Errorf("Complete render must not claim a restart happened; got:\n%s", out)
	}
	if !strings.Contains(out, "restart Pilot manually") {
		t.Errorf("Complete render should instruct manual restart; got:\n%s", out)
	}
	if !strings.Contains(out, "v2.101.0") {
		t.Errorf("Complete render should include the new version; got:\n%s", out)
	}
}

func TestUpgradeRender_FailedShowsError(t *testing.T) {
	m := newUpgradeModel(t)

	errMsg := "Gatekeeper rejected the binary"
	updated, _ := m.Update(upgradeCompleteMsg{Success: false, Error: errMsg})
	model := updated.(Model)

	out := model.renderUpdateNotification()
	if !strings.Contains(out, errMsg) {
		t.Errorf("Failed render should show the Gatekeeper-aware error; got:\n%s", out)
	}
	if strings.Contains(out, "Restarting") {
		t.Errorf("Failed render must not contain 'Restarting'; got:\n%s", out)
	}
}
