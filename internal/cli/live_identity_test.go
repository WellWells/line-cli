package cli

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/WellWells/line-cli/internal/line"
)

// Live identity checks: chat types, official accounts, --from, contacts and
// members. Like live_test.go they report only codes, counts and field names.

func TestLiveIdentityChatTypes(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	chats := mustRows(t, "chats", "--limit", "5000")
	byType := map[string]int{}
	for _, c := range chats {
		byType[str(c, "type")]++
		if !line.IsChatTypeFilter(str(c, "type")) {
			t.Fatalf("type %q is not a --chat-type value", str(c, "type"))
		}
		if str(c, "type") == "thread" && str(c, "parent_chat_id") == "" {
			t.Fatal("thread without parent_chat_id")
		}
	}
	for _, typ := range line.ChatTypeNames() {
		if got := len(mustRows(t, "chats", "--chat-type", typ, "--limit", "5000")); got != byType[typ] {
			t.Errorf("chats --chat-type %s: %d rows, the full list has %d", typ, got, byType[typ])
		}
	}

	// Official accounts: --chat-type official must list exactly the chats that
	// independent SQL finds, and every official contact's 1:1 chat.
	official := map[string]bool{}
	for _, c := range mustRows(t, "chats", "--chat-type", "official", "--limit", "5000") {
		official[str(c, "chat_id")] = true
	}
	db, err := line.Open(func(string) {}, false)
	if err != nil {
		t.Fatal(err)
	}
	want, err := line.NewReader(db).CountSQL(line.OfficialChatsSQL)
	db.Close()
	if err != nil || len(official) != want {
		t.Fatalf("chats --chat-type official: %d, SQL says %d (%v)", len(official), want, err)
	}
	n := 0
	for _, c := range mustRows(t, "contacts", "--limit", "5000") {
		if str(c, "type") == "official" && str(c, "chat_id") != "" {
			n++
			if !official[str(c, "chat_id")] {
				t.Fatal("official contact's chat is not typed official")
			}
		}
	}
	if n > 0 && len(official) == 0 {
		t.Fatal("official contacts with chats exist but chats --chat-type official is empty")
	}
	t.Logf("types %v, official chats %d, official contacts with a chat %d", byType, want, n)

	if len(official) > 0 {
		for _, u := range mustRows(t, "unread", "--limit-chats", "5000", "--per-chat-limit", "1") {
			if str(u, "type") == "official" {
				t.Fatal("unread lists an official chat without --include-official")
			}
		}
	}
	if hits := mustRows(t, "search", "--chat-type", "official", "--limit", "5"); len(official) > 0 && len(hits) == 0 {
		t.Error("search --chat-type official is empty")
	}
}

func TestLiveIdentityResolveNote(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	chats := mustRows(t, "chats", "--limit", "5000")
	names := map[string]int{}
	for _, c := range chats {
		names[strings.ToLower(str(c, "name"))]++
	}
	for _, c := range chats {
		id, name := str(c, "chat_id"), str(c, "name")
		if name == id || names[strings.ToLower(name)] != 1 || strings.TrimSpace(name) != name {
			continue
		}
		rc, _, stderr := runCLIErr("read", name, "--limit", "1")
		if rc != 0 || !strings.Contains(stderr, "note: resolved \"") || !strings.Contains(stderr, "-> "+id+" ("+str(c, "type")+", ") {
			t.Fatalf("read by name: rc=%d, resolve note missing or wrong chat", rc)
		}
		rc, _, stderr = runCLIErr("read", id, "--limit", "1")
		if rc != 0 || strings.Contains(stderr, "resolved") {
			t.Fatalf("read by chat_id: rc=%d, resolve note printed", rc)
		}
		return
	}
	t.Skip("no chat with a unique name")
}

