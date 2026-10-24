// Package discovery provides a leased directory of trusted Anra Agent Cards.
package discovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	sdk "github.com/a2aproject/a2a-go/v2/a2a"
)

const AgentsPath = "/agents"

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Record is public directory information; the lease credential is never exposed.
type Record struct {
	Name      string         `json:"name"`
	CardURL   string         `json:"cardUrl"`
	Card      *sdk.AgentCard `json:"card"`
	ExpiresAt time.Time      `json:"expiresAt"`
}

type registration struct {
	Record
	Lease string `json:"lease"`
}

type leaseResponse struct {
	TTL time.Duration `json:"ttl"`
}

type registered struct {
	record Record
	lease  string
}

type registry struct {
	mu      sync.Mutex
	entries map[string]registered
	ttl     time.Duration
}

// NewHandler creates an in-memory registry. Agents re-register after a registry
// restart. Expired entries are invisible to lookup and can be reclaimed.
// Access to the directory must be restricted to trusted team members.
func NewHandler(ttl time.Duration) (http.Handler, error) {
	if ttl < time.Second {
		return nil, errors.New("discovery: lease TTL of at least 1s is required")
	}
	r := &registry{entries: make(map[string]registered), ttl: ttl}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+AgentsPath, r.list)
	mux.HandleFunc("GET "+AgentsPath+"/{name}", r.get)
	mux.HandleFunc("PUT "+AgentsPath+"/{name}", r.put)
	mux.HandleFunc("DELETE "+AgentsPath+"/{name}", r.remove)
	return mux, nil
}

func (r *registry) prune() {
	for name, value := range r.entries {
		if !time.Now().Before(value.record.ExpiresAt) {
			delete(r.entries, name)
		}
	}
}

func (r *registry) list(w http.ResponseWriter, _ *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	values := make([]Record, 0, len(r.entries))
	for _, value := range r.entries {
		values = append(values, value.record)
	}
	slices.SortFunc(values, func(a, b Record) int { return strings.Compare(a.Name, b.Name) })
	writeJSON(w, values)
}

func (r *registry) get(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	value, ok := r.entries[req.PathValue("name")]
	if !ok {
		http.Error(w, "agent is not registered", http.StatusNotFound)
		return
	}
	writeJSON(w, value.record)
}

func (r *registry) put(w http.ResponseWriter, req *http.Request) {
	var value registration
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1<<20)).Decode(&value); err != nil {
		http.Error(w, "invalid registration", http.StatusBadRequest)
		return
	}
	name := req.PathValue("name")
	if !validName.MatchString(name) || value.Name != name || value.Lease == "" || value.Card == nil || value.Card.Name != name {
		http.Error(w, "invalid registration", http.StatusBadRequest)
		return
	}
	if _, err := BaseURL(value.Record); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	if current, ok := r.entries[name]; ok && current.lease != value.Lease {
		http.Error(w, "agent name is already registered", http.StatusConflict)
		return
	}
	value.ExpiresAt = time.Now().Add(r.ttl)
	r.entries[name] = registered{record: value.Record, lease: value.Lease}
	writeJSON(w, leaseResponse{TTL: r.ttl})
}

func (r *registry) remove(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	name := req.PathValue("name")
	if current, ok := r.entries[name]; ok {
		if current.lease != req.Header.Get("Anra-Lease") {
			http.Error(w, "lease does not own registration", http.StatusConflict)
			return
		}
		delete(r.entries, name)
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

// BaseURL validates the card location and its advertised messaging origin.
func BaseURL(value Record) (string, error) {
	u, err := url.Parse(value.CardURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/.well-known/agent-card.json" {
		return "", errors.New("discovery: invalid card URL")
	}
	if value.Card == nil || len(value.Card.SupportedInterfaces) == 0 {
		return "", errors.New("discovery: card has no interfaces")
	}
	for _, endpoint := range value.Card.SupportedInterfaces {
		if endpoint == nil {
			return "", errors.New("discovery: empty card interface")
		}
		remote, err := url.Parse(endpoint.URL)
		if err != nil || remote.Scheme != u.Scheme || remote.Host != u.Host || remote.User != nil {
			return "", fmt.Errorf("discovery: card interface is outside its origin")
		}
	}
	return u.Scheme + "://" + u.Host, nil
}
