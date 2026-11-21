package wa

import "testing"

func TestCodigoDoConvite(t *testing.T) {
	for in, want := range map[string]string{
		"https://chat.whatsapp.com/ABCdef123":        "ABCdef123",
		"chat.whatsapp.com/ABCdef123/":               "ABCdef123",
		"https://chat.whatsapp.com/invite/ABCdef123": "ABCdef123",
	} {
		if got, err := inviteCode(in); err != nil || got != want {
			t.Errorf("%q → %q (%v)", in, got, err)
		}
	}
	for _, bad := range []string{"https://whatsapp.com/ABC", "https://chat.whatsapp.com/", "https://evil.com/chat.whatsapp.com/x"} {
		if _, err := inviteCode(bad); err == nil {
			t.Errorf("%q deveria ser recusado", bad)
		}
	}
}
