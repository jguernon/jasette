package main

import "testing"

// Le plateau : sourdine et entrées sans nom écartées, « vue » quand seen
// atteint la story la plus récente, échéance de 24 h par défaut.
func TestAnalyserReelsTray(t *testing.T) {
	corps := []byte(`{"status":"ok","tray":[
		{"latest_reel_media":1764160000,"expiring_at":1764246400,"seen":0,"user":{"pk":1784,"username":"camille.t"}},
		{"latest_reel_media":1764160000,"seen":1764160000,"user":{"pk":"42","username":"luc"}},
		{"latest_reel_media":1764160000,"seen":0,"muted":true,"user":{"pk":7,"username":"bruit"}},
		{"latest_reel_media":1764160000,"seen":0,"user":{"pk":8,"username":""}}]}`)
	s, err := analyserReelsTray(corps)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 2 {
		t.Fatalf("%d stories : %+v", len(s), s)
	}
	if s[0].User != "1784" || s[0].Seen || s[0].Latest != 1764160000000 || s[0].Until != 1764246400000 {
		t.Fatalf("première : %+v", s[0])
	}
	if s[1].User != "42" || !s[1].Seen || s[1].Until != (1764160000+86400)*1000 {
		t.Fatalf("deuxième : %+v", s[1])
	}
	if lienStoryIG("luc") != "https://www.instagram.com/stories/luc/" {
		t.Fatal("lien")
	}
	if _, err := analyserReelsTray([]byte(`{"status":"fail","message":"login_required"}`)); err == nil {
		t.Fatal("plateau absent accepté")
	}
}
