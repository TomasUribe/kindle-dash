package main

import (
	"fmt"
	"image/png"
	"net/http"
	"strconv"
)

// ServeSim shows the screen in a web page and turns clicks into touches, so the whole app
// (layout, taps, rotation, swipes) can be tried on a computer. It is never used on the Kindle.
func ServeSim(addr string, a *App, input chan<- InputEvent, nd *NullDisplay, logf func(string, ...any)) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, simPage)
	})
	mux.HandleFunc("/screen.png", func(w http.ResponseWriter, r *http.Request) {
		img, _, _ := a.Snapshot()
		if img == nil {
			http.Error(w, "no frame yet", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_ = png.Encode(w, img)
	})
	mux.HandleFunc("/ver", func(w http.ResponseWriter, r *http.Request) {
		_, o, v := a.Snapshot()
		fmt.Fprintf(w, "%d|%s|full refreshes %d, partial %d", v, o, nd.Fulls.Load(), nd.Partials.Load())
	})
	mux.HandleFunc("/tap", func(w http.ResponseWriter, r *http.Request) {
		x, _ := strconv.Atoi(r.URL.Query().Get("x"))
		y, _ := strconv.Atoi(r.URL.Query().Get("y"))
		_, o, _ := a.Snapshot()
		dx, dy := LogicalToDevice(x, y, o)
		input <- InputEvent{Kind: "tap", X: dx, Y: dy}
	})
	mux.HandleFunc("/swipe", func(w http.ResponseWriter, r *http.Request) {
		_, o, _ := a.Snapshot()
		lx0, ly := 900, 700
		lx1 := 300
		if r.URL.Query().Get("dir") == "right" {
			lx0, lx1 = 300, 900
		}
		sx, sy := LogicalToDevice(lx0, ly, o)
		ex, ey := LogicalToDevice(lx1, ly, o)
		input <- InputEvent{Kind: "swipe", X: sx, Y: sy, DX: ex - sx, DY: ey - sy}
	})
	logf("simulator on http://%s/", addr)
	return http.ListenAndServe(addr, mux)
}

const simPage = `<!doctype html><meta charset=utf-8><title>Kindle Dash simulator</title>
<style>
body{margin:0;background:#d8d8d8;font:14px sans-serif;display:flex;flex-direction:column;align-items:center}
#bar{padding:6px 10px;color:#333}
img{max-height:calc(100vh - 70px);max-width:100vw;background:#fff;box-shadow:0 2px 12px #0006;cursor:pointer}
button{margin:0 4px}
</style>
<div id=bar><button onclick="fetch('/swipe?dir=right')">&larr; swipe</button>
<button onclick="fetch('/swipe?dir=left')">swipe &rarr;</button> <span id=info></span></div>
<img id=s src="/screen.png">
<script>
var s=document.getElementById('s'), last='';
s.onclick=function(e){var r=s.getBoundingClientRect();
 var x=Math.round((e.clientX-r.left)*s.naturalWidth/r.width), y=Math.round((e.clientY-r.top)*s.naturalHeight/r.height);
 fetch('/tap?x='+x+'&y='+y);};
setInterval(function(){fetch('/ver').then(function(r){return r.text()}).then(function(t){
 document.getElementById('info').textContent=t.split('|').slice(1).join(' | ');
 var v=t.split('|')[0]; if(v!==last){last=v; s.src='/screen.png?v='+v;}});},700);
</script>`
