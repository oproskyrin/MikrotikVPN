package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed web/index.html
var webFS embed.FS

type Service struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Note     string   `json:"note"`
	Domains  []string `json:"domains"`
}
type State struct {
	Services      []string `json:"services"`
	CustomDomains []string `json:"custom_domains"`
	Channel       string   `json:"channel"`
	VLESSURL      string   `json:"vless_url"`
}
type App struct {
	mu                                                              sync.Mutex
	state                                                           State
	password, dataDir, routerAPI, routerUser, routerPass, xraySocks string
	sessions                                                        map[string]time.Time
	geo                                                             map[string][]string
}

var catalog = []Service{
	{"youtube", "YouTube", "Видео", "Видео, музыка и трансляции", []string{"youtube.com", "youtu.be", "youtubei.googleapis.com", "googlevideo.com", "ytimg.com", "youtube-nocookie.com"}},
	{"instagram", "Instagram", "Соцсети", "Веб и приложение", []string{"instagram.com", "cdninstagram.com", "fbcdn.net"}},
	{"facebook", "Facebook", "Соцсети", "Соцсеть и Messenger", []string{"facebook.com", "facebook.net", "fbcdn.net", "messenger.com"}},
	{"x", "X / Twitter", "Соцсети", "Новости и сообщения", []string{"x.com", "twitter.com", "twimg.com", "t.co"}},
	{"openai", "ChatGPT / OpenAI", "ИИ", "ChatGPT, API и статические файлы", []string{"chatgpt.com", "openai.com", "oaistatic.com", "oaiusercontent.com"}},
	{"anthropic", "Claude / Anthropic", "ИИ", "Claude и API", []string{"claude.ai", "anthropic.com", "claudeusercontent.com"}},
	{"zai", "z.ai (GLM)", "ИИ", "Чат и API", []string{"z.ai", "chat.z.ai", "api.z.ai", "glm.ai"}},
	{"telegram", "Telegram", "Мессенджеры", "Web-версия и загрузки", []string{"web.telegram.org", "telegram.org", "t.me", "tdesktop.com"}},
	{"telegram-web", "Telegram (веб)", "Мессенджеры", "Веб-версия Telegram", []string{"web.telegram.org", "telegram.org", "t.me", "tdesktop.com"}},
	{"discord", "Discord", "Мессенджеры", "Текст и голос", []string{"discord.com", "discordapp.com", "discordapp.net", "discord.media"}},
	{"reddit", "Reddit", "Соцсети", "Сообщество", []string{"reddit.com", "redd.it", "redditstatic.com", "redditmedia.com"}},
	{"twitch", "Twitch", "Видео", "Стриминг", []string{"twitch.tv", "ttvnw.net", "jtvnw.net"}},
	{"netflix", "Netflix", "Видео", "Видео", []string{"netflix.com", "nflxvideo.net", "nflximg.net", "nflxso.net"}},
	{"spotify", "Spotify", "Музыка", "Музыка", []string{"spotify.com", "scdn.co", "spotifycdn.com"}},
	{"notion", "Notion", "Работа", "Заметки", []string{"notion.so", "notion.site", "notion-static.com"}},
	{"github", "GitHub", "Работа", "Код и релизы", []string{"github.com", "githubusercontent.com", "githubassets.com", "githubapp.com"}},
	{"rutracker", "RuTracker", "Другое", "Трекер и зеркала", []string{"rutracker.org", "rutracker.net", "rutracker.nl", "rutracker.wiki"}},
	{"intel", "Intel", "Работа", "Сайт и драйверы", []string{"intel.com", "intel.ru", "downloadmirror.intel.com", "ark.intel.com"}},
}

var geoFiles = map[string]string{"youtube": "youtube.txt", "discord": "discord.txt", "google": "google.txt"}

