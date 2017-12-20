// +build darwin

package test

import (
	"os"
	"testing"
)

func TestIntegration__date(t *testing.T) {
	cmd := Command("date", "-u", "-r", "0").Trim()
	cmd.EqualT(t, "Thu Jan  1 00:00:00 UTC 1970")

	ans := `Command:
  date -u -r 0
Output:
  Thu Jan  1 00:00:00 UTC 1970`
	if cmd.String() != ans {
		t.Errorf("cmd.String() = '%s'", cmd.String())
	}
}

func TestIntegration__unknown(t *testing.T) {
	cmd := CertManage("other").Trim()
	cmd.FailedT(t)
}

func TestIntegration__list(t *testing.T) {
	cmd := CertManage("list", "-count").Trim()
	cmd.CmpFnT(t, func(i int) bool { return i > 1 })
}

func TestIntegration__backup(t *testing.T) {
	cmd := CertManage("backup").Trim()
	cmd.EqualT(t, "Backup completed successfully")
}

// TODO(adam): Need to run -whitelist and -restore

// Firefox tests
func TestIntegration__firefox(t *testing.T) {
	if !online(t) {
		t.Skip("offline, can't run firefox tests (no NSS setup)")
	}

	// Make a request using the Keychain to get it ready
	// travis needs this
	cmd := Command("curl", "-s", "-o", os.DevNull, "https://google.com")
	cmd.SuccessT(t)

	// Verify firefox has found certificates
	cmd = CertManage("list", "-app", "firefox", "-count").Trim()
	cmd.SuccessT(t)
	cmd.CmpFnT(t, func(i int) bool { return i > 1 })
}
