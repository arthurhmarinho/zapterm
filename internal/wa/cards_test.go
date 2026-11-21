package wa

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestVCard(t *testing.T) {
	v := "BEGIN:VCARD\r\nVERSION:3.0\r\nN:;Zé;;;\r\nFN:Zé da Oficina\r\nitem1.TEL;waid=5511900000004:+55 11 90000-0004\r\nitem1.X-ABLabel:Celular\r\nEND:VCARD"
	name, phone, waid := parseVCard(v)
	if name != "Zé da Oficina" || phone != "+55 11 90000-0004" || waid != "5511900000004" {
		t.Fatalf("%q %q %q", name, phone, waid)
	}
	c := contactCard(&waE2E.ContactMessage{Vcard: proto.String(v)})
	if c.Title != "Zé da Oficina" || c.Chat != "5511900000004@s.whatsapp.net" {
		t.Fatalf("cartão: %+v", c)
	}
}

func TestDescribeNovosTipos(t *testing.T) {
	for _, tc := range []struct {
		m    *waE2E.Message
		want string
	}{
		{&waE2E.Message{PollCreationMessageV5: &waE2E.PollCreationMessage{Name: proto.String("Pizza?")}}, "[enquete: Pizza?]"},
		{&waE2E.Message{LiveLocationMessage: &waE2E.LiveLocationMessage{}}, "[localização em tempo real]"},
		{&waE2E.Message{ImageMessage: &waE2E.ImageMessage{ViewOnce: proto.Bool(true)}}, "[visualização única]"},
	} {
		if kind, text := describe(tc.m); kind != "other" || text != tc.want {
			t.Errorf("%q %q, queria %q", kind, text, tc.want)
		}
	}
}

func TestHashesDoVoto(t *testing.T) {
	got := hexHashes([][]byte{{0xab, 0x01}})
	if len(got) != 1 || got[0] != "ab01" {
		t.Fatalf("%v", got)
	}
}
