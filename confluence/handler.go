package confluence

import (
	"net/http"
	"time"

	"github.com/anacrolix/missinggo/httptoo"
	"github.com/anacrolix/torrent"
)

type handler struct {
	mux        *http.ServeMux
	client     *torrent.Client
	closeGrace time.Duration
}

func NewHandler(client *torrent.Client, closeGrace time.Duration) http.Handler {
	h := handler{http.NewServeMux(), client, closeGrace}

	h.mux.HandleFunc("/", h.mainHandler)
	h.mux.HandleFunc("/data", h.withTorrent(dataHandler))
	h.mux.HandleFunc("/status", h.statusHandler)
	h.mux.HandleFunc("/info", h.withTorrent(infoHandler))
	h.mux.HandleFunc("/events", h.withTorrent(eventHandler))
	h.mux.Handle("/fileState", httptoo.GzipHandler(h.withTorrent(fileStateHandler)))
	h.mux.HandleFunc("/metainfo", h.withTorrent(metainfoHandler))

	return &h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}
