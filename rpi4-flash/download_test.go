package main

import "testing"

func TestValidSig(t *testing.T) {
	const fpr = alarmBuilderFpr
	good := "[GNUPG:] NEWSIG\n" +
		"[GNUPG:] GOODSIG 77193F152BDBE6A6 Arch Linux ARM Build System <builder@archlinuxarm.org>\n" +
		"[GNUPG:] VALIDSIG " + fpr + " 2026-08-05 1785934511 0 4 0 1 10 00 " + fpr + "\n"
	cases := []struct {
		name, status string
		want         bool
	}{
		{"valid", good, true},
		{"empty", "", false},
		{"bad signature", "[GNUPG:] BADSIG 77193F152BDBE6A6 Arch Linux ARM Build System\n", false},
		{"other key", "[GNUPG:] VALIDSIG AAAA 2026-08-05 1 0 4 0 1 10 00 0000000000000000000000000000000000000000\n", false},
		{"expired key", "[GNUPG:] EXPKEYSIG 77193F152BDBE6A6 Arch Linux ARM Build System\n" + good, false},
		{"revoked key", good + "[GNUPG:] REVKEYSIG 77193F152BDBE6A6 Arch Linux ARM Build System\n", false},
		{"not a status line", "VALIDSIG " + fpr + " x " + fpr + "\n", false},
	}
	for _, c := range cases {
		if got := validSig(c.status, fpr); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// The embedded keyring must be the pinned key: gpgv trusts whatever is in it.
func TestEmbeddedKeyIsNotEmpty(t *testing.T) {
	if len(alarmBuilderKey) < 1000 {
		t.Fatalf("embedded key is %d bytes", len(alarmBuilderKey))
	}
}
