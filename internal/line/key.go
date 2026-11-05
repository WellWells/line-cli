package line

func readFirstPage(mainEdb string) ([]byte, int, error) {
	f, err := openShared(mainEdb)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	ps, err := readPageSize(f)
	if err != nil {
		return nil, 0, err
	}
	page1 := make([]byte, ps)
	if _, err := f.ReadAt(page1, 0); err != nil {
		return nil, 0, &Error{Code: ErrDBAccess, Message: "cannot read first page"}
	}
	return page1, ps, nil
}

func verifyPassphrase(page1 []byte, passphrase []byte) (key, bool) {
	m := deriveKey(passphrase)
	buf := append([]byte(nil), page1...)
	if decryptPage1(buf, m) {
		return m, true
	}
	return key{}, false
}

func scanForKey(page1 []byte) (string, key, error) {
	cands, err := scanKeyCandidates()
	if err != nil {
		return "", key{}, err
	}
	for _, c := range cands {
		if m, ok := verifyPassphrase(page1, []byte(c)); ok {
			return c, m, nil
		}
	}
	return "", key{}, &Error{Code: ErrNoKeyDecrypted, Message: "Found key candidates but none decrypted the database. LINE may have rotated its key or changed its cipher."}
}

func resolveKey(mainEdb string, forceRescan, mayStartLine bool, notify func(string)) (key, error) {
	page1, _, err := readFirstPage(mainEdb)
	if err != nil {
		return key{}, err
	}
	if !forceRescan {
		if stored, ok := loadStoredKey(); ok {
			if m, ok := verifyPassphrase(page1, stored); ok {
				return m, nil
			}
		}
	}
	if notify != nil {
		notify("scanning LINE memory for the database key (first run; this takes a few seconds)…")
	}
	type scanned struct {
		passphrase string
		master     key
	}
	found, err := whenLineReady(func() (scanned, error) {
		p, m, err := scanForKey(page1)
		return scanned{p, m}, err
	}, mayStartLine, notify)
	if err != nil {
		return key{}, err
	}
	_ = saveKey([]byte(found.passphrase))
	return found.master, nil
}

func Init(notify func(string)) error {
	loc, err := Locate()
	if err != nil {
		return err
	}
	_, err = resolveKey(loc.MainEdb, true, true, notify)
	return err
}

func Forget() error { return forgetKey() }
