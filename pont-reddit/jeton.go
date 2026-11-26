package main

// Le jeton de chat : des cookies de reddit.com au JWT qui ouvre le serveur
// Matrix de Reddit.
//
//  1. POST https://www.reddit.com/svc/shreddit/token, formulaire
//     « csrf_token=<cookie csrf_token> », avec les cookies, l'origine et le
//     référent de la page /chat/ et l'en-tête X-Reddit-Client-Version ;
//     réponse {"token": "<JWT>", "expires": …}.
//  2. À défaut : GET https://www.reddit.com/chat/, et le cookie token_v2 que
//     la page pose, ou l'attribut « token » de <rs-app> (du JSON échappé
//     {"token":…,"expires":…}).
//
// Le JWT n'est pas vérifié (ce n'est pas notre rôle) : on n'en lit que le
// compte (« lid », sinon « aid » : « t2_xxx ») et l'échéance (« exp »).
// L'identifiant Matrix est « @t2_xxx:reddit.com ».
//
// Rien de tout cela ne va dans les journaux : ni cookie, ni jeton.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Les adresses de Reddit (variables pour les essais, qui les pointent vers
// un faux serveur local).
var (
	redditBase = "https://www.reddit.com"
	matrixBase = "https://matrix.redditspace.com"
)

// Le serveur Matrix de Reddit nomme ses comptes « @t2_xxx:reddit.com ».
const matrixServerName = "reddit.com"

// Le navigateur dont le pont emprunte la voix (celui de la fenêtre de connexion).
const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

// La version du client web, si la page ne nous l'a pas apprise (Reddit la
// demande, sans trop la vérifier).
const clientVersionDefaut = "2026-11-01T12:00Z~jasette"

// Les cookies de reddit.com qu'on garde (les autres ne servent à rien).
var cookiesVoulus = map[string]bool{
	"reddit_session": true, "csrf_token": true, "token_v2": true, "loid": true,
	"session_tracker": true, "edgebucket": true, "rdt": true, "csv": true,
	"jasette_client_version": true,
}

// Renouveler le jeton tant de temps avant son échéance.
const refreshAvant = time.Hour

type jwtInfo struct {
	Account string    // « t2_xxx »
	Expires time.Time // zéro si le jeton n'en dit rien
}

