package main

import "testing"

// Le plateau Facebook, d'une forme plausible : les personnes, l'heure la plus
// récente, l'échéance, « déjà vu » ; inconnu veut dire pas vu.
func TestAnalyserStoriesFB(t *testing.T) {
	corps := []byte(`for (;;);{"data":{"me":{"unified_stories_buckets":{"edges":[
		{"node":{"id":"b1","story_bucket_owner":{"__typename":"User","id":"100001"},"is_bucket_seen_by_viewer":false,
			"unified_stories":{"edges":[{"node":{"creation_time":1764150000}},{"node":{"creation_time":1764160000}}]}}},
		{"node":{"id":"b2","story_bucket_owner":{"id":"100002"},"is_bucket_seen_by_viewer":true,
			"unified_stories":{"edges":[{"node":{"creation_time":1764100000,"expiration_time":1764186400}}]}}},
		{"node":{"__typename":"StoryBucket","owner":{"id":"100003"}}},
		{"node":{"story_bucket_owner":{"id":"pas un id"}}}]}}}}`)
	s, err := analyserStoriesFB(corps)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 3 {
		t.Fatalf("%d : %+v", len(s), s)
	}
	if s[0].Owner != "100001" || s[0].Seen || s[0].Latest != 1764160000000 || s[0].Until != (1764160000+86400)*1000 {
		t.Fatalf("première : %+v", s[0])
	}
	if s[1].Owner != "100002" || !s[1].Seen || s[1].Until != 1764186400000 {
		t.Fatalf("deuxième : %+v", s[1])
	}
	if s[2].Owner != "100003" || s[2].Seen || s[2].Latest != 0 {
		t.Fatalf("troisième : %+v", s[2])
	}
	if _, err := analyserStoriesFB([]byte(`{"errors":[{"message":"PersistedQueryNotFound"}]}`)); err == nil {
		t.Fatal("erreur acceptée")
	}
}