func main() {
	a := &App{dataDir: env("DATA_DIR", "/data"), password: os.Getenv("PANEL_PASSWORD"), routerAPI: os.Getenv("ROUTER_API"), routerUser: os.Getenv("ROUTER_USER"), routerPass: os.Getenv("ROUTER_PASS"), xraySocks: os.Getenv("XRAY_SOCKS"), sessions: map[string]time.Time{}, geo: map[string][]string{}}
	a.load()
	a.loadGeo()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", a.login)
	mux.HandleFunc("/api/data", a.auth(a.data))
	mux.HandleFunc("/api/apply", a.auth(a.apply))
	mux.HandleFunc("/api/geodata/refresh", a.auth(a.refreshGeo))
	mux.HandleFunc("/api/xray", a.auth(a.xrayStatus))
	mux.HandleFunc("/api/test", a.auth(a.test))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		b, _ := webFS.ReadFile("web/index.html")
		page := string(b)
		page = strings.Replace(page, "</div></div><div id=\"services\"", "</div><button id=\"geo-refresh\" style=\"margin-top:12px;border:1px solid #3b4d66;border-radius:9px;background:#151d28;color:#c7d4e5;padding:9px 12px;font-weight:600;cursor:pointer\" onclick=\"refreshGeo()\">↻ Обновить GeoSite через VLESS</button></div><div id=\"services\"", 1)
		page = strings.Replace(page, "</textarea><div class=\"channels\">", "</textarea><label for=\"vless\" style=\"display:block;margin:16px 0 8px;font-weight:650\">Ключ VLESS</label><input id=\"vless\" type=\"password\" placeholder=\"vless://uuid@сервер:порт?type=tcp&amp;security=tls…\"><div class=\"geo\" style=\"margin-top:7px\">Ключ хранится только на вашем роутере.</div><div class=\"channels\">", 1)
		page = strings.Replace(page, "$('custom').value=(data.state.custom_domains||[]).join('\\n');render();status()", "$('custom').value=(data.state.custom_domains||[]).join('\\n');$('vless').value=data.state.vless_url||'';render();status()", 1)
		page = strings.Replace(page, "channel:'vless'})", "channel:'vless',vless_url:$('vless').value.trim()})", 1)
		page = strings.Replace(page, "</body>", "<script>async function refreshGeo(){let b=document.getElementById('geo-refresh'),out=document.getElementById('status');b.disabled=true;out.className='status';out.textContent='Скачиваю GeoSite через VLESS…';try{let r=await api('/api/geodata/refresh',{method:'POST'});if(r.ok){out.className='status ok';out.textContent='Обновлено категорий: '+r.updated;data=await api('/api/data');render()}else{out.className='status error';out.textContent=r.error||'Не удалось обновить GeoSite'}}catch{out.className='status error';out.textContent='Нет связи с панелью'}b.disabled=false}</script></body>", 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	})
	addr := env("LISTEN", ":8080")
	log.Printf("Unblock Panel v2 on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func (a *App) load() {
	b, e := os.ReadFile(filepath.Join(a.dataDir, "panel-v2-state.json"))
	if e != nil {
		b, e = os.ReadFile(filepath.Join(a.dataDir, "state.json"))
	}
	if e == nil {
		_ = json.Unmarshal(b, &a.state)
	}
	if a.state.Channel == "" {
		a.state.Channel = "vless"
	}
}
func (a *App) save() error {
	_ = os.MkdirAll(a.dataDir, 0700)
	b, _ := json.MarshalIndent(a.state, "", "  ")
	return os.WriteFile(filepath.Join(a.dataDir, "panel-v2-state.json"), b, 0600)
}
func jsonOut(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if a.password == "" || subtle.ConstantTimeCompare([]byte(in.Password), []byte(a.password)) != 1 {
		http.Error(w, "unauthorized", 401)
		return
	}
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	token := fmt.Sprintf("%x", raw)
	a.mu.Lock()
	a.sessions[token] = time.Now().Add(24 * time.Hour)
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "up_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	jsonOut(w, map[string]bool{"ok": true})
}
func (a *App) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie("up_session")
		a.mu.Lock()
		until, ok := a.sessions[func() string {
			if e != nil {
				return ""
			}
			return c.Value
		}()]
		a.mu.Unlock()
		if !ok || time.Now().After(until) {
			http.Error(w, "unauthorized", 401)
			return
		}
		next(w, r)
	}
}
func (a *App) data(w http.ResponseWriter, r *http.Request) {
	jsonOut(w, map[string]any{"catalog": a.currentCatalog(), "state": a.state, "lan": os.Getenv("LAN_ADDR"), "channelReady": a.state.Channel == "vless"})
}
func selectedDomains(services []Service, ids, custom []string) []string {
	set := map[string]bool{}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	for _, s := range services {
		if wanted[s.ID] {
			for _, d := range s.Domains {
				if strings.Contains(d, ":") {
					set[d] = true
				} else {
					set["domain:"+d] = true
				}
			}
		}
	}
	for _, d := range custom {
		d = strings.TrimSpace(strings.ToLower(d))
		if d != "" {
			set["domain:"+d] = true
		}
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}
func (a *App) apply(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	var in State
	if e := json.NewDecoder(r.Body).Decode(&in); e != nil {
		http.Error(w, "bad json", 400)
		return
	}
	if in.Channel != "vless" {
		jsonOut(w, map[string]string{"error": "AmneziaWG пока недоступен: контейнер wireproxy не запускается."})
		return
	}
	domains := selectedDomains(a.currentCatalog(), in.Services, in.CustomDomains)
	if len(domains) == 0 {
		jsonOut(w, map[string]string{"error": "Выберите сервис или добавьте домен."})
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if in.VLESSURL != "" && in.VLESSURL != a.state.VLESSURL {
		if e := a.writeVLESSConfig(in.VLESSURL, domains); e != nil {
			jsonOut(w, map[string]string{"error": "Не удалось сохранить VLESS: " + e.Error()})
			return
		}
	} else if e := a.writeXrayRules(domains); e != nil {
		log.Printf("apply: %v", e)
		jsonOut(w, map[string]string{"error": "Не удалось применить маршрут: " + e.Error()})
		return
	}
	a.state = in
	_ = a.save()
	jsonOut(w, map[string]any{"ok": true, "services": len(in.Services), "domains": len(domains)})
}

func buildVLESSConfig(raw string, domains []string) ([]byte, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "vless" || u.User == nil || u.Hostname() == "" || u.Port() == "" {
		return nil, errors.New("ожидается ссылка vless://uuid@сервер:порт")
	}
	port := 0
	if _, e = fmt.Sscanf(u.Port(), "%d", &port); e != nil || port < 1 || port > 65535 {
		return nil, errors.New("некорректный порт VLESS")
	}
	q := u.Query()
	network := q.Get("type")
	if network == "" {
		network = "tcp"
	}
	if network != "tcp" && network != "ws" && network != "grpc" {
		return nil, fmt.Errorf("неподдерживаемый транспорт VLESS: %s", network)
	}
	security := q.Get("security")
	if security == "" {
		security = "none"
	}
	if security != "none" && security != "tls" && security != "reality" {
		return nil, fmt.Errorf("неподдерживаемая защита VLESS: %s", security)
	}
	user := map[string]any{"id": u.User.Username(), "encryption": "none"}
	if flow := q.Get("flow"); flow != "" {
		user["flow"] = flow
	}
	stream := map[string]any{"network": network, "security": security}
	if security == "tls" {
		stream["tlsSettings"] = map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp")}
	}
	if security == "reality" {
		if q.Get("pbk") == "" || q.Get("sid") == "" {
			return nil, errors.New("для reality нужны параметры pbk и sid")
		}
		stream["realitySettings"] = map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp"), "publicKey": q.Get("pbk"), "shortId": q.Get("sid")}
	}
	if network == "ws" {
		stream["wsSettings"] = map[string]any{"path": q.Get("path"), "headers": map[string]any{"Host": q.Get("host")}}
	}
	if network == "grpc" {
		stream["grpcSettings"] = map[string]any{"serviceName": q.Get("serviceName")}
	}
	cfg := map[string]any{"log": map[string]any{"loglevel": "warning"}, "inbounds": []any{map[string]any{"listen": "0.0.0.0", "port": 1080, "protocol": "socks", "settings": map[string]any{"udp": true}}}, "outbounds": []any{map[string]any{"tag": "vless-out", "protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": u.Hostname(), "port": port, "users": []any{user}}}}, "streamSettings": stream}, map[string]any{"tag": "direct", "protocol": "freedom"}}, "routing": map[string]any{"domainStrategy": "AsIs", "rules": []any{map[string]any{"type": "field", "domain": domains, "outboundTag": "vless-out"}}}}
	return json.MarshalIndent(cfg, "", "  ")
}