func TestLiveIdentityFrom(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	runCase(t, liveCase{name: "from me", args: []string{"search", "--from", "me", "--limit", "200"},
		check: all(every("from_me not true", func(r map[string]any) bool { return r["from_me"] == true }), func(v any) string {
			if rows, _ := rowsOf(v); len(rows) == 0 {
				return "--from me found nothing"
			}
			return ""
		})})
	runCase(t, liveCase{name: "find-file from me", args: []string{"find-file", "--from", "me", "--limit", "50", "--no-local"},
		check: every("from_me not true", func(r map[string]any) bool { return r["from_me"] == true })})
	runCase(t, liveCase{name: "from unknown id", args: []string{"search", "--from", "u00000000000000000000000000000000"}, code: "SENDER_NOT_FOUND"})

	// A member id from members works with --from, also for senders in no name table.
	var cid, sender string
	var count int
	for _, c := range mustRows(t, "chats", "--limit", "50") {
		if typ := str(c, "type"); typ != "group" && typ != "open" {
			continue
		}
		for _, m := range mustRows(t, "members", str(c, "chat_id"), "--limit", "5") {
			if mc, _ := m["message_count"].(float64); mc > 0 && mc < 4000 && m["is_me"] != true {
				cid, sender, count = str(c, "chat_id"), str(m, "id"), int(mc)
				break
			}
		}
		if cid != "" {
			break
		}
	}
	if cid == "" {
		t.Skip("no group or open chat with an active member")
	}
	if hits := mustRows(t, "search", "--from", sender, "--chat", cid, "--limit", "5000"); len(hits) < count {
		t.Fatalf("search --from <member id>: %d hits, members says %d messages", len(hits), count)
	}

	// Two people with the same exact name make --from <name> SENDER_AMBIGUOUS.
	ids := map[string]map[string]bool{}
	for _, p := range mustRows(t, "contacts", "--include-members", "--limit", "5000") {
		name := str(p, "display_name")
		if name == "" || name == "me" || name == str(p, "contact_id") || p["is_me"] == true || strings.TrimSpace(name) != name || strings.ContainsAny(name, ",") {
			continue
		}
		if ids[name] == nil {
			ids[name] = map[string]bool{}
		}
		ids[name][str(p, "contact_id")] = true
	}
	for name, set := range ids {
		if len(set) < 2 {
			continue
		}
		runCase(t, liveCase{name: "from shared name", args: []string{"search", "--from", name}, code: "SENDER_AMBIGUOUS", check: func(v any) string {
			m, _ := v.(map[string]any)
			c, _ := m["candidates"].([]any)
			if len(c) < 2 {
				return fmt.Sprintf("want >= 2 candidates, got %d", len(c))
			}
			for _, x := range c {
				if msg := hasKeys("id", "name", "source")(x); msg != "" {
					return msg
				}
				if p, _ := x.(map[string]any); str(p, "chat_id") != "" && str(p, "chat_id") != str(p, "id") {
					return "candidate chat_id is not their own 1:1 chat"
				}
			}
			return ""
		}})
		return
	}
	t.Log("no two people share a name; SENDER_AMBIGUOUS not exercised")
}

func TestLiveIdentityContacts(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	rc, out, stderr := runCLIErr("contacts")
	rows, msg := rowsOf(decode(out))
	if rc != 0 || msg != "" || len(rows) == 0 || rows[0]["is_me"] != true {
		t.Fatalf("contacts: rc=%d %s", rc, msg)
	}
	if len(rows) > line.DefaultContactsLimit || (len(rows) == line.DefaultContactsLimit) != strings.Contains(stderr, "note: returned") {
		t.Fatalf("contacts default limit: %d rows, note=%v", len(rows), strings.Contains(stderr, "note:"))
	}
	chatName := map[string]string{}
	for _, c := range mustRows(t, "chats", "--limit", "5000") {
		chatName[str(c, "chat_id")] = str(c, "name")
	}
	all := mustRows(t, "contacts", "--limit", "5000")
	for i, c := range all {
		if str(c, "contact_id") == "" || str(c, "source") == "" {
			t.Fatalf("row %d: empty contact_id or source", i)
		}
		if typ := str(c, "type"); i > 0 && !slices.Contains([]string{"friend", "official", "other"}, typ) {
			t.Fatalf("row %d: type %q", i, typ)
		}
		if id := str(c, "chat_id"); id != "" && id != str(c, "contact_id") {
			t.Fatalf("row %d: chat_id is not the contact's own 1:1 chat", i)
		}
		if id := str(c, "chat_id"); id != "" && i > 0 && chatName[id] != str(c, "display_name") {
			t.Fatalf("row %d: display_name differs from the 1:1 chat's name", i)
		}
		if p := str(c, "profile_name"); p != "" && p == str(c, "display_name") {
			t.Fatalf("row %d: profile_name repeats display_name", i)
		}
	}
	members := mustRows(t, "contacts", "--include-members", "--limit", "5000")
	if len(members) < len(all) {
		t.Fatalf("--include-members returned fewer rows (%d < %d)", len(members), len(all))
	}
	open := len(mustRows(t, "chats", "--chat-type", "open", "--limit", "5000"))
	n := 0
	for _, m := range members {
		if str(m, "source") == "open_chat_member" {
			n++
			if str(m, "community") == "" {
				t.Fatal("open-chat member without community")
			}
		}
	}
	if open > 0 && n == 0 {
		t.Fatal("open chats exist but --include-members lists no open-chat member")
	}
	// At the default limit, members either show up or a note says how many were cut.
	rc, out, stderr = runCLIErr("contacts", "--include-members")
	rows, msg = rowsOf(decode(out))
	shown := 0
	for _, m := range rows {
		if str(m, "source") == "open_chat_member" {
			shown++
		}
	}
	if rc != 0 || msg != "" || (n > 0 && shown == 0 && !strings.Contains(stderr, "open-chat members match")) {
		t.Fatalf("contacts --include-members: rc=%d, %d members shown of %d, no note", rc, shown, n)
	}
	t.Logf("contacts %d, open-chat members %d, shown at default limit %d", len(all), n, shown)
	runCase(t, liveCase{name: "contacts limit", args: []string{"contacts", "--limit", "3"}, check: maxLen(3)})
}

