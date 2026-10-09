// Command uidev serves the BackupHarbor web front end over plain HTTP and
// forwards the JavaScript calls to the real Go back end (internal/gui). It lets
// the UI be exercised and screenshotted with a regular headless browser, which
// the Wails window itself does not allow. It is a development and documentation
// tool only and is not part of the shipped application.
//
// Usage: uidev -addr 127.0.0.1:8765 [-pick-file PATH] [-pick-dir PATH] [-save PATH]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"backupharbor/frontend"
	"backupharbor/internal/gui"
)

const shim = `(function () {
  const post = async (name, args) => {
    const r = await fetch('/rpc/' + name, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(args) });
    const j = await r.json();
    if (j.error) throw new Error(j.error);
    return j.result;
  };
  const handlers = {};
  window.go = { gui: { App: new Proxy({}, { get: (_, name) => (...args) => post(name, args) }) } };
  window.runtime = { EventsOn: (n, cb) => { (handlers[n] = handlers[n] || []).push(cb); } };
  const es = new EventSource('/events');
  es.onmessage = (m) => { const e = JSON.parse(m.data); (handlers[e.name] || []).forEach((cb) => cb(e.data)); };
})();`

type hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func (h *hub) emit(name string, data any) {
	b, _ := json.Marshal(map[string]any{"name": name, "data": data})
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.subs {
		select {
		case c <- b:
		default:
		}
	}
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "listen address")
	pickFile := flag.String("pick-file", "", "path returned by the file picker")
	pickDir := flag.String("pick-dir", "", "path returned by the folder picker")
	save := flag.String("save", "", "path returned by the save dialog (extension is added)")
	flag.Parse()
	if !strings.HasPrefix(*addr, "127.0.0.1:") && !strings.HasPrefix(*addr, "localhost:") {
		log.Fatal("uidev only listens on the loopback interface")
	}

	app := gui.New()
	h := &hub{subs: map[chan []byte]struct{}{}}
	app.Emit = h.emit
	app.PickFile = func(string, string) (string, error) { return *pickFile, nil }
	app.PickDir = func(string) (string, error) { return *pickDir, nil }
	app.SaveFile = func(_, name, _ string) (string, error) {
		if *save == "" {
			return "", nil
		}
		return *save, nil
	}
	app.OpenPath = func(p string) error { log.Printf("open: %s", p); return nil }

	mux := http.NewServeMux()
	assets, _ := fs.Sub(frontend.Assets, ".")
	mux.HandleFunc("/shim.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, shim)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			b, _ := fs.ReadFile(assets, "index.html")
			html := strings.Replace(string(b), `<script src="core.js">`, `<script src="/shim.js"></script><script src="core.js">`, 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, html)
			return
		}
		http.FileServerFS(assets).ServeHTTP(w, r)
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no streaming", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		c := make(chan []byte, 256)
		h.mu.Lock()
		h.subs[c] = struct{}{}
		h.mu.Unlock()
		defer func() { h.mu.Lock(); delete(h.subs, c); h.mu.Unlock() }()
		fmt.Fprint(w, ": ok\n\n")
		fl.Flush()
		for {
			select {
			case b := <-c:
				fmt.Fprintf(w, "data: %s\n\n", b)
				fl.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/rpc/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/rpc/")
		var raw []json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&raw)
		m := reflect.ValueOf(app).MethodByName(name)
		reply := func(res any, err error) {
			w.Header().Set("Content-Type", "application/json")
			out := map[string]any{"result": res}
			if err != nil {
				out["error"] = err.Error()
			}
			_ = json.NewEncoder(w).Encode(out)
		}
		if !m.IsValid() {
			reply(nil, fmt.Errorf("unknown method %s", name))
			return
		}
		mt := m.Type()
		args := make([]reflect.Value, mt.NumIn())
		for i := range args {
			v := reflect.New(mt.In(i))
			if i < len(raw) {
				if err := json.Unmarshal(raw[i], v.Interface()); err != nil {
					reply(nil, fmt.Errorf("argument %d: %v", i, err))
					return
				}
			}
			args[i] = v.Elem()
		}
		outs := m.Call(args)
		var res any
		var err error
		errType := reflect.TypeOf((*error)(nil)).Elem()
		for _, o := range outs {
			if o.Type() == errType {
				if !o.IsNil() {
					err = o.Interface().(error)
				}
				continue
			}
			res = o.Interface()
		}
		reply(res, err)
	})
	log.Printf("uidev listening on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
