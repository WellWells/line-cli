package cli

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// lang is the language of the help people read. JSON, errors and stderr notes
// stay English: they are for agents.
type lang int

const (
	langEN   lang = iota // English: every language other than Chinese
	langZhTW             // Traditional Chinese (Taiwan): any Chinese
)

// pick returns the text for l from an English and Traditional Chinese pair.
func (l lang) pick(s [2]string) string { return s[l] }

// detectLang reads LINECLI_LANG, then LANGUAGE, LC_ALL, LC_MESSAGES and LANG,
// then the user's Windows display languages. The first language found decides:
// Chinese of any region is langZhTW, anything else langEN.
func detectLang(getenv func(string) string, system func() []string) lang {
	for _, k := range []string{"LINECLI_LANG", "LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		for _, v := range strings.Split(getenv(k), ":") {
			v = strings.TrimSpace(v)
			if v == "" || v == "C" || v == "POSIX" || strings.HasPrefix(v, "C.") {
				continue
			}
			return langOf(v)
		}
	}
	if system != nil {
		for _, v := range system() {
			if v != "" {
				return langOf(v)
			}
		}
	}
	return langEN
}

// langOf maps a language tag (zh-TW, zh_CN.UTF-8, zh-Hant, en-US, ja) to a lang.
func langOf(tag string) lang {
	t := strings.ToLower(strings.ReplaceAll(tag, "_", "-"))
	if t == "zh" || strings.HasPrefix(t, "zh-") || strings.HasPrefix(t, "zh.") || strings.HasPrefix(t, "zh@") {
		return langZhTW
	}
	return langEN
}

// systemLanguages lists the user's Windows display languages, preferred first.
func systemLanguages() []string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil {
		return nil
	}
	return langs
}

func userLang() lang { return detectLang(os.Getenv, systemLanguages) }
