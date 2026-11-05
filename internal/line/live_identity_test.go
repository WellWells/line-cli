package line

import (
	"os"
	"testing"
)

// TestLiveIdentitySQL checks chat typing against independent SQL on the real
// database (LINECLI_LIVE=1, LINE running). It reports counts only.
func TestLiveIdentitySQL(t *testing.T) {
	if os.Getenv("LINECLI_LIVE") != "1" {
		t.Skip("set LINECLI_LIVE=1 to run against the real LINE database")
	}
	db, err := Open(func(string) {}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := NewReader(db)
	count := func(sql string) int {
		t.Helper()
		n, err := r.CountSQL(sql)
		if err != nil {
			t.Fatalf("count failed: %v", err)
		}
		return n
	}
	chats, err := r.ListChats(ChatsOptions{Limit: 5000})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	muted := 0
	for _, c := range chats {
		got[c.Type]++
		if c.Muted {
			muted++
		}
	}
	official := count(OfficialChatsSQL)
	if got["official"] != official {
		t.Errorf("official chats: %d, SQL says %d", got["official"], official)
	}
	for typ, sql := range map[string]string{
		"thread": "SELECT count(*) FROM _chat WHERE _midType=7",
		"open":   "SELECT count(*) FROM _chat WHERE _midType=4",
		"group":  "SELECT count(*) FROM _chat WHERE _midType=2",
		"self":   "SELECT count(*) FROM _chat WHERE _id IN (SELECT _mid FROM _profile)",
	} {
		if want := count(sql); got[typ] != want {
			t.Errorf("%s chats: %d, SQL says %d", typ, got[typ], want)
		}
	}
	if want := count("SELECT count(*) FROM _chat WHERE _midType=0"); got["personal"]+got["official"]+got["self"] != want {
		t.Errorf("1:1 chats: %d, SQL says %d", got["personal"]+got["official"]+got["self"], want)
	}
	if want := count("SELECT count(*) FROM _chatOption o JOIN _chat c ON c._id=o._id WHERE o._notification=0"); muted != want {
		t.Errorf("muted: %d, SQL says %d", muted, want)
	}
	t.Logf("types %v", got)
}
