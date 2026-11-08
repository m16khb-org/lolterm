package main

import (
	"strings"
	"testing"
	"time"
)

// The menu bar title is all most users read: the blue side's kills must sit next to the
// blue team's code (getLive keeps one team order for the whole series), and the gold
// lead must name the team that has it.
func TestBarTitlePutsTheBlueSideLeftAndNamesTheGoldLeader(t *testing.T) {
	ev := appEvent("m", "inProgress", "T1", "KC", 2, 1, time.Now())
	ev.ID = "m"
	ev.Match.Teams[0].ID, ev.Match.Teams[1].ID = "t1", "kc"
	var w windowResp
	w.GameMetadata.BlueTeamMetadata.EsportsTeamID = "kc"
	fr := frame{GameState: "in_game"}
	fr.BlueTeam.TotalKills, fr.RedTeam.TotalKills = 7, 3
	fr.BlueTeam.TotalGold, fr.RedTeam.TotalGold = 40000, 42100
	w.Frames = []frame{fr}

	for _, tc := range []struct {
		state string
		game  time.Duration
		want  string
	}{
		{"in_game", 20*time.Minute + 5*time.Second, "KC 7–3 T1 · T1 +2.1k · 20:05"},
		{"paused", 20 * time.Minute, "KC 7–3 T1 · T1 +2.1k · 20:00 ⏸"},
		{"finished", 31 * time.Minute, "KC 7–3 T1 · completed"},
	} {
		w.Frames[0].GameState = tc.state
		block := barBlock(feedSnap{Ev: ev, W: w, Game: tc.game}, "/bin/lolterm")
		if title, _, _ := strings.Cut(block, "\n"); title != tc.want {
			t.Errorf("%s: title %q, want %q", tc.state, title, tc.want)
		}
	}
}
