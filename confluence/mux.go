package confluence

import (
	"net/http"

	"github.com/anacrolix/missinggo/httptoo"
)

var mux = http.NewServeMux()

func init() {
	mux.HandleFunc("/data", withTorrentContext(dataHandler))
	mux.HandleFunc("/status", statusHandler)
	mux.HandleFunc("/info", withTorrentContext(infoHandler))
	mux.HandleFunc("/events", withTorrentContext(eventHandler))
	mux.Handle("/fileState", httptoo.GzipHandler(withTorrentContext(fileStateHandler)))
	mux.HandleFunc("/metainfo", withTorrentContext(metainfoHandler))
}