// TestLiveIdentityPersonWithoutChat reads by the name of people who have no
// 1:1 chat: that must not resolve to some other chat containing the name.
func TestLiveIdentityPersonWithoutChat(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	chatNames := map[string]bool{}
	for _, c := range mustRows(t, "chats", "--limit", "5000") {
		chatNames[strings.ToLower(str(c, "name"))] = true
	}
	people := mustRows(t, "contacts", "--limit", "5000")
	for _, p := range people {
		if str(p, "chat_id") != "" {
			chatNames[strings.ToLower(str(p, "profile_name"))] = true
		}
	}
	tried, codes := 0, map[string]int{}
	for _, p := range people {
		name := str(p, "display_name")
		if tried == 20 || str(p, "chat_id") != "" || p["is_me"] == true || name == str(p, "contact_id") ||
			chatNames[strings.ToLower(name)] || strings.TrimSpace(name) != name {
			continue
		}
		tried++
		rc, out, _ := runCLIErr("read", name, "--limit", "1")
		m, _ := decode(out).(map[string]any)
		code, _ := m["error"].(string)
		codes[code]++
		if msg, _ := m["message"].(string); rc != 1 || (code != "CHAT_NOT_FOUND" && code != "CHAT_AMBIGUOUS") || !strings.Contains(msg, "search --from") {
			t.Fatalf("read <name of a person without a chat>: rc=%d code=%q, want CHAT_NOT_FOUND or CHAT_AMBIGUOUS with a search --from hint", rc, code)
		}
	}
	t.Logf("tried %d names: %v", tried, codes)
}

func TestLiveIdentityMembers(t *testing.T) {
	liveOnly(t, "LINECLI_LIVE")
	done := map[string]bool{}
	for _, c := range mustRows(t, "chats", "--limit", "5000") {
		typ, id := str(c, "type"), str(c, "chat_id")
		if done[typ] || typ == "thread" {
			continue
		}
		done[typ] = true
		rc, out, stderr := runCLIErr("members", id, "--limit", "5000")
		rows, msg := rowsOf(decode(out))
		if rc != 0 || msg != "" {
			t.Fatalf("members (%s): rc=%d %s", typ, rc, msg)
		}
		me := 0
		for _, m := range rows {
			if m["is_me"] == true {
				me++
			}
			if msg := hasKeys("id", "name", "message_count")(m); msg != "" {
				t.Fatalf("members (%s): %s", typ, msg)
			}
		}
		switch typ {
		case "personal", "official":
			if len(rows) != 2 || me != 1 {
				t.Errorf("members (%s): %d rows, %d is_me", typ, len(rows), me)
			}
		case "self":
			if len(rows) != 1 || me != 1 {
				t.Errorf("members (self): %d rows", len(rows))
			}
		case "group":
			if mc, _ := c["member_count"].(float64); mc > 0 && (len(rows) < int(mc) || me != 1) {
				t.Errorf("members (group): %d rows for member_count %d, %d is_me", len(rows), int(mc), me)
			}
		case "open":
			if !strings.Contains(stderr, "not a complete member list") {
				t.Error("members (open): no completeness note")
			}
		}
	}
	runCase(t, liveCase{name: "members not found", args: []string{"members", "zzzqqq_nomatch_123"}, code: "CHAT_NOT_FOUND"})
	runCase(t, liveCase{name: "members no arg", args: []string{"members"}, code: "INVALID_ARGUMENT"})
}