func (a *App) writeVLESSConfig(raw string, domains []string) error {
	b, e := buildVLESSConfig(raw, domains)
	if e != nil {
		return e
	}
	c, e := a.router()
	if e != nil {
		return e
	}
	defer c.Close()
	rows, e := c.cmd("/file/print", map[string]string{".proplist": ".id"}, "?name=xray-config/config.json")
	if e != nil {
		return e
	}
	if len(rows) == 0 {
		_, e = c.cmd("/file/add", map[string]string{"name": "xray-config/config.json", "contents": string(b)})
	} else {
		_, e = c.cmd("/file/set", map[string]string{".id": rows[0][".id"], "contents": string(b)})
	}
	if e != nil {
		return e
	}
	_, e = c.cmd("/container/restart", map[string]string{"numbers": "xray-vless"})
	return e
}
func (a *App) loadGeo() {
	for id, file := range geoFiles {
		if b, e := os.ReadFile(filepath.Join(a.dataDir, "geosite-"+file)); e == nil {
			a.geo[id] = parseGeoText(string(b))
		}
	}
}
func parseGeoText(raw string) []string {
	set := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "domain:") || strings.HasPrefix(line, "full:") {
			set[line] = true
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
func (a *App) currentCatalog() []Service {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Service, len(catalog))
	copy(out, catalog)
	for i := range out {
		if list := a.geo[out[i].ID]; len(list) > 0 {
			out[i].Domains = list
			out[i].Note = "GeoSite RUNET Freedom · обновлено"
		}
	}
	return out
}
func (a *App) refreshGeo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method", 405)
		return
	}
	if a.xraySocks == "" {
		jsonOut(w, map[string]string{"error": "Не задан адрес SOCKS Xray."})
		return
	}
	client := &http.Client{Timeout: 35 * time.Second, Transport: &http.Transport{DialContext: socksDialer(a.xraySocks)}}
	updated := 0
	for id, file := range geoFiles {
		url := "https://raw.githubusercontent.com/runetfreedom/russia-blocked-geosite/release/" + file
		resp, e := client.Get(url)
		if e != nil {
			continue
		}
		body, e := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if e != nil || resp.StatusCode != 200 {
			continue
		}
		domains := parseGeoText(string(body))
		if len(domains) == 0 {
			continue
		}
		if e = os.WriteFile(filepath.Join(a.dataDir, "geosite-"+file), body, 0600); e != nil {
			continue
		}
		a.mu.Lock()
		a.geo[id] = domains
		a.mu.Unlock()
		updated++
	}
	if updated == 0 {
		jsonOut(w, map[string]string{"error": "Не удалось скачать GeoSite через VLESS. Проверьте работу VLESS/SOCKS."})
		return
	}
	jsonOut(w, map[string]any{"ok": true, "updated": updated})
}
func socksDialer(proxy string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, target string) (net.Conn, error) {
		d := net.Dialer{}
		c, e := d.DialContext(ctx, "tcp", proxy)
		if e != nil {
			return nil, e
		}
		fail := func(err error) (net.Conn, error) { c.Close(); return nil, err }
		if _, e = c.Write([]byte{5, 1, 0}); e != nil {
			return fail(e)
		}
		rep := make([]byte, 2)
		if _, e = io.ReadFull(c, rep); e != nil || rep[1] != 0 {
			return fail(errors.New("SOCKS authentication failed"))
		}
		host, port, e := net.SplitHostPort(target)
		if e != nil {
			return fail(e)
		}
		pn, e := net.LookupPort("tcp", port)
		if e != nil {
			return fail(e)
		}
		req := []byte{5, 1, 0, 3, byte(len(host))}
		req = append(req, []byte(host)...)
		req = append(req, byte(pn>>8), byte(pn))
		if _, e = c.Write(req); e != nil {
			return fail(e)
		}
		h := make([]byte, 4)
		if _, e = io.ReadFull(c, h); e != nil || h[1] != 0 {
			return fail(errors.New("SOCKS connect failed"))
		}
		n := 0
		switch h[3] {
		case 1:
			n = 4
		case 4:
			n = 16
		case 3:
			b := make([]byte, 1)
			if _, e = io.ReadFull(c, b); e != nil {
				return fail(e)
			}
			n = int(b[0])
		default:
			return fail(errors.New("invalid SOCKS reply"))
		}
		tail := make([]byte, n+2)
		if _, e = io.ReadFull(c, tail); e != nil {
			return fail(e)
		}
		return c, nil
	}
}
func (a *App) xrayStatus(w http.ResponseWriter, r *http.Request) {
	c, e := a.router()
	if e != nil {
		jsonOut(w, map[string]any{"running": false, "error": e.Error()})
		return
	}
	defer c.Close()
	rows, e := c.cmd("/container/print", map[string]string{".proplist": "name,status,memory-current,cpu-usage"}, "?name=xray-vless")
	if e != nil || len(rows) == 0 {
		jsonOut(w, map[string]any{"running": false})
		return
	}
	row := rows[0]
	jsonOut(w, map[string]any{"running": row["status"] == "running", "memory": row["memory-current"], "cpu": row["cpu-usage"]})
}
func (a *App) test(w http.ResponseWriter, r *http.Request) {
	if a.xraySocks == "" {
		jsonOut(w, map[string]any{"ok": false, "error": "не задан SOCKS Xray — проверка маршрута невозможна"})
		return
	}
	req, e := http.NewRequest(http.MethodGet, "https://api.ipify.org", nil)
	if e == nil {
		client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{DialContext: socksDialer(a.xraySocks)}}
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
			jsonOut(w, map[string]any{"ok": resp.StatusCode == 200, "ip": strings.TrimSpace(string(b))})
			return
		}
	}
	jsonOut(w, map[string]any{"ok": false, "error": "проверка недоступна"})
}
func (a *App) writeXrayRules(domains []string) error {
	c, e := a.router()
	if e != nil {
		return e
	}
	defer c.Close()
	rows, e := c.cmd("/file/print", map[string]string{".proplist": ".id,name,contents"}, "?name=xray-config/config.json")
	if e != nil {
		return e
	}
	if len(rows) != 1 {
		return errors.New("config.json не найден")
	}
	var cfg map[string]any
	if e = json.Unmarshal([]byte(rows[0]["contents"]), &cfg); e != nil {
		return e
	}
	routes, ok := cfg["routing"].(map[string]any)
	if !ok {
		return errors.New("в конфиге нет routing")
	}
	rules, ok := routes["rules"].([]any)
	if !ok {
		return errors.New("в конфиге нет rules")
	}
	updated := false
	for _, v := range rules {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if m["outboundTag"] == "vless-out" {
			if _, has := m["domain"]; has {
				m["domain"] = domains
				updated = true
				break
			}
		}
	}
	if !updated {
		rules = append(rules, map[string]any{"type": "field", "domain": domains, "outboundTag": "vless-out"})
		routes["rules"] = rules
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if _, e = c.cmd("/file/set", map[string]string{".id": rows[0][".id"], "contents": string(b)}); e != nil {
		return e
	}
	_, e = c.cmd("/container/restart", map[string]string{"numbers": "xray-vless"})
	return e
}

type ros struct{ conn net.Conn }

func (a *App) router() (*ros, error) {
	if a.routerAPI == "" || a.routerUser == "" {
		return nil, errors.New("нет настроек RouterOS API")
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	n, e := d.Dial("tcp", a.routerAPI)
	if e != nil {
		return nil, e
	}
	c := &ros{n}
	if _, e = c.cmd("/login", map[string]string{"name": a.routerUser, "password": a.routerPass}); e != nil {
		n.Close()
		return nil, e
	}
	return c, nil
}
func (c *ros) Close() { _ = c.conn.Close() }
func writeLen(w io.Writer, n int) error {
	var b []byte
	switch {
	case n < 0x80:
		b = []byte{byte(n)}
	case n < 0x4000:
		b = []byte{byte(n>>8 | 0x80), byte(n)}
	case n < 0x200000:
		b = []byte{byte(n>>16 | 0xC0), byte(n >> 8), byte(n)}
	case n < 0x10000000:
		b = []byte{byte(n>>24 | 0xE0), byte(n >> 16), byte(n >> 8), byte(n)}
	default:
		b = []byte{0xF0, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	}
	_, e := w.Write(b)
	return e
}
func readLen(r io.Reader) (int, error) {
	var b [1]byte
	if _, e := io.ReadFull(r, b[:]); e != nil {
		return 0, e
	}
	x := b[0]
	if x < 0x80 {
		return int(x), nil
	}
	n := 0
	count := 0
	switch {
	case x < 0xC0:
		n = int(x & 0x3F)
		count = 1
	case x < 0xE0:
		n = int(x & 0x1F)
		count = 2
	case x < 0xF0:
		n = int(x & 0x0F)
		count = 3
	default:
		count = 4
	}
	for i := 0; i < count; i++ {
		if _, e := io.ReadFull(r, b[:]); e != nil {
			return 0, e
		}
		n = n<<8 | int(b[0])
	}
	return n, nil
}
func (c *ros) cmd(path string, attrs map[string]string, queries ...string) ([]map[string]string, error) {
	words := []string{path}
	for k, v := range attrs {
		words = append(words, "="+k+"="+v)
	}
	words = append(words, queries...)
	for _, s := range words {
		if e := writeLen(c.conn, len(s)); e != nil {
			return nil, e
		}
		if _, e := io.WriteString(c.conn, s); e != nil {
			return nil, e
		}
	}
	if e := writeLen(c.conn, 0); e != nil {
		return nil, e
	}
	var out []map[string]string
	for {
		row := map[string]string{}
		for {
			n, e := readLen(c.conn)
			if e != nil {
				return nil, e
			}
			if n == 0 {
				break
			}
			b := make([]byte, n)
			if _, e = io.ReadFull(c.conn, b); e != nil {
				return nil, e
			}
			s := string(b)
			if s == "!done" {
				return out, nil
			}
			if s == "!trap" {
				return nil, errors.New(row["message"])
			}
			if s == "!re" {
				continue
			}
			if strings.HasPrefix(s, "=") {
				p := strings.SplitN(s[1:], "=", 2)
				if len(p) == 2 {
					row[p[0]] = p[1]
				}
			}
		}
		if len(row) > 0 {
			out = append(out, row)
		}
	}
}

var _ = binary.BigEndian
