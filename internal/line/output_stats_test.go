package line

import (
	"strings"
	"testing"
	"time"
)

func TestStatsGroupings(t *testing.T) {
	d := outTestDB(t)
	d.chat("c1group")
	d.insert("_contact", map[string]any{"_mid": "u-a", "_displayName": "alpha"})
	d.insert("_profile", map[string]any{"_mid": "u-me", "_displayName": "me"})
	day := func(dd, hh int) int64 { return time.Date(2026, 3, dd, hh, 30, 0, 0, time.Local).UnixMilli() }
	n := 0
	add := func(from string, ms int64, ct int, extra map[string]any) {
		n++
		row := map[string]any{"_from": from}
		for k, v := range extra {
			row[k] = v
		}
		d.msg("s"+string(rune('a'+n)), "c1group", ms, ct, row)
	}
	add("u-a", day(1, 9), 0, nil)
	add("u-a", day(1, 9), 1, nil)
	add("u-a", day(2, 20), 7, nil)
	add("u-me", day(2, 9), 0, nil)
	add("u-b", day(3, 9), 0, nil)
	add("u-a", day(3, 10), 0, map[string]any{"_type": 3})                  // member event
	add("u-b", day(3, 11), 0, map[string]any{"_type": 3, "_attribute": 1}) // unsent
	add("u-a", day(3, 12), 18, nil)                                        // notice

	res, err := d.r.Stats(StatsOptions{ChatArg: "c1group"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 5 || res.GroupCount != 3 || res.By != "sender" || res.ChatType != "group" {
		t.Fatalf("sender: %+v", res)
	}
	g := res.Groups
	if g[0].SenderID != "u-a" || g[0].Sender != "alpha" || g[0].Count != 3 || g[0].FirstSentAt != tsToIso(day(1, 9)) || g[0].LastSentAt != tsToIso(day(2, 20)) {
		t.Fatalf("top sender %+v", g[0])
	}
	if me := g[1].SenderID + g[2].SenderID; !strings.Contains(me, "u-me") || !(g[1].FromMe || g[2].FromMe) {
		t.Fatalf("own messages not marked: %+v", g)
	}
	if res.FirstSentAt != tsToIso(day(1, 9)) || res.LastSentAt != tsToIso(day(3, 9)) {
		t.Fatalf("range %s..%s", res.FirstSentAt, res.LastSentAt)
	}

	res, _ = d.r.Stats(StatsOptions{ChatArg: "c1group", By: "type", IncludeEvents: true})
	got := strings.Join(outStatuses(res.Groups, func(g StatsGroup) string { return g.Type + "=" + itoa(uint32(g.Count)) }), " ")
	if got != "text=3 event=2 image=1 sticker=1 unsent=1" || res.Total != 8 {
		t.Fatalf("type with events: %s total %d", got, res.Total)
	}

	res, _ = d.r.Stats(StatsOptions{ChatArg: "c1group", By: "day", Limit: 2})
	got = strings.Join(outStatuses(res.Groups, func(g StatsGroup) string { return g.Day + "=" + itoa(uint32(g.Count)) }), " ")
	if got != "2026-03-02=2 2026-03-03=1" || res.GroupCount != 3 {
		t.Fatalf("newest 2 days: %s (of %d)", got, res.GroupCount)
	}

	res, _ = d.r.Stats(StatsOptions{ChatArg: "c1group", By: "hour", SinceTs: day(2, 0) / 1000})
	got = strings.Join(outStatuses(res.Groups, func(g StatsGroup) string { return itoa(uint32(*g.Hour)) + "=" + itoa(uint32(g.Count)) }), " ")
	if got != "9=2 20=1" {
		t.Fatalf("hours since day 2: %s", got)
	}

	res, _ = d.r.Stats(StatsOptions{ChatArg: "c1group", By: "hour", Limit: 1})
	got = strings.Join(outStatuses(res.Groups, func(g StatsGroup) string { return itoa(uint32(*g.Hour)) + "=" + itoa(uint32(g.Count)) }), " ")
	if got != "9=4 20=1" || res.GroupCount != 2 {
		t.Fatalf("--by hour must ignore --limit: %s (of %d)", got, res.GroupCount)
	}

	res, err = d.r.Stats(StatsOptions{ChatArg: "c1group", UntilTs: day(1, 0) / 1000})
	if err != nil || res.Total != 0 || res.Groups == nil || len(res.Groups) != 0 {
		t.Fatalf("empty range: %+v %v", res, err)
	}
	if _, err := d.r.Stats(StatsOptions{ChatArg: "c1group", By: "week"}); err == nil {
		t.Fatal("--by week accepted")
	}
}
