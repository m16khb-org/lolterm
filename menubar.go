package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// barRetry is the wait before a failed league lookup of the menu bar runs again.
const barRetry = 30 * time.Second

// cmdBar prints the live game as a SwiftBar streamable plugin for the macOS menu bar: a
// title with the kills, the gold lead and the game clock, and a dropdown with the series,
// the objectives and the players. Every -i it prints "~~~" (SwiftBar's block separator)
// and the next block. The feed runs as in relay, so the clock also leaves out the pauses
// from before the plugin started.
func cmdBar(args []string) {
	fs := flag.NewFlagSet("lol bar", flag.ExitOnError)
	league := fs.String("l", "", "league slug")
	interval := fs.Duration("i", 5*time.Second, "feed poll and menu bar refresh interval")
	once := fs.Bool("1", false, "print one block and exit")
	fs.Parse(args)
	if *interval <= 0 {
		fmt.Fprintln(os.Stderr, "error: -i must be > 0")
		os.Exit(1)
	}
	exe, err := os.Executable()
	if err != nil {
		exe = "lolterm"
	}

	first := true
	emit := func(block string) {
		if !first {
			block = "~~~\n" + block
		}
		first = false
		fmt.Print(block) // one write: SwiftBar never shows half a block
	}

	// SwiftBar starts the plugin at login, often before the network is up: a failed lookup
	// is shown in the menu bar and runs again instead of ending the stream.
	lid, err := leagueID(*league)
	for err != nil {
		if *once {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		emit("LoL ⚠\n---\n" + tr("error_prefix") + err.Error() + " | color=gray\n")
		time.Sleep(barRetry)
		lid, err = leagueID(*league)
	}

	fd := newFeed(lid, *interval)
	fd.FetchOnce()
	pl := &relayPlayout{fd: fd}
	block := func() string { return barBlock(pl.Tick(time.Now()), exe) }
	if *once {
		fmt.Print(block())
		return
	}
	go fd.Run(context.Background())
	emit(block())
	tick := time.NewTicker(*interval)
	defer tick.Stop()
	for range tick.C {
		emit(block())
	}
}

// barBlock is one SwiftBar block for snapshot s: the menu bar title, "---", then the
// dropdown. exe is the binary the dropdown's actions run in a terminal.
func barBlock(s feedSnap, exe string) string {
	var b strings.Builder
	item := func(text, params string) {
		b.WriteString(text)
		if params != "" {
			b.WriteString(" | " + params)
		}
		b.WriteByte('\n')
	}
	const mono = "font=Menlo size=12"
	app := barAction(exe)

	l, r, ok := boardTeams(s.Ev, s.W)
	if s.Ev.ID == "" || !ok {
		item("LoL", "")
		item("---", "")
		item(tr("bar_no_live"), "color=gray")
		item("---", "")
		item(tr("bar_open_app"), app)
		return b.String()
	}
	lc, rc := codeOr(l.Code, l.Name), codeOr(r.Code, r.Name)
	lw, rw := "0", "0"
	if l.Result != nil {
		lw = itoa(l.Result.GameWins)
	}
	if r.Result != nil {
		rw = itoa(r.Result.GameWins)
	}
	var f *frame
	if n := len(s.W.Frames); n > 0 {
		f = &s.W.Frames[n-1]
	}

	title := lc + " " + lw + "–" + rw + " " + rc
	if f != nil {
		title = fmt.Sprintf("%s %d–%d %s", lc, f.BlueTeam.TotalKills, f.RedTeam.TotalKills, rc)
		if f.GameState == "finished" {
			title += " · " + tr("completed_state")
		} else {
			if lead := barLead(lc, rc, f.BlueTeam.TotalGold-f.RedTeam.TotalGold); lead != "" {
				title += " · " + lead
			}
			if s.Game >= 0 {
				title += fmt.Sprintf(" · %d:%02d", int(s.Game.Minutes()), int(s.Game.Seconds())%60)
			}
			if f.GameState == "paused" {
				title += " ⏸"
			}
		}
	}
	item(title, "")
	item("---", "")

	no, total := gameNumber(s.Ev)
	hdr := strings.ToUpper(s.Ev.League.Slug) + " · " + or(s.Ev.BlockName, "Match") + fmt.Sprintf(" · BO%d·G%d", max(1, total), max(1, no))
	if p := s.W.GameMetadata.PatchVersion; p != "" {
		hdr += " · patch " + shortPatch(p)
	}
	item(hdr, "color=gray")

	type row struct{ label, left, right, note string }
	rows := []row{{tr("bar_series"), lc + " " + lw, rw + " " + rc, ""}}
	if f != nil {
		bt, rt := f.BlueTeam, f.RedTeam
		// The drake types go in the note so the counts stay in their column.
		var drakes []string
		for _, t := range []struct {
			code string
			ds   []string
		}{{lc, bt.Dragons}, {rc, rt.Dragons}} {
			if len(t.ds) == 0 {
				continue
			}
			d := t.code
			for _, k := range t.ds {
				d += " " + or(objDrakeShort[k], k)
			}
			drakes = append(drakes, d+soulStr(t.ds))
		}
		rows = append(rows,
			row{tr("bar_kills"), itoa(bt.TotalKills), itoa(rt.TotalKills), ""},
			row{tr("bar_gold"), gold(bt.TotalGold), gold(rt.TotalGold), barLead(lc, rc, bt.TotalGold-rt.TotalGold)},
			row{tr("bar_towers"), itoa(bt.Towers), itoa(rt.Towers), ""},
			row{tr("bar_inhibs"), itoa(bt.Inhibitors), itoa(rt.Inhibitors), ""},
			row{tr("bar_dragons"), itoa(len(bt.Dragons)), itoa(len(rt.Dragons)), strings.Join(drakes, " · ")},
			row{tr("bar_barons"), itoa(bt.Barons), itoa(rt.Barons), ""},
		)
	}
	lab, val := 0, 0
	for _, x := range rows {
		lab, val = max(lab, runeLen(x.label)), max(val, runeLen(x.left))
	}
	for _, x := range rows {
		t := padR(x.label, lab) + "  " + padL(x.left, val) + " – " + x.right
		if x.note != "" {
			t += "   " + x.note
		}
		item(t, mono)
	}

	if f == nil {
		item(tr("bar_waiting"), "color=gray")
	} else {
		// Timers of kills from before the feed was joined stay unknown: no line then.
		if o := s.Obj; o.Dragon.State != objUnknown || o.Baron.State != objUnknown {
			var timers string
			for _, sg := range objTimerRow(o, 200) {
				timers += sg.text
			}
			item(timers, mono)
		}
		team := func(code, color string, meta teamMeta, ps []participant) {
			item("---", "")
			item(code, "color="+color)
			pm := metaMap(meta)
			for _, p := range ps {
				m := pm[p.ParticipantID]
				t := padR(m.name, 16) + " " + padR(m.champ, 12) + " " + padL(kda(p.Kills, p.Deaths, p.Assists), 8) +
					" " + padL(itoa(p.CreepScore), 4) + " " + padL(gold(p.TotalGold), 6)
				if lf := s.Lives[p.ParticipantID]; lf.Dead {
					t += "  " + deadSeg(lf).text
				}
				item(t, mono)
			}
		}
		team(lc, "#3b82f6", s.W.GameMetadata.BlueTeamMetadata, f.BlueTeam.Participants)
		team(rc, "#ef4444", s.W.GameMetadata.RedTeamMetadata, f.RedTeam.Participants)
	}

	item("---", "")
	item(tr("bar_open_board"), barAction(exe, "lol", "relay", "-l", s.Ev.League.Slug))
	item(tr("bar_open_app"), app)
	return b.String()
}

// barLead is the gold lead as "<team> +2.1k", or "" when the gold is even.
func barLead(blue, red string, diff int) string {
	switch {
	case diff > 0:
		return blue + " +" + gold(diff)
	case diff < 0:
		return red + " +" + gold(-diff)
	}
	return ""
}

// barAction is the SwiftBar parameters that run exe with args, in the current language,
// in a terminal window.
func barAction(exe string, args ...string) string {
	p := `bash="` + exe + `" param1=-lang param2=` + langCodes[currentLang]
	for i, a := range args {
		p += fmt.Sprintf(" param%d=%s", i+3, a)
	}
	return p + " terminal=true"
}
