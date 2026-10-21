// woovi-pix-tester: app web mínimo para testar cobranças Pix na Woovi.
//
// O AppID fica só no backend (nunca vai pro navegador). O front fala com
// este servidor, que repassa as chamadas para a API da Woovi.
//
// Variáveis de ambiente:
//
//	WOOVI_APP_ID    (obrigatória) AppID gerado em API/Plugins
//	WOOVI_BASE_URL  (opcional)    padrão: https://api.woovi-sandbox.com
//	PORT            (opcional)    padrão: 8080
package main

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

//go:embed static
var staticFS embed.FS

type server struct {
	appID   string
	baseURL string
	client  *http.Client

	mu     sync.Mutex
	events []webhookEvent
	subs   map[chan webhookEvent]struct{}
}

type webhookEvent struct {
	ReceivedAt time.Time       `json:"receivedAt"`
	Event      string          `json:"event"`
	Payload    json.RawMessage `json:"payload"`
}

func main() {
	appID := os.Getenv("WOOVI_APP_ID")
	if appID == "" {
		log.Fatal("defina WOOVI_APP_ID (AppID da Woovi)")
	}
	base := envOr("WOOVI_BASE_URL", "https://api.woovi-sandbox.com")
	port := envOr("PORT", "8080")

	s := &server{
		appID:   appID,
		baseURL: base,
		client:  &http.Client{Timeout: 20 * time.Second},
		subs:    map[chan webhookEvent]struct{}{},
	}

	static, _ := fs.Sub(staticFS, "static")
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServer(http.FS(static)))
	mux.HandleFunc("GET /api/config", s.config)
	mux.HandleFunc("POST /api/charge", s.createCharge)
	mux.HandleFunc("GET /api/charge/{id}", s.getCharge)
	mux.HandleFunc("POST /api/charge/{transactionID}/simulate", s.simulatePayment)
	mux.HandleFunc("POST /webhook", s.webhook)
	mux.HandleFunc("GET /api/events", s.eventsSSE)

	log.Printf("woovi-pix-tester em http://localhost:%s (API: %s)", port, base)
	log.Fatal(http.ListenAndServe(":"+port, logRequests(mux)))
}

func (s *server) config(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"baseURL": s.baseURL,
		"sandbox": s.baseURL != "https://api.woovi.com",
	})
}

// POST /api/charge  {value: centavos, comment?, customer?{name,email,phone,taxID}, expiresIn?}
func (s *server) createCharge(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Value     int             `json:"value"`
		Comment   string          `json:"comment,omitempty"`
		ExpiresIn int             `json:"expiresIn,omitempty"`
		Customer  json.RawMessage `json:"customer,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Value <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "value (centavos) obrigatório e > 0"})
		return
	}
	body := map[string]any{
		"correlationID": uuid(),
		"value":         in.Value,
	}
	if in.Comment != "" {
		body["comment"] = in.Comment
	}
	if in.ExpiresIn > 0 {
		body["expiresIn"] = in.ExpiresIn
	}
	if len(in.Customer) > 0 && string(in.Customer) != "null" {
		body["customer"] = in.Customer
	}
	s.forward(w, http.MethodPost, "/api/v1/charge", body)
}

// GET /api/charge/{id}  (id = correlationID ou globalID)
func (s *server) getCharge(w http.ResponseWriter, r *http.Request) {
	s.forward(w, http.MethodGet, "/api/v1/charge/"+url.PathEscape(r.PathValue("id")), nil)
}

// Simula o pagamento de uma cobrança da conta de teste.
// Doc: developers.woovi.com/docs/test-environment/test-account/test-pay-pix
func (s *server) simulatePayment(w http.ResponseWriter, r *http.Request) {
	q := url.Values{
		"transactionID": []string{r.PathValue("transactionID")},
	}
	s.forward(w, http.MethodGet, "/openpix/testing?"+q.Encode(), nil)
}

// Recebe webhooks da Woovi (ex.: OPENPIX:CHARGE_COMPLETED) e repassa via SSE.
func (s *server) webhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	var head struct {
		Event string `json:"event"`
	}
	_ = json.Unmarshal(raw, &head)
	if !json.Valid(raw) {
		raw, _ = json.Marshal(string(raw))
	}
	ev := webhookEvent{ReceivedAt: time.Now(), Event: head.Event, Payload: raw}
	log.Printf("webhook recebido: %s", ev.Event)

	s.mu.Lock()
	s.events = append(s.events, ev)
	if len(s.events) > 100 {
		s.events = s.events[len(s.events)-100:]
	}
	for ch := range s.subs {
		select {
		case ch <- ev:
		default:
		}
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (s *server) eventsSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming não suportado", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	ch := make(chan webhookEvent, 16)
	s.mu.Lock()
	backlog := append([]webhookEvent(nil), s.events...)
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()

	send := func(ev webhookEvent) bool {
		b, err := json.Marshal(ev)
		if err != nil {
			log.Printf("SSE event encode failed: %v", err)
			return true
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	for _, ev := range backlog {
		if !send(ev) {
			return
		}
	}
	if _, err := fmt.Fprint(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			if !send(ev) {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// forward chama a Woovi e devolve status + corpo como vieram.
func (s *server) forward(w http.ResponseWriter, method, path string, body any) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, s.baseURL+path, rdr)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// A Woovi espera o AppID puro, sem "Bearer".
	req.Header.Set("Authorization", s.appID)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("response body close failed: %v", err)
		}
	}()
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("response copy failed: %v", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("JSON response encode failed: %v", err)
	}
}

func uuid() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/events" {
			log.Printf("%s %s", r.Method, r.URL.Path)
		}
		h.ServeHTTP(w, r)
	})
}
