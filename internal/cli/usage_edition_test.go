package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestOverviewSaysWhatTheOpenBuildLacks(t *testing.T) {
	useEdition(t, "open")
	var b bytes.Buffer
	printOverview(&b, langEN)
	got := b.String()
	if strings.Contains(got, "sends messages as you") {
		t.Errorf("open build claims to send:\n%s", got)
	}
	for _, want := range []string{"open build", "Official prebuilt"} {
		if !strings.Contains(got, want) {
			t.Errorf("open overview lacks %q:\n%s", want, got)
		}
	}
}

func TestOverviewOfFullBuildIsUnchanged(t *testing.T) {
	useEdition(t, "full")
	var b bytes.Buffer
	printOverview(&b, langEN)
	got := b.String()
	if !strings.Contains(got, "sends messages as you") || strings.Contains(got, "open build") {
		t.Errorf("full overview:\n%s", got)
	}
}

func TestCommandHelpMarksOnlineCommandsInOpenBuild(t *testing.T) {
	useEdition(t, "open")
	var send, read bytes.Buffer
	printCommandHelp(&send, langEN, "send")
	printCommandHelp(&read, langEN, "read")
	if !strings.Contains(send.String(), "not available in this open build") {
		t.Errorf("send help:\n%s", send.String())
	}
	if strings.Contains(read.String(), "not available in this open build") {
		t.Errorf("read help:\n%s", read.String())
	}
}
