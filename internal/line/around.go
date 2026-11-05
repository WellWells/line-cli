package line

// readAround returns the messages around opt.Around in its chat, oldest first:
// limit/2 before it, the message itself, and the rest after it. Messages sent in
// the same millisecond are ordered by id. A given chat must be the message's chat.
func (r *Reader) readAround(opt ReadOptions) ([]Message, error) {
	chatID, at, err := r.messageAt(opt.Around)
	if err != nil {
		return nil, err
	}
	if opt.ChatArg != "" {
		want, err := r.ResolveChat(opt.ChatArg, false)
		if err != nil {
			return nil, err
		}
		if want != chatID {
			return nil, &Error{Code: ErrInvalidArgument, Message: "message " + opt.Around + " is in chat " + chatID + ", not " + want}
		}
	}
	limit := opt.EffectiveLimit()
	before := limit / 2
	older, err := r.readRows("SELECT * FROM _message WHERE _chatId=? AND (_createdTime<? OR (_createdTime=? AND _id<?)) ORDER BY _createdTime DESC, _id DESC LIMIT ?",
		chatID, at, at, opt.Around, before)
	if err != nil {
		return nil, err
	}
	newer, err := r.readRows("SELECT * FROM _message WHERE _chatId=? AND (_createdTime>? OR (_createdTime=? AND _id>=?)) ORDER BY _createdTime, _id LIMIT ?",
		chatID, at, at, opt.Around, limit-before)
	if err != nil {
		return nil, err
	}
	reverse(older)
	return append(older, newer...), nil
}

func (r *Reader) readRows(sql string, params ...any) ([]Message, error) {
	st, err := r.query(sql, params...)
	if err != nil || st == nil {
		return nil, err
	}
	defer st.Close()
	idx := colIndex(st)
	var out []Message
	for st.Step() {
		out = append(out, r.readMessageRow(st, idx))
	}
	return out, stepErr(st)
}

// MessageChat is the chat_id of a stored message; read --around uses it when no
// chat is given.
func (r *Reader) MessageChat(id string) (string, error) {
	chatID, _, err := r.messageAt(id)
	return chatID, err
}

func (r *Reader) messageAt(id string) (string, int64, error) {
	st, err := r.query("SELECT _chatId, _createdTime FROM _message WHERE _id=?", id)
	if err != nil {
		return "", 0, err
	}
	var chatID string
	var at int64
	found := false
	if st != nil {
		if found = st.Step(); found {
			chatID, at = st.ColumnText(0), st.ColumnInt64(1)
		}
		err = stepErr(st)
		st.Close()
		if err != nil {
			return "", 0, err
		}
	}
	if !found {
		return "", 0, &Error{Code: ErrInvalidArgument, Message: "no message with id " + id + " in the local database"}
	}
	return chatID, at, nil
}