// decodeJWT lit le compte et l'échéance d'un JWT (sans vérifier la signature).
func decodeJWT(tok string) (jwtInfo, error) {
	parts := strings.Split(strings.TrimSpace(tok), ".")
	if len(parts) != 3 {
		return jwtInfo{}, errors.New("jeton de chat illisible (pas un JWT)")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return jwtInfo{}, errors.New("jeton de chat illisible (base64)")
	}
	var c struct {
		Lid string  `json:"lid"`
		Aid string  `json:"aid"`
		Sub string  `json:"sub"`
		Exp float64 `json:"exp"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return jwtInfo{}, errors.New("jeton de chat illisible (JSON)")
	}
	acct := firstOf(c.Lid, c.Aid)
	if acct == "" && strings.HasPrefix(c.Sub, "t2_") {
		acct = c.Sub
	}
	if !strings.HasPrefix(acct, "t2_") {
		return jwtInfo{}, errors.New("le jeton de chat ne nomme aucun compte Reddit")
	}
	info := jwtInfo{Account: acct}
	if c.Exp > 0 {
		sec := int64(c.Exp)
		info.Expires = time.Unix(sec, int64((c.Exp-float64(sec))*1e9))
	}
	return info, nil
}

func matrixUserID(account string) string { return "@" + account + ":" + matrixServerName }

// aRenouveler : le jeton expire dans moins d'une heure (ou on n'en sait rien).
func aRenouveler(exp time.Time, now time.Time) bool {
	return exp.IsZero() || now.Add(refreshAvant).After(exp)
}

// cleanCookies : seulement les cookies utiles, sans espaces parasites.
func cleanCookies(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if cookiesVoulus[k] && v != "" {
			out[k] = v
		}
	}
	return out
}

func cookieHeader(c map[string]string) string {
	var b strings.Builder
	for k, v := range c {
		if k == "jasette_client_version" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		b.WriteString(k + "=" + v)
	}
	return b.String()
}

// noteSetCookies : les cookies que Reddit renouvelle en chemin (token_v2…).
func noteSetCookies(c map[string]string, resp *http.Response) {
	for _, ck := range resp.Cookies() {
		if cookiesVoulus[ck.Name] && ck.Value != "" && ck.MaxAge >= 0 {
			c[ck.Name] = ck.Value
		}
	}
}

// errSession : Reddit ne reconnaît plus les cookies (déconnecté ailleurs,
// mot de passe changé) : il faut se reconnecter.
var errSession = errors.New("Reddit ne reconnaît plus cette session : reconnectez Reddit")

// errBloque : une page de blocage temporaire (« whoa there, pardner »).
var errBloque = errors.New("Reddit bloque temporairement les requêtes de cette adresse : réessai plus tard")

type tokenResult struct {
	Token   string
	Expires time.Time
	Info    jwtInfo
}

// fetchToken : un jeton de chat neuf. Les cookies sont mis à jour (Reddit en
// renouvelle certains). La méthode 1, puis la 2.
func fetchToken(ctx context.Context, cli *http.Client, cookies map[string]string) (tokenResult, error) {
	if cookies["reddit_session"] == "" && cookies["token_v2"] == "" {
		return tokenResult{}, errors.New("il manque le cookie reddit_session : connectez-vous d'abord sur reddit.com dans la fenêtre")
	}
	r, err1 := tokenParShreddit(ctx, cli, cookies)
	if err1 == nil {
		return r, nil
	}
	log.Info().Str("raison", err1.Error()).Msg("jeton : /svc/shreddit/token refusé, essai par la page /chat/")
	r, err2 := tokenParPageChat(ctx, cli, cookies)
	if err2 == nil {
		return r, nil
	}
	if errors.Is(err2, errSession) || errors.Is(err2, errBloque) {
		return tokenResult{}, err2
	}
	return tokenResult{}, fmt.Errorf("jeton de chat introuvable (%v ; %v)", err1, err2)
}

func tokenParShreddit(ctx context.Context, cli *http.Client, cookies map[string]string) (tokenResult, error) {
	csrf := cookies["csrf_token"]
	if csrf == "" {
		return tokenResult{}, errors.New("pas de cookie csrf_token")
	}
	form := url.Values{"csrf_token": {csrf}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, redditBase+"/svc/shreddit/token", strings.NewReader(form))
	if err != nil {
		return tokenResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("Origin", redditBase)
	req.Header.Set("Referer", redditBase+"/chat/")
	req.Header.Set("X-Original-Referer", redditBase+"/chat/")
	req.Header.Set("X-Reddit-Client-Version", firstOf(cookies["jasette_client_version"], clientVersionDefaut))
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Cookie", cookieHeader(cookies))
	resp, err := cli.Do(req)
	if err != nil {
		return tokenResult{}, err
	}
	defer resp.Body.Close()
	noteSetCookies(cookies, resp)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return tokenResult{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return parseTokenJSON(body)
}

// parseTokenJSON : {"token": "<JWT>", "expires": <secondes ou millisecondes>}.
func parseTokenJSON(body []byte) (tokenResult, error) {
	var t struct {
		Token   string          `json:"token"`
		Expires json.RawMessage `json:"expires"`
	}
	if err := json.Unmarshal(body, &t); err != nil || t.Token == "" {
		return tokenResult{}, errors.New("réponse sans jeton")
	}
	info, err := decodeJWT(t.Token)
	if err != nil {
		return tokenResult{}, err
	}
	exp := info.Expires
	if e := parseExpires(t.Expires); !e.IsZero() && (exp.IsZero() || e.Before(exp)) {
		exp = e
	}
	return tokenResult{Token: t.Token, Expires: exp, Info: info}, nil
}

// parseExpires : un nombre (secondes ou millisecondes) ou une date ISO.
func parseExpires(raw json.RawMessage) time.Time {
	if len(raw) == 0 {
		return time.Time{}
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		if n > 1e12 {
			return time.UnixMilli(int64(n))
		}
		return time.Unix(int64(n), 0)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

var (
	reRsApp      = regexp.MustCompile(`<rs-app[^>]*\stoken="([^"]+)"`)
	reVersion    = regexp.MustCompile(`"?(?:client[-_]?version|clientVersion)"?\s*[:=]\s*"([0-9]{4}-[0-9]{2}-[0-9]{2}T[^"]{1,40})"`)
	reBloquePage = regexp.MustCompile(`(?i)blocked by network security|whoa there, pardner`)
)

func tokenParPageChat(ctx context.Context, cli *http.Client, cookies map[string]string) (tokenResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, redditBase+"/chat/", nil)
	if err != nil {
		return tokenResult{}, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "fr-CA,fr;q=0.9,en;q=0.8")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Cookie", cookieHeader(cookies))
	resp, err := cli.Do(req)
	if err != nil {
		return tokenResult{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	// Le cookie token_v2 posé par la page : le plus frais.
	frais := ""
	for _, ck := range resp.Cookies() {
		if ck.Name == "token_v2" && ck.Value != "" && ck.MaxAge >= 0 {
			frais = ck.Value
		}
	}
	noteSetCookies(cookies, resp)
	if reBloquePage.Match(body) {
		return tokenResult{}, errBloque
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return tokenResult{}, errSession
	}
	if resp.StatusCode != http.StatusOK {
		return tokenResult{}, fmt.Errorf("page /chat/ : HTTP %d", resp.StatusCode)
	}
	if m := reVersion.FindSubmatch(body); m != nil {
		cookies["jasette_client_version"] = string(m[1])
	}
	return tokenDePage(body, frais)
}

// tokenDePage : le jeton tiré de la page /chat/ (cookie neuf, sinon <rs-app>).
func tokenDePage(body []byte, cookieToken string) (tokenResult, error) {
	if cookieToken != "" {
		if info, err := decodeJWT(cookieToken); err == nil {
			return tokenResult{Token: cookieToken, Expires: info.Expires, Info: info}, nil
		}
	}
	if m := reRsApp.FindSubmatch(body); m != nil {
		return parseTokenJSON([]byte(html.UnescapeString(string(m[1]))))
	}
	return tokenResult{}, errSession
}
