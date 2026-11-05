package line

import (
	"errors"
	"strings"
	"testing"
)

func TestMissingOptionalTablesReadAsEmpty(t *testing.T) {
	d := outTestDB(t, "_groupChat", "_squareChat", "_squareMember", "_square", "_squareThread", "_room", "_buddyDetail", "_chatOption")
	d.chat("u-peer-1")
	d.msg("m1", "u-peer-1", 1_700_000_000_000, 0, map[string]any{"_text": "hello"})
	chats, err := d.r.ListChats(ChatsOptions{})
	if err != nil || len(chats) != 1 {
		t.Fatalf("ListChats without optional tables: %v %v", chats, err)
	}
	if _, err := d.r.ReadHistory(ReadOptions{ChatArg: "u-peer-1"}); err != nil {
		t.Fatalf("ReadHistory without optional tables: %v", err)
	}
	// A renamed column in an optional table is tolerated too.
	if err := d.conn.Exec("CREATE TABLE _squareChat(_squareChatMid TEXT)"); err != nil {
		t.Fatal(err)
	}
	r := NewReader(d.r.db)
	if err := r.buildMaps(); err != nil {
		t.Fatalf("optional table missing a column: %v", err)
	}
}

func TestMissingCoreSchemaFailsLoudly(t *testing.T) {
	for _, table := range []string{"_message", "_chat", "_contact", "_profile"} {
		d := outTestDB(t, table)
		_, err := d.r.ListChats(ChatsOptions{})
		if err == nil {
			_, err = d.r.Search(SearchOptions{Query: "x"})
		}
		if err == nil {
			_, err = d.r.Contacts("")
		}
		le, ok := AsError(err)
		if !ok || le.Code != ErrDBUnreadable || !strings.Contains(le.Message, "schema changed") || !strings.Contains(le.Message, table) {
			t.Errorf("missing %s: got %v", table, err)
		}
	}
	d := outTestDB(t)
	if err := d.conn.Exec("DROP TABLE _message; CREATE TABLE _message(_id TEXT, _chatId TEXT, _createdTime INTEGER)"); err != nil {
		t.Fatal(err)
	}
	_, err := d.r.Search(SearchOptions{Query: "x"})
	if le, ok := AsError(err); !ok || le.Code != ErrDBUnreadable || !strings.Contains(le.Message, "no such column") {
		t.Fatalf("missing core column: got %v", err)
	}
	if err := classifyQueryErr(errors.New("database is locked")); err == nil || !strings.Contains(err.Error(), string(ErrDBLocked)) {
		t.Fatalf("locked: %v", err)
	}
}
