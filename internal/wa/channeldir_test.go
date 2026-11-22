package wa

import (
	"encoding/json"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestAcharCanaisNaResposta(t *testing.T) {
	data := `{"xwa2_newsletters_directory_list":{"page_info":{"end_cursor":"x"},"result":[
		{"id":"1@newsletter","thread_metadata":{"name":{"text":"Notícias"},"subscribers_count":"1200","verification":"verified"}},
		{"id":"2@newsletter","thread_metadata":{"name":{"text":"Receitas"},"description":{"text":"bolos"}}},
		{"id":"nao-canal","thread_metadata":{}}]}}`
	var tree any
	if err := json.Unmarshal([]byte(data), &tree); err != nil {
		t.Fatal(err)
	}
	var mds []types.NewsletterMetadata
	findChannels(tree, &mds)
	if len(mds) != 2 {
		t.Fatalf("achou %d canais", len(mds))
	}
	c := channelOf(&mds[0])
	if c.Name != "Notícias" || c.Subscribers != 1200 || !c.Verified || c.JID != "1@newsletter" {
		t.Fatalf("canal: %+v", c)
	}
}
